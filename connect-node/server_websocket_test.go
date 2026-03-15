package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubControllerClient struct {
	joinRoomFn  func(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error)
	leaveRoomFn func(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error)
}

func (s *stubControllerClient) JoinRoom(ctx context.Context, in *controller.JoinRoomRequest, opts ...grpc.CallOption) (*controller.JoinRoomResponse, error) {
	return s.joinRoomFn(ctx, in, opts...)
}

func (s *stubControllerClient) LeaveRoom(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
	if s.leaveRoomFn != nil {
		return s.leaveRoomFn(ctx, in, opts...)
	}
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

func newLeaveTestHandler(client controller.ControllerServiceClient) *ProtoMessageHandler {
	bucketCfg := &config.BucketConfig{Size: 1, Channel: 8, Room: 8, RoutineAmount: 1, RoutineSize: 8}
	server := &ConnectNodeServer{
		nodeID:                   "node-1",
		controllerClient:         client,
		buckets:                  []*Bucket{NewBucket(bucketCfg)},
		bucketIdx:                1,
		roomWorkerEnqueueTimeout: 50 * time.Millisecond,
		leavePending:             make(map[string]struct{}),
	}
	server.initLeaveWorkers(1, 8)
	ch := NewChannel(8, 8)
	ch.Key = "leave-user"
	h := &ProtoMessageHandler{
		server:   server,
		channel:  ch,
		bucket:   server.buckets[0],
		roomId:   "room-leave",
		clientId: "user-leave",
		auth:     true,
	}
	if err := h.bucket.Put(h.roomId, h.channel); err != nil {
		panic(err)
	}
	return h
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

func TestCleanupUserUnbindsLocallyBeforeAsyncLeaveCompletes(t *testing.T) {
	releaseLeave := make(chan struct{})
	leaveCalled := make(chan struct{}, 1)
	h := newLeaveTestHandler(&stubControllerClient{
		leaveRoomFn: func(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
			leaveCalled <- struct{}{}
			<-releaseLeave
			return &controller.LeaveRoomResponse{}, nil
		},
	})

	start := time.Now()
	h.cleanupUser()
	elapsed := time.Since(start)

	select {
	case <-leaveCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected LeaveRoom to be dispatched asynchronously")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected cleanupUser to return quickly, took %v", elapsed)
	}
	if got := h.bucket.ChannelCount(); got != 0 {
		t.Fatalf("expected local bucket cleanup before leave completes, got %d channels", got)
	}
	if h.channel.Room != nil {
		t.Fatalf("expected local channel room cleared, got %#v", h.channel.Room)
	}

	close(releaseLeave)
	h.server.Stop()
}

func TestLeaveQueueDeduplicatesRoomUserKey(t *testing.T) {
	leaveCalls := atomic.Int32{}
	releaseLeave := make(chan struct{})
	client := &stubControllerClient{
		leaveRoomFn: func(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
			leaveCalls.Add(1)
			<-releaseLeave
			return &controller.LeaveRoomResponse{}, nil
		},
	}
	server := &ConnectNodeServer{
		controllerClient:         client,
		roomWorkerEnqueueTimeout: 50 * time.Millisecond,
		leavePending:             make(map[string]struct{}),
	}
	server.initLeaveWorkers(1, 8)

	if err := server.EnqueueLeave("user-x", "room-x"); err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	if err := server.EnqueueLeave("user-x", "room-x"); err != nil {
		t.Fatalf("dedup enqueue failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := leaveCalls.Load(); got != 1 {
		t.Fatalf("expected one LeaveRoom call for dedup key, got %d", got)
	}

	close(releaseLeave)
	server.Stop()
}

func TestLeaveQueueRetriesAndEventuallySucceeds(t *testing.T) {
	attempts := atomic.Int32{}
	success := make(chan struct{}, 1)
	server := &ConnectNodeServer{
		controllerClient: &stubControllerClient{leaveRoomFn: func(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
			if attempts.Add(1) < 3 {
				return nil, errors.New("temporary leave failure")
			}
			success <- struct{}{}
			return &controller.LeaveRoomResponse{}, nil
		}},
		roomWorkerEnqueueTimeout: 50 * time.Millisecond,
		leaveRetryDelay:          10 * time.Millisecond,
		leaveMaxAttempts:         3,
		leavePending:             make(map[string]struct{}),
	}
	server.initLeaveWorkers(1, 8)
	defer server.Stop()

	if err := server.EnqueueLeave("user-r", "room-r"); err != nil {
		t.Fatalf("enqueue leave failed: %v", err)
	}

	select {
	case <-success:
	case <-time.After(time.Second):
		t.Fatal("expected retry to eventually succeed")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		server.leavePendingMu.Lock()
		_, exists := server.leavePending[leaveDedupKey("room-r", "user-r")]
		server.leavePendingMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected pending key to clear after retry success")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLeaveQueueFinalFailureClearsPendingAndAllowsReenqueue(t *testing.T) {
	attempts := atomic.Int32{}
	done := make(chan struct{}, 1)
	server := &ConnectNodeServer{
		controllerClient: &stubControllerClient{leaveRoomFn: func(ctx context.Context, in *controller.LeaveRoomRequest, opts ...grpc.CallOption) (*controller.LeaveRoomResponse, error) {
			if attempts.Add(1) == 2 {
				done <- struct{}{}
			}
			return nil, errors.New("permanent leave failure")
		}},
		roomWorkerEnqueueTimeout: 50 * time.Millisecond,
		leaveRetryDelay:          10 * time.Millisecond,
		leaveMaxAttempts:         2,
		leavePending:             make(map[string]struct{}),
	}
	server.initLeaveWorkers(1, 8)
	defer server.Stop()

	if err := server.EnqueueLeave("user-f", "room-f"); err != nil {
		t.Fatalf("enqueue leave failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected leave retries to exhaust")
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		server.leavePendingMu.Lock()
		_, exists := server.leavePending[leaveDedupKey("room-f", "user-f")]
		server.leavePendingMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected pending key to clear after final failure")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := server.EnqueueLeave("user-f", "room-f"); err != nil {
		t.Fatalf("reenqueue after final failure failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := attempts.Load(); got < 3 {
		t.Fatalf("expected reenqueue to trigger new leave attempts, got %d", got)
	}
}

func TestStopConcurrentEnqueueLeaveNoPanicAndStableStopErrors(t *testing.T) {
	server := &ConnectNodeServer{
		roomWorkerEnqueueTimeout: 5 * time.Millisecond,
		leavePending:             make(map[string]struct{}),
	}
	server.initLeaveWorkers(1, 4)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			seq := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				seq++
				err := server.EnqueueLeave(
					fmt.Sprintf("user-stop-%d-%d", worker, seq),
					"room-stop",
				)
				if err == nil {
					continue
				}
				if errors.Is(err, ErrWorkerQueueStopping) || errors.Is(err, ErrWorkerQueueClosed) || errors.Is(err, context.DeadlineExceeded) {
					continue
				}
				t.Errorf("unexpected enqueue error: %v", err)
				return
			}
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	server.Stop()
	close(stop)
	wg.Wait()

	if err := server.EnqueueLeave("user-final", "room-final"); !errors.Is(err, ErrWorkerQueueClosed) {
		t.Fatalf("expected ErrWorkerQueueClosed after stop, got %v", err)
	}
}

func TestStopConcurrentBroadcastRoomNoPanicAndStableStopErrors(t *testing.T) {
	bucketCfg := &config.BucketConfig{Size: 1, Channel: 8, Room: 8}
	server := &ConnectNodeServer{
		buckets:                  []*Bucket{NewBucket(bucketCfg)},
		bucketIdx:                1,
		roomWorkerEnqueueTimeout: 5 * time.Millisecond,
	}
	server.initRoomWorkers(1, 1)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := server.BroadcastRoom(context.Background(), &push.BroadcastRoomReq{
					RoomID: "room-stop",
					Proto:  &proto.Proto{Op: 2, Seq: 1},
				})
				if err == nil {
					continue
				}
				if errors.Is(err, ErrWorkerQueueStopping) || errors.Is(err, ErrWorkerQueueClosed) {
					continue
				}
				if status.Code(err) == codes.ResourceExhausted {
					continue
				}
				t.Errorf("unexpected broadcast error: %v", err)
				return
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	server.Stop()
	close(stop)
	wg.Wait()

	if _, err := server.BroadcastRoom(context.Background(), &push.BroadcastRoomReq{
		RoomID: "room-final",
		Proto:  &proto.Proto{Op: 2, Seq: 9},
	}); !errors.Is(err, ErrWorkerQueueClosed) {
		t.Fatalf("expected ErrWorkerQueueClosed after stop, got %v", err)
	}
}

func TestEnqueueRejectReasonClassifiesQueueStates(t *testing.T) {
	if got := enqueueRejectReason(context.DeadlineExceeded); got != "queue_full" {
		t.Fatalf("expected queue_full for deadline, got %s", got)
	}
	if got := enqueueRejectReason(ErrWorkerQueueStopping); got != "stopping" {
		t.Fatalf("expected stopping reason, got %s", got)
	}
	if got := enqueueRejectReason(ErrWorkerQueueClosed); got != "closed" {
		t.Fatalf("expected closed reason, got %s", got)
	}
}

func TestEnqueueLeaveTaskPendingDedupReturnsStopErrorWhenQueueStopping(t *testing.T) {
	server := &ConnectNodeServer{
		leavePending:             make(map[string]struct{}),
		roomWorkerEnqueueTimeout: 10 * time.Millisecond,
	}
	server.initLeaveWorkers(1, 1)
	defer server.Stop()

	task := &leaveTask{
		key:    leaveDedupKey("room-stop", "user-stop"),
		userID: "user-stop",
		roomID: "room-stop",
	}
	server.leavePendingMu.Lock()
	server.leavePending[task.key] = struct{}{}
	server.leavePendingMu.Unlock()

	server.queueStateMu.Lock()
	server.queueState = queueStateStopping
	server.queueStateMu.Unlock()

	err := server.enqueueLeaveTask(task, true)
	if !errors.Is(err, ErrWorkerQueueStopping) {
		t.Fatalf("expected ErrWorkerQueueStopping for pending dedup during stopping, got %v", err)
	}
}

func TestRecordEnqueueFailureReportsMetricsReason(t *testing.T) {
	server := &ConnectNodeServer{}

	originalRecorder := criticalEnqueueFailureRecorder
	defer func() {
		criticalEnqueueFailureRecorder = originalRecorder
	}()

	cases := []struct {
		name       string
		source     string
		err        error
		wantReason string
	}{
		{name: "queue_full", source: "leave_queue", err: context.DeadlineExceeded, wantReason: "queue_full"},
		{name: "stopping", source: "leave_queue", err: ErrWorkerQueueStopping, wantReason: "stopping"},
		{name: "closed", source: "broadcast_queue", err: ErrWorkerQueueClosed, wantReason: "closed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotSource string
				gotReason string
				calls     int
			)
			criticalEnqueueFailureRecorder = func(source, reason string) {
				calls++
				gotSource = source
				gotReason = reason
			}

			server.recordEnqueueFailure(tc.source, tc.err)

			if calls != 1 {
				t.Fatalf("expected metrics recorder called once, got %d", calls)
			}
			if gotSource != tc.source {
				t.Fatalf("expected source %q, got %q", tc.source, gotSource)
			}
			if gotReason != tc.wantReason {
				t.Fatalf("expected reason %q, got %q", tc.wantReason, gotReason)
			}
		})
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
