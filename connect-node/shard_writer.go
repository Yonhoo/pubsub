package main

import (
	"errors"
	"fmt"
	getty "github.com/AlexStocks/getty/transport"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	writeBatchMaxBytes = 64 * 1024
)

var (
	errSharedWriterQueueFull = errors.New("shared writer queue full")
)

type writeEventKind uint8

const (
	writeEventRegister writeEventKind = iota + 1
	writeEventEnqueue
	writeEventUnregister
)

type writeEvent struct {
	kind      writeEventKind
	sessionID uint64

	session getty.Session
	handler *gettypkg.ProtoPackageHandler
	owner   *ProtoMessageHandler

	msg *proto.Proto
}

type sharedSessionState struct {
	session getty.Session
	handler *gettypkg.ProtoPackageHandler
	owner   *ProtoMessageHandler

	pending      [][]byte
	pendingBytes int
	deadline     time.Time
}

type flushShard struct {
	id int

	batchSize     int
	maxBatchBytes int
	flushInterval time.Duration

	in   chan writeEvent
	stop chan struct{}
	wg   sync.WaitGroup

	sessions map[uint64]*sharedSessionState

	timer        *time.Timer
	timerC       <-chan time.Time
	timerArmed   bool
	nextDeadline time.Time
}

func newFlushShard(id, batchSize, maxBatchBytes int, flushInterval time.Duration, queueSize int) *flushShard {
	if batchSize <= 0 {
		batchSize = 1
	}
	if maxBatchBytes <= 0 {
		maxBatchBytes = writeBatchMaxBytes
	}
	if queueSize <= 0 {
		queueSize = 1024
	}
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond
	}
	return &flushShard{
		id:            id,
		batchSize:     batchSize,
		maxBatchBytes: maxBatchBytes,
		flushInterval: flushInterval,
		in:            make(chan writeEvent, queueSize),
		stop:          make(chan struct{}),
		sessions:      make(map[uint64]*sharedSessionState),
	}
}

func (s *flushShard) start() {
	s.wg.Add(1)
	go s.loop()
}

func (s *flushShard) stopAndWait() {
	close(s.stop)
	s.wg.Wait()
}

func (s *flushShard) loop() {
	defer s.wg.Done()
	for {
		select {
		case ev := <-s.in:
			s.handleEvent(ev)
		case <-s.timerC:
			s.timerArmed = false
			s.timerC = nil
			s.nextDeadline = time.Time{}
			s.handleTimer()
		case <-s.stop:
			s.flushAll()
			s.stopTimer()
			return
		}
	}
}

func (s *flushShard) handleEvent(ev writeEvent) {
	switch ev.kind {
	case writeEventRegister:
		s.sessions[ev.sessionID] = &sharedSessionState{
			session: ev.session,
			handler: ev.handler,
			owner:   ev.owner,
			pending: make([][]byte, 0, s.batchSize),
		}
	case writeEventUnregister:
		st, ok := s.sessions[ev.sessionID]
		if !ok {
			return
		}
		s.flushState(st)
		delete(s.sessions, ev.sessionID)
	case writeEventEnqueue:
		st, ok := s.sessions[ev.sessionID]
		if !ok || st.handler == nil || st.session == nil || ev.msg == nil {
			return
		}
		data, err := st.handler.Write(st.session, ev.msg)
		if err != nil {
			wsLog("❌ [SharedWriter] encode failed: %v", err)
			return
		}
		st.pending = append(st.pending, data)
		st.pendingBytes += len(data)
		if len(st.pending) == 1 {
			st.deadline = time.Now().Add(s.flushInterval)
			s.armTimer(st.deadline)
		}
		if len(st.pending) >= s.batchSize || st.pendingBytes >= s.maxBatchBytes {
			s.flushState(st)
		}
	}
}

func (s *flushShard) handleTimer() {
	now := time.Now()
	var next time.Time
	for _, st := range s.sessions {
		if len(st.pending) == 0 || st.deadline.IsZero() {
			continue
		}
		if !st.deadline.After(now) {
			s.flushState(st)
			continue
		}
		if next.IsZero() || st.deadline.Before(next) {
			next = st.deadline
		}
	}
	if !next.IsZero() {
		s.armTimer(next)
	}
}

func (s *flushShard) flushAll() {
	for _, st := range s.sessions {
		s.flushState(st)
	}
}

func (s *flushShard) flushState(st *sharedSessionState) {
	if st == nil || len(st.pending) == 0 {
		return
	}

	pkgs := len(st.pending)
	bytes := st.pendingBytes
	owner := st.owner
	if owner != nil {
		owner.writeTraceStart("shared-batch")
	}
	if _, err := st.session.WriteBytesArray(st.pending...); err != nil {
		wsLog("❌ [SharedWriter] WriteBytesArray failed: %v", err)
	} else if owner != nil {
		owner.markWriteTime()
		atomic.AddUint64(&owner.batchFlushes, 1)
		atomic.AddUint64(&owner.batchFlushedPkgs, uint64(pkgs))
		atomic.AddUint64(&owner.batchFlushedBytes, uint64(bytes))
		for {
			old := atomic.LoadUint32(&owner.batchMaxPkgsPerFlush)
			if uint32(pkgs) <= old {
				break
			}
			if atomic.CompareAndSwapUint32(&owner.batchMaxPkgsPerFlush, old, uint32(pkgs)) {
				break
			}
		}
		if pkgs > 1 {
			now := time.Now().UnixNano()
			last := atomic.LoadInt64(&owner.batchLastFlushLogNano)
			if now-last >= int64(time.Second) && atomic.CompareAndSwapInt64(&owner.batchLastFlushLogNano, last, now) {
				remote := ""
				if owner.channel != nil {
					remote = owner.channel.IP
				}
				if enableWriteTraceLog {
					wsLog("✅ [ProtoHandler] batch flush: pkgs=%d bytes=%d remote=%s", pkgs, bytes, remote)
				}
			}
		}
	}
	if owner != nil {
		owner.writeTraceEnd()
	}

	st.pending = st.pending[:0]
	st.pendingBytes = 0
	st.deadline = time.Time{}
}

func (s *flushShard) armTimer(deadline time.Time) {
	if deadline.IsZero() {
		return
	}
	d := time.Until(deadline)
	if d < 0 {
		d = 0
	}
	if s.timer == nil {
		s.timer = time.NewTimer(d)
		s.timerC = s.timer.C
		s.timerArmed = true
		s.nextDeadline = deadline
		return
	}
	if s.timerArmed && !deadline.Before(s.nextDeadline) {
		return
	}
	if s.timerArmed {
		if !s.timer.Stop() {
			select {
			case <-s.timer.C:
			default:
			}
		}
	}
	s.timer.Reset(d)
	s.timerC = s.timer.C
	s.timerArmed = true
	s.nextDeadline = deadline
}

func (s *flushShard) stopTimer() {
	s.timerArmed = false
	s.timerC = nil
	s.nextDeadline = time.Time{}
	if s.timer == nil {
		return
	}
	if !s.timer.Stop() {
		select {
		case <-s.timer.C:
		default:
		}
	}
}

type sharedWriteManager struct {
	shards    []*flushShard
	shardNum  uint64
	startOnce sync.Once
	stopOnce  sync.Once
}

func newSharedWriteManager(shardNum, batchSize, maxBatchBytes int, flushInterval time.Duration, queueSize int) *sharedWriteManager {
	if shardNum <= 0 {
		shardNum = runtime.GOMAXPROCS(0)
	}
	if shardNum <= 0 {
		shardNum = 1
	}
	m := &sharedWriteManager{
		shards:   make([]*flushShard, shardNum),
		shardNum: uint64(shardNum),
	}
	for i := 0; i < shardNum; i++ {
		m.shards[i] = newFlushShard(i, batchSize, maxBatchBytes, flushInterval, queueSize)
	}
	return m
}

func (m *sharedWriteManager) Start() {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		for _, shard := range m.shards {
			shard.start()
		}
	})
}

func (m *sharedWriteManager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		for _, shard := range m.shards {
			shard.stopAndWait()
		}
	})
}

func (m *sharedWriteManager) Register(sessionID uint64, session getty.Session, handler *gettypkg.ProtoPackageHandler, owner *ProtoMessageHandler) error {
	if m == nil {
		return nil
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventRegister,
		sessionID: sessionID,
		session:   session,
		handler:   handler,
		owner:     owner,
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) Unregister(sessionID uint64) error {
	if m == nil {
		return nil
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventUnregister,
		sessionID: sessionID,
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) Enqueue(sessionID uint64, p *proto.Proto) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventEnqueue,
		sessionID: sessionID,
		msg:       p,
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) TryEnqueue(sessionID uint64, p *proto.Proto) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventEnqueue,
		sessionID: sessionID,
		msg:       p,
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	default:
		return errSharedWriterQueueFull
	}
}

func (m *sharedWriteManager) pickShard(sessionID uint64) *flushShard {
	idx := int(sessionID % m.shardNum)
	return m.shards[idx]
}
