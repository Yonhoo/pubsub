package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"google.golang.org/grpc"
)

type stubControllerClient struct {
	joinRoomFn func(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error)
}

func (s *stubControllerClient) JoinRoom(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error) {
	return s.joinRoomFn(ctx, in, opts...)
}

func (s *stubControllerClient) LeaveRoom(context.Context, *controller.LeaveRoomRequest, ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
	return nil, nil
}

func (s *stubControllerClient) GetRoomInfo(context.Context, *controller.GetRoomInfoRequest, ...grpc.CallOption) (*controller.GetRoomInfoResponse, error) {
	return nil, nil
}

func (s *stubControllerClient) GetUserNode(context.Context, *controller.GetUserNodeRequest, ...grpc.CallOption) (*controller.GetUserNodeResponse, error) {
	return nil, nil
}

func (s *stubControllerClient) GetRoomStats(context.Context, *controller.GetRoomStatsRequest, ...grpc.CallOption) (*controller.GetRoomStatsResponse, error) {
	return nil, nil
}

func newJoinTestHandler(client controller.ControllerServiceClient) *ProtoMessageHandler {
	manager := newSharedWriteManager(1, 1, writeBatchMaxBytes, time.Second, 8)
	ch := NewChannel(8, 8)
	ch.Key = "user-join"
	bucketCfg := &config.BucketConfig{Size: 1, Channel: 8, Room: 8}
	server := &ConnectNodeServer{
		nodeID:           "node-1",
		controllerClient: client,
		sharedWriter:     manager,
		buckets:          []*Bucket{NewBucket(bucketCfg)},
		bucketIdx:        1,
	}
	return &ProtoMessageHandler{
		server:         server,
		channel:        ch,
		writeSessionID: 21,
	}
}

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

func TestJoinRoomSuccessWaitsForControllerBeforeAck(t *testing.T) {
	releaseJoin := make(chan struct{})
	joinCalled := make(chan struct{}, 1)
	h := newJoinTestHandler(&stubControllerClient{
		joinRoomFn: func(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error) {
			joinCalled <- struct{}{}
			<-releaseJoin
			return &controller.JoinRoomResponse{Success: true, Message: "ok"}, nil
		},
	})
	req := &proto.Proto{Op: 1, Seq: 7, Roomid: "room-a", Userid: "user-a", Body: []byte("user-a")}
	session := &mockGettySession{}

	done := make(chan error, 1)
	go func() {
		done <- h.processClientRequest(session, req)
	}()

	select {
	case <-joinCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected JoinRoom to be invoked")
	}

	shard := h.server.sharedWriter.pickShard(h.writeSessionID)
	if got := len(shard.in); got != 0 {
		t.Fatalf("expected no response enqueue before controller reply, got %d", got)
	}
	select {
	case err := <-done:
		t.Fatalf("expected processClientRequest to block until JoinRoom reply, got %v", err)
	default:
	}

	close(releaseJoin)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected join success, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected processClientRequest to complete after controller reply")
	}

	if got := len(shard.in); got != 1 {
		t.Fatalf("expected one queued response, got %d", got)
	}
	ev := <-shard.in
	if ev.msg == nil || ev.msg.Op != 2 || ev.msg.Seq != req.Seq {
		t.Fatalf("unexpected ack proto: %+v", ev.msg)
	}
	var body ProtoResponse
	if err := json.Unmarshal(ev.msg.Body, &body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("expected success response, got %+v", body)
	}
	if h.channel.Room == nil || h.channel.Room.ID != req.Roomid {
		t.Fatalf("expected channel room=%s, got %#v", req.Roomid, h.channel.Room)
	}
	if !h.channel.NeedPush(2) {
		t.Fatal("expected join success to watch op=2 before returning")
	}
}

func TestJoinRoomFailureReturnsErrorAckWithoutRoomMutation(t *testing.T) {
	h := newJoinTestHandler(&stubControllerClient{
		joinRoomFn: func(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error) {
			return &controller.JoinRoomResponse{Success: false, Message: "denied"}, nil
		},
	})
	req := &proto.Proto{Op: 1, Seq: 8, Roomid: "room-b", Userid: "user-b", Body: []byte("user-b")}

	err := h.processClientRequest(&mockGettySession{}, req)
	if err == nil || !strings.Contains(err.Error(), "JoinRoom 失败") {
		t.Fatalf("expected join failure, got %v", err)
	}

	shard := h.server.sharedWriter.pickShard(h.writeSessionID)
	if got := len(shard.in); got != 1 {
		t.Fatalf("expected one queued error response, got %d", got)
	}
	ev := <-shard.in
	if ev.msg == nil || ev.msg.Op != 2 || ev.msg.Seq != req.Seq {
		t.Fatalf("unexpected error ack proto: %+v", ev.msg)
	}
	var body ProtoResponse
	if err := json.Unmarshal(ev.msg.Body, &body); err != nil {
		t.Fatalf("failed to decode error response body: %v", err)
	}
	if body.Code == 0 {
		t.Fatalf("expected non-success response, got %+v", body)
	}
	if h.channel.Room != nil {
		t.Fatalf("expected room to remain unset on join failure, got %#v", h.channel.Room)
	}
	if h.channel.NeedPush(2) {
		t.Fatal("expected join failure to not register watch op=2")
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
