package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

type roomFanoutAggregatorManager struct {
	server        *ConnectNodeServer
	batchSize     int
	flushInterval time.Duration
	queueSize     int

	mu          sync.Mutex
	aggregators map[string]*roomFanoutAggregator
	stopped     bool
}

type roomFanoutAggregator struct {
	roomID        string
	server        *ConnectNodeServer
	batchSize     int
	flushInterval time.Duration
	input         chan *proto.Proto
	stop          chan struct{}
	stopped       chan struct{}
}

func newRoomFanoutAggregatorManager(server *ConnectNodeServer, cfg *config.RoomFanoutAggregatorConfig) *roomFanoutAggregatorManager {
	if cfg == nil {
		cfg = &config.RoomFanoutAggregatorConfig{BatchSize: 32, FlushInterval: 5 * time.Millisecond, QueueSize: 1024}
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Millisecond
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	return &roomFanoutAggregatorManager{
		server:        server,
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		queueSize:     cfg.QueueSize,
		aggregators:   make(map[string]*roomFanoutAggregator),
	}
}

func (m *roomFanoutAggregatorManager) Enqueue(roomID string, p *proto.Proto) error {
	if m == nil || roomID == "" || p == nil {
		return nil
	}
	agg, err := m.getOrCreate(roomID)
	if err != nil {
		return err
	}
	select {
	case agg.input <- p:
		return nil
	case <-agg.stop:
		return ErrWorkerQueueClosed
	default:
		return context.DeadlineExceeded
	}
}

func (m *roomFanoutAggregatorManager) getOrCreate(roomID string) (*roomFanoutAggregator, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, ErrWorkerQueueClosed
	}
	if agg, ok := m.aggregators[roomID]; ok {
		return agg, nil
	}
	agg := &roomFanoutAggregator{
		roomID:        roomID,
		server:        m.server,
		batchSize:     m.batchSize,
		flushInterval: m.flushInterval,
		input:         make(chan *proto.Proto, m.queueSize),
		stop:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	m.aggregators[roomID] = agg
	go agg.run()
	return agg, nil
}

func (m *roomFanoutAggregatorManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	aggs := make([]*roomFanoutAggregator, 0, len(m.aggregators))
	for _, agg := range m.aggregators {
		aggs = append(aggs, agg)
	}
	m.mu.Unlock()
	for _, agg := range aggs {
		close(agg.stop)
	}
	for _, agg := range aggs {
		<-agg.stopped
	}
}

func (a *roomFanoutAggregator) run() {
	defer close(a.stopped)
	batch := make([]*proto.Proto, 0, a.batchSize)
	var timer *time.Timer
	var timerC <-chan time.Time

	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timerC = nil
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		atomic.AddInt64(&a.server.roomStarted, 1)
		_ = a.server.broadcastRoomBatch(a.roomID, append([]*proto.Proto(nil), batch...))
		atomic.AddInt64(&a.server.roomCompleted, 1)
		batch = batch[:0]
	}

	for {
		select {
		case p := <-a.input:
			if p == nil {
				continue
			}
			batch = append(batch, p)
			if len(batch) == 1 {
				if timer == nil {
					timer = time.NewTimer(a.flushInterval)
				} else {
					timer.Reset(a.flushInterval)
				}
				timerC = timer.C
			}
			if len(batch) >= a.batchSize {
				stopTimer()
				flush()
			}
		case <-timerC:
			timerC = nil
			flush()
		case <-a.stop:
			stopTimer()
			for {
				select {
				case p := <-a.input:
					if p != nil {
						batch = append(batch, p)
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (s *ConnectNodeServer) broadcastRoomBatch(roomID string, protos []*proto.Proto) error {
	if s == nil || roomID == "" || len(protos) == 0 {
		return nil
	}
	task := &roomBroadcastTask{RoomID: roomID, Protos: protos}
	fullCount := 0
	stoppedCount := 0
	for _, bucket := range s.Buckets() {
		if err := bucket.BroadcastRoomBatch(task); err != nil {
			if errors.Is(err, errBucketStopped) {
				stoppedCount++
				continue
			}
			fullCount++
		}
	}
	if stoppedCount > 0 {
		s.queueStateMu.RLock()
		state := s.queueState
		s.queueStateMu.RUnlock()
		if state != queueStateRunning {
			err := queueStateErr(state)
			s.recordEnqueueFailure("broadcast_queue", err)
			return err
		}
	}
	if fullCount > 0 && fullCount == len(s.buckets) {
		s.recordEnqueueFailure("broadcast_queue", errBucketQueueFull)
		return fmt.Errorf("%w", errBucketQueueFull)
	}
	return nil
}
