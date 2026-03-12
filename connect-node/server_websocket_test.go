package main

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

func TestWriteProtoRequiresSharedWriter(t *testing.T) {
	h := &ProtoMessageHandler{}

	err := h.writeProto(nil, &proto.Proto{Op: 1}, "response")
	if err == nil {
		t.Fatal("expected writeProto to fail without shared writer")
	}
	if !strings.Contains(err.Error(), "shared writer unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteProtoConcurrentUsesSharedWriterQueue(t *testing.T) {
	const writers = 32

	manager := newSharedWriteManager(1, 1, writeBatchMaxBytes, time.Second, writers+8)
	h := &ProtoMessageHandler{
		server:         &ConnectNodeServer{sharedWriter: manager},
		writeSessionID: 7,
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(seq int32) {
			defer wg.Done()
			errs <- h.writeProto(nil, &proto.Proto{Op: 1, Seq: seq}, "response")
		}(int32(i + 1))
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("expected shared writer enqueue to succeed, got %v", err)
		}
	}

	shard := manager.pickShard(h.writeSessionID)
	if got := len(shard.in); got != writers {
		t.Fatalf("expected %d queued write events, got %d", writers, got)
	}
	if got := atomic.LoadUint64(&h.batchEnqueued); got != writers {
		t.Fatalf("expected batchEnqueued=%d, got %d", writers, got)
	}
}

func TestWriteProtoConcurrentWithoutSharedWriterStaysOnErrorPath(t *testing.T) {
	const writers = 16

	h := &ProtoMessageHandler{}

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(seq int32) {
			defer wg.Done()
			errs <- h.writeProto(nil, &proto.Proto{Op: 1, Seq: seq}, "response")
		}(int32(i + 1))
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err == nil {
			t.Fatal("expected shared writer unavailable error")
		}
		if !strings.Contains(err.Error(), "shared writer unavailable") {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if got := atomic.LoadUint64(&h.batchEnqueued); got != 0 {
		t.Fatalf("expected batchEnqueued to remain 0, got %d", got)
	}
	if got := atomic.LoadUint64(&h.batchEnqueueFailures); got != writers {
		t.Fatalf("expected batchEnqueueFailures=%d, got %d", writers, got)
	}
}

func TestWriteProtoQueueFullReturnsExplicitErrorAndCounts(t *testing.T) {
	manager := newSharedWriteManager(1, 1, writeBatchMaxBytes, time.Second, 1)
	h := &ProtoMessageHandler{
		server:         &ConnectNodeServer{sharedWriter: manager},
		writeSessionID: 9,
	}

	shard := manager.pickShard(h.writeSessionID)
	shard.in <- writeEvent{kind: writeEventEnqueue, sessionID: h.writeSessionID, msg: &proto.Proto{Op: 9, Seq: 1}}

	err := h.writeProto(nil, &proto.Proto{Op: 9, Seq: 2}, "response")
	if !errors.Is(err, errSharedWriterQueueFull) {
		t.Fatalf("expected errSharedWriterQueueFull, got %v", err)
	}
	if got := atomic.LoadUint64(&h.batchEnqueued); got != 0 {
		t.Fatalf("expected batchEnqueued=0, got %d", got)
	}
	if got := atomic.LoadUint64(&h.batchEnqueueFailures); got != 1 {
		t.Fatalf("expected batchEnqueueFailures=1, got %d", got)
	}
	if got := atomic.LoadUint64(&h.batchEnqueueQueueFull); got != 1 {
		t.Fatalf("expected batchEnqueueQueueFull=1, got %d", got)
	}
}

func TestServerPushWriterQueueFullStaysNonBlocking(t *testing.T) {
	manager := newSharedWriteManager(1, 1, writeBatchMaxBytes, time.Second, 1)
	ch := NewChannel(8, 8)
	ch.Key = "user-1"
	h := &ProtoMessageHandler{
		server:         &ConnectNodeServer{sharedWriter: manager},
		channel:        ch,
		writeSessionID: 12,
	}
	ch.SetServerPushWriter(func(p *proto.Proto) error {
		return h.enqueueSharedWrite(p, "broadcast")
	})

	shard := manager.pickShard(h.writeSessionID)
	shard.in <- writeEvent{kind: writeEventEnqueue, sessionID: h.writeSessionID, msg: &proto.Proto{Op: 10, Seq: 1}}

	err := ch.Push(&proto.Proto{Op: 10, Seq: 2})
	if !errors.Is(err, errSharedWriterQueueFull) {
		t.Fatalf("expected errSharedWriterQueueFull, got %v", err)
	}
	if got := atomic.LoadInt64(&ch.pushDropCount); got != 1 {
		t.Fatalf("expected pushDropCount=1, got %d", got)
	}
	if got := atomic.LoadUint64(&h.batchEnqueueQueueFull); got != 1 {
		t.Fatalf("expected batchEnqueueQueueFull=1, got %d", got)
	}
}

func BenchmarkWriteProtoSharedWriterParallel(b *testing.B) {
	manager := newSharedWriteManager(1, 64, writeBatchMaxBytes, 50*time.Millisecond, b.N+1024)
	h := &ProtoMessageHandler{
		server:         &ConnectNodeServer{sharedWriter: manager},
		writeSessionID: 11,
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var seq int32
		for pb.Next() {
			seq++
			if err := h.writeProto(nil, &proto.Proto{Op: 1, Seq: seq}, "response"); err != nil {
				b.Fatalf("writeProto failed: %v", err)
			}
		}
	})
}

func BenchmarkWriteProtoSharedWriterManySessions(b *testing.B) {
	manager := newSharedWriteManager(4, 64, writeBatchMaxBytes, 50*time.Millisecond, b.N+1024)
	const sessions = 64
	handlers := make([]*ProtoMessageHandler, sessions)
	for i := 0; i < sessions; i++ {
		handlers[i] = &ProtoMessageHandler{
			server:         &ConnectNodeServer{sharedWriter: manager},
			writeSessionID: uint64(i + 1),
		}
	}

	var cursor uint64
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&cursor, 1)
			h := handlers[idx%uint64(len(handlers))]
			if err := h.writeProto(nil, &proto.Proto{Op: 2, Seq: int32(idx)}, "broadcast"); err != nil {
				b.Fatalf("writeProto failed: %v", err)
			}
		}
	})
}
