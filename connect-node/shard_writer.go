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

type flushTrigger uint8

const (
	flushTriggerOther flushTrigger = iota
	flushTriggerCount
	flushTriggerBytes
	flushTriggerTimeout
)

type writeEvent struct {
	kind      writeEventKind
	sessionID uint64

	session getty.Session
	handler *gettypkg.ProtoPackageHandler
	owner   *ProtoMessageHandler

	// Event payload variants:
	// - msg: encode per session at flush time.
	// - data: pre-encoded frame for one receiver.
	// - batchData: per-session pre-encoded frames.
	// - sessionIDs + batchFrames: same frame batch for many sessions.
	msg         *proto.Proto
	data        []byte
	batchData   map[uint64][]byte
	sessionIDs  []uint64
	batchFrames [][]byte
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
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case ev := <-s.in:
			s.handleEvent(ev)
		case <-ticker.C:
			s.handleTimer()
		case <-s.stop:
			s.flushAll()
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
		s.flushState(st, flushTriggerOther)
		delete(s.sessions, ev.sessionID)
	case writeEventEnqueue:
		switch {
		case ev.batchData != nil:
			for sid, frameData := range ev.batchData {
				st, ok := s.sessions[sid]
				if !ok || len(frameData) == 0 {
					continue
				}
				s.enqueueFrame(st, frameData)
			}
		case len(ev.sessionIDs) > 0 && len(ev.batchFrames) > 0:
			for _, sid := range ev.sessionIDs {
				st, ok := s.sessions[sid]
				if !ok {
					continue
				}
				for _, frameData := range ev.batchFrames {
					s.enqueueFrame(st, frameData)
				}
			}
		case ev.sessionID > 0:
			st, ok := s.sessions[ev.sessionID]
			if !ok {
				return
			}
			var data []byte
			switch {
			case ev.data != nil:
				data = ev.data
			case ev.msg != nil:
				var err error
				data, err = st.handler.Write(st.session, ev.msg)
				if err != nil {
					wsLog("? [SharedWriter] encode failed: %v", err)
					return
				}
			default:
				return
			}
			s.enqueueFrame(st, data)
		}
	}
}

func (s *flushShard) enqueueFrame(st *sharedSessionState, data []byte) {
	if st == nil || len(data) == 0 {
		return
	}
	st.pending = append(st.pending, data)
	st.pendingBytes += len(data)
	if len(st.pending) == 1 {
		st.deadline = time.Now().Add(s.flushInterval)
	}
	switch {
	case len(st.pending) >= s.batchSize:
		s.flushState(st, flushTriggerCount)
	case st.pendingBytes >= s.maxBatchBytes:
		s.flushState(st, flushTriggerBytes)
	}
}

func (s *flushShard) handleTimer() {
	for _, st := range s.sessions {
		if len(st.pending) == 0 {
			continue
		}
		s.flushState(st, flushTriggerTimeout)
	}
}

func (s *flushShard) flushAll() {
	for _, st := range s.sessions {
		s.flushState(st, flushTriggerOther)
	}
}

func (s *flushShard) flushState(st *sharedSessionState, trigger flushTrigger) {
	if st == nil || len(st.pending) == 0 {
		return
	}

	bytes := st.pendingBytes
	owner := st.owner

	if owner != nil && enableWriteTraceLog {
		owner.writeTraceStart("shared-batch")
		defer owner.writeTraceEnd()
	}

	written := len(st.pending)
	var writeErr error
	if written > 0 {
		// pending only contains non-empty encoded frames. Passing the slice directly
		// avoids allocating/copying a second [][]byte on every connection flush.
		if _, err := st.session.WriteBytesArray(st.pending...); err != nil {
			writeErr = err
		}
	}
	if writeErr != nil {
		wsLog("❌ [SharedWriter] WriteBytes failed: %v", writeErr)
	} else if owner != nil {
		s.updateOwnerStats(owner, written, bytes, trigger)
	}

	st.pending = st.pending[:0]
	st.pendingBytes = 0
	st.deadline = time.Time{}
}

func (s *flushShard) updateOwnerStats(owner *ProtoMessageHandler, pkgs, bytes int, trigger flushTrigger) {
	owner.markWriteTime()
	if !enableSharedWriterStats {
		return
	}
	atomic.AddUint64(&owner.batchFlushes, 1)

	switch trigger {
	case flushTriggerCount:
		atomic.AddUint64(&owner.batchFlushByCount, 1)
	case flushTriggerBytes:
		atomic.AddUint64(&owner.batchFlushByBytes, 1)
	case flushTriggerTimeout:
		atomic.AddUint64(&owner.batchFlushByTimeout, 1)
	}

	atomic.AddUint64(&owner.batchFlushedPkgs, uint64(pkgs))
	atomic.AddUint64(&owner.batchFlushedBytes, uint64(bytes))

	// Update max packages per flush (atomic max operation)
	updateAtomicMax(&owner.batchMaxPkgsPerFlush, uint32(pkgs))

	// Log batch flush (throttled)
	if pkgs > 1 && enableWriteTraceLog {
		s.logBatchFlushIfNeeded(owner, pkgs, bytes)
	}
}

func updateAtomicMax(addr *uint32, val uint32) {
	for {
		old := atomic.LoadUint32(addr)
		if val <= old || atomic.CompareAndSwapUint32(addr, old, val) {
			return
		}
	}
}

func (s *flushShard) logBatchFlushIfNeeded(owner *ProtoMessageHandler, pkgs, bytes int) {
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&owner.batchLastFlushLogNano)
	if now-last < int64(time.Second) || !atomic.CompareAndSwapInt64(&owner.batchLastFlushLogNano, last, now) {
		return
	}
	remote := ""
	if owner.channel != nil {
		remote = owner.channel.IP
	}
	wsLog("✅ [ProtoHandler] batch flush: pkgs=%d bytes=%d remote=%s", pkgs, bytes, remote)
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
	ev := writeEvent{
		kind:      writeEventRegister,
		sessionID: sessionID,
		session:   session,
		handler:   handler,
		owner:     owner,
	}
	return m.sendEvent(sessionID, ev)
}

func (m *sharedWriteManager) Unregister(sessionID uint64) error {
	if m == nil {
		return nil
	}
	ev := writeEvent{
		kind:      writeEventUnregister,
		sessionID: sessionID,
	}
	return m.sendEvent(sessionID, ev)
}

func (m *sharedWriteManager) sendEvent(sessionID uint64, ev writeEvent) error {
	shard := m.pickShard(sessionID)
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) Enqueue(sessionID uint64, p *proto.Proto) error {
	return m.enqueue(sessionID, p, false)
}

func (m *sharedWriteManager) TryEnqueue(sessionID uint64, p *proto.Proto) error {
	return m.enqueue(sessionID, p, true)
}

// EnqueuePreEncoded 投递已编码好的字节帧（房间广播的快路径）。
// 与 Enqueue 走同一分片队列，flush 时直接 append，不再调用 handler.Write。
// data 不能为空且必须是一帧完整 ProtoPackage 字节流（与 handler.Write 输出一致）。
func (m *sharedWriteManager) EnqueuePreEncoded(sessionID uint64, data []byte) error {
	return m.enqueueData(sessionID, data, false)
}

func (m *sharedWriteManager) TryEnqueuePreEncoded(sessionID uint64, data []byte) error {
	return m.enqueueData(sessionID, data, true)
}

// EnqueueBatch 批量投递（房间广播优化）：同一字节帧分发给一个 shard 内的多个 session。
// 调用方预先把房间内所有接收者按 shard 分组（via pickShard），每组一次 chan send。
// 相比 100 人房间 100 次 TryEnqueuePreEncoded（100 次 chan send），现在只有 16 次（shard 数量）。

func (m *sharedWriteManager) EnqueueBatchFrames(shardID int, sessionIDs []uint64, frames [][]byte) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	if shardID < 0 || shardID >= len(m.shards) {
		return fmt.Errorf("invalid shard ID %d", shardID)
	}
	if len(sessionIDs) == 0 || len(frames) == 0 {
		return nil
	}
	shard := m.shards[shardID]
	ev := writeEvent{
		kind:        writeEventEnqueue,
		sessionIDs:  append([]uint64(nil), sessionIDs...),
		batchFrames: append([][]byte(nil), frames...),
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

func (m *sharedWriteManager) EnqueueBatch(shardID int, sessionIDs []uint64, data []byte) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	if shardID < 0 || shardID >= len(m.shards) {
		return fmt.Errorf("invalid shard ID %d", shardID)
	}
	if len(sessionIDs) == 0 || len(data) == 0 {
		return nil
	}
	shard := m.shards[shardID]
	batchData := make(map[uint64][]byte, len(sessionIDs))
	for _, sid := range sessionIDs {
		batchData[sid] = data
	}
	ev := writeEvent{
		kind:      writeEventEnqueue,
		batchData: batchData,
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

func (m *sharedWriteManager) enqueue(sessionID uint64, p *proto.Proto, nonBlocking bool) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventEnqueue,
		sessionID: sessionID,
		msg:       p,
	}
	if nonBlocking {
		select {
		case shard.in <- ev:
			return nil
		case <-shard.stop:
			return fmt.Errorf("shared writer shard stopped")
		default:
			return errSharedWriterQueueFull
		}
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) enqueueData(sessionID uint64, data []byte, nonBlocking bool) error {
	if m == nil {
		return fmt.Errorf("shared writer manager nil")
	}
	if len(data) == 0 {
		return fmt.Errorf("shared writer pre-encoded data empty")
	}
	shard := m.pickShard(sessionID)
	ev := writeEvent{
		kind:      writeEventEnqueue,
		sessionID: sessionID,
		data:      data,
	}
	if nonBlocking {
		select {
		case shard.in <- ev:
			return nil
		case <-shard.stop:
			return fmt.Errorf("shared writer shard stopped")
		default:
			return errSharedWriterQueueFull
		}
	}
	select {
	case shard.in <- ev:
		return nil
	case <-shard.stop:
		return fmt.Errorf("shared writer shard stopped")
	}
}

func (m *sharedWriteManager) pickShard(sessionID uint64) *flushShard {
	idx := int(sessionID % m.shardNum)
	return m.shards[idx]
}

// PickShardID 返回给定 sessionID 应路由到的 shard 索引（用于批量分组）。
func (m *sharedWriteManager) PickShardID(sessionID uint64) int {
	return int(sessionID % m.shardNum)
}

// ShardCount 返回 shard 总数（用于批量分组时预分配 map）。
func (m *sharedWriteManager) ShardCount() int {
	return int(m.shardNum)
}
