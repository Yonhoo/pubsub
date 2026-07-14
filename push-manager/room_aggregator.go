package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

type roomBatchAggregatorConfig struct {
	Enabled       bool
	BatchSize     int
	FlushInterval time.Duration
	QueueSize     int
}

type roomBatchAggregatorManager struct {
	server        *PushManagerServer
	batchSize     int
	flushInterval time.Duration
	queueSize     int

	mu          sync.Mutex
	aggregators map[string]*roomBatchAggregator
	stopped     bool
}

type roomBatchAggregator struct {
	roomID        string
	server        *PushManagerServer
	batchSize     int
	flushInterval time.Duration
	input         chan *proto.Proto
	stop          chan struct{}
	stopped       chan struct{}
}

func newRoomBatchAggregatorManager(server *PushManagerServer, cfg roomBatchAggregatorConfig) *roomBatchAggregatorManager {
	if !cfg.Enabled {
		return nil
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 500 * time.Millisecond
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 4096
	}
	return &roomBatchAggregatorManager{
		server:        server,
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		queueSize:     cfg.QueueSize,
		aggregators:   make(map[string]*roomBatchAggregator),
	}
}

func (m *roomBatchAggregatorManager) Enqueue(req *broadcast.BroadCastReq) error {
	if m == nil {
		return errRoomBatchAggregatorDisabled
	}
	if req == nil || req.Proto == nil {
		return nil
	}
	roomID := req.Proto.GetRoomid()
	if roomID == "" {
		return errRoomBatchAggregatorNoRoom
	}
	agg, err := m.getOrCreate(roomID)
	if err != nil {
		return err
	}
	select {
	case agg.input <- req.Proto:
		return nil
	case <-agg.stop:
		return errRoomBatchAggregatorStopped
	default:
		return errRoomBatchAggregatorFull
	}
}

func (m *roomBatchAggregatorManager) getOrCreate(roomID string) (*roomBatchAggregator, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, errRoomBatchAggregatorStopped
	}
	if agg, ok := m.aggregators[roomID]; ok {
		return agg, nil
	}
	agg := &roomBatchAggregator{
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

func (m *roomBatchAggregatorManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	aggs := make([]*roomBatchAggregator, 0, len(m.aggregators))
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

func (a *roomBatchAggregator) run() {
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
		a.server.EnqueueRoomBatch(a.roomID, append([]*proto.Proto(nil), batch...))
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

var (
	errRoomBatchAggregatorDisabled = errors.New("room batch aggregator disabled")
	errRoomBatchAggregatorNoRoom   = errors.New("room batch aggregator requires room id")
	errRoomBatchAggregatorStopped  = errors.New("room batch aggregator stopped")
	errRoomBatchAggregatorFull     = errors.New("room batch aggregator queue full")
)

func describeRoomBatchAggregatorConfig(cfg roomBatchAggregatorConfig) string {
	return fmt.Sprintf("enabled=%v batch=%d flush=%s queue=%d", cfg.Enabled, cfg.BatchSize, cfg.FlushInterval, cfg.QueueSize)
}
