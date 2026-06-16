//go:build integration
// +build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

var integrationSessionSeq uint64

type fakeControllerService struct {
	controller.UnimplementedControllerServiceServer
	joinHook  func(context.Context, *controller.JoinRoomRequest) (*controller.JoinRoomResponse, error)
	leaveHook func(context.Context, *controller.LeaveRoomRequest) (*controller.LeaveRoomResponse, error)
}

func (f *fakeControllerService) JoinRoom(ctx context.Context, req *controller.JoinRoomRequest) (*controller.JoinRoomResponse, error) {
	if f.joinHook != nil {
		return f.joinHook(ctx, req)
	}
	return &controller.JoinRoomResponse{Success: true, Message: "ok"}, nil
}

func (f *fakeControllerService) LeaveRoom(ctx context.Context, req *controller.LeaveRoomRequest) (*controller.LeaveRoomResponse, error) {
	if f.leaveHook != nil {
		return f.leaveHook(ctx, req)
	}
	return &controller.LeaveRoomResponse{}, nil
}

func (f *fakeControllerService) GetRoomInfo(context.Context, *controller.GetRoomInfoRequest) (*controller.GetRoomInfoResponse, error) {
	return &controller.GetRoomInfoResponse{}, nil
}

func (f *fakeControllerService) GetUserNode(context.Context, *controller.GetUserNodeRequest) (*controller.GetUserNodeResponse, error) {
	return &controller.GetUserNodeResponse{}, nil
}

func (f *fakeControllerService) GetRoomStats(context.Context, *controller.GetRoomStatsRequest) (*controller.GetRoomStatsResponse, error) {
	return &controller.GetRoomStatsResponse{}, nil
}

func newBufconnControllerClient(t *testing.T, svc controller.ControllerServiceServer) (controller.ControllerServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	controller.RegisterControllerServiceServer(grpcServer, svc)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("dial bufconn failed: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
	}

	return controller.NewControllerServiceClient(conn), cleanup
}

func newIntegrationServer(client controller.ControllerServiceClient) *ConnectNodeServer {
	bucketCfg := &config.BucketConfig{
		Size:                       1,
		Channel:                    64,
		Room:                       64,
		RoutineAmount:              1,
		RoutineSize:                64,
		RoutineBackpressureTimeout: 20 * time.Millisecond,
	}
	server := &ConnectNodeServer{
		nodeID:                   "node-it",
		controllerClient:         client,
		buckets:                  []*Bucket{NewBucket(bucketCfg)},
		bucketIdx:                1,
		roomWorkerEnqueueTimeout: 20 * time.Millisecond,
		leaveRetryDelay:          20 * time.Millisecond,
		leaveMaxAttempts:         3,
		leavePending:             make(map[string]struct{}),
		queueState:               queueStateRunning,
		stopDrainTimeout:         time.Second,
	}
	server.sharedWriter = newSharedWriteManager(1, 1, writeBatchMaxBytes, 5*time.Millisecond, 128)
	server.sharedWriter.Start()
	server.initLeaveWorkers(1, 64)
	server.initRoomWorkers(1, 64)
	return server
}

func registerIntegrationHandler(t *testing.T, server *ConnectNodeServer, userID string) (*ProtoMessageHandler, *mockGettySession) {
	t.Helper()
	sessionID := atomic.AddUint64(&integrationSessionSeq, 1)
	h := &ProtoMessageHandler{
		server:              server,
		channel:             NewChannel(32, 32),
		protoPackageHandler: gettypkg.NewProtoPackageHandler(),
		writeSessionID:      sessionID,
	}
	// Note: In tests, Key is set directly. In production, SetKeyIP is called with roomId:userId composite key.
	// Tests that need the composite key should manually update it after setting handler.roomId.
	h.channel.Key = userID
	session := &mockGettySession{record: true}
	if err := server.sharedWriter.Register(sessionID, session, h.protoPackageHandler, h); err != nil {
		t.Fatalf("register shared writer session failed: %v", err)
	}
	h.channel.SetServerPushWriter(func(p *proto.Proto) error {
		return h.enqueueSharedWrite(p, "broadcast")
	})
	return h, session
}

func waitEventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func requireExtendedSuite(t *testing.T) {
	t.Helper()
	if os.Getenv("CONNECT_NODE_IT_SUITE") != "extended" {
		t.Skip("set CONNECT_NODE_IT_SUITE=extended to run extended integration suite")
	}
}

func TestIntegrationJoinSyncConfirmationWithBufconnController(t *testing.T) {
	releaseJoin := make(chan struct{})
	joinCalled := make(chan struct{}, 1)
	client, cleanup := newBufconnControllerClient(t, &fakeControllerService{
		joinHook: func(ctx context.Context, req *controller.JoinRoomRequest) (*controller.JoinRoomResponse, error) {
			select {
			case joinCalled <- struct{}{}:
			default:
			}
			<-releaseJoin
			return &controller.JoinRoomResponse{Success: true, Message: "joined"}, nil
		},
	})
	defer cleanup()

	server := newIntegrationServer(client)
	defer server.Stop()
	h, session := registerIntegrationHandler(t, server, "join-user")
	defer func() {
		_ = server.sharedWriter.Unregister(h.writeSessionID)
	}()

	req := &proto.Proto{Op: 1, Seq: 7, Roomid: "room-it", Userid: "join-user", Body: []byte("join-user")}
	done := make(chan error, 1)
	go func() {
		done <- h.processClientRequest(session, req)
	}()

	select {
	case <-joinCalled:
	case <-time.After(time.Second):
		t.Fatal("expected JoinRoom to be invoked")
	}
	if got := len(session.writeBatches()); got != 0 {
		t.Fatalf("expected no websocket write before join confirmation, got %d", got)
	}
	select {
	case err := <-done:
		t.Fatalf("expected join path blocked before controller reply, got %v", err)
	default:
	}

	close(releaseJoin)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected join to succeed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected join confirmation to complete")
	}

	waitEventually(t, time.Second, func() bool {
		return len(session.writeBatches()) >= 1
	}, "expected ack write after join confirmation")
	room := h.channel.Room.Load()
	if room == nil || room.ID != "room-it" {
		t.Fatalf("expected room-it binding after join, got %#v", room)
	}
	if !h.channel.NeedPush(2) {
		t.Fatal("expected join path to watch op=2")
	}
}

func TestIntegrationLeaveAsyncUnbindAndRetry(t *testing.T) {
	var attempts atomic.Int32
	client, cleanup := newBufconnControllerClient(t, &fakeControllerService{
		leaveHook: func(ctx context.Context, req *controller.LeaveRoomRequest) (*controller.LeaveRoomResponse, error) {
			if attempts.Add(1) < 2 {
				return nil, errors.New("retry")
			}
			return &controller.LeaveRoomResponse{}, nil
		},
	})
	defer cleanup()

	server := newIntegrationServer(client)
	defer server.Stop()
	h, _ := registerIntegrationHandler(t, server, "leave-user")
	defer func() {
		_ = server.sharedWriter.Unregister(h.writeSessionID)
	}()

	bucket := server.buckets[0]
	h.bucket = bucket
	h.auth = true
	h.roomId = "room-leave"
	h.clientId = "leave-user"
	// Update channel.Key to use composite key (roomId:userId) to match production behavior
	h.channel.Key = "room-leave:leave-user"
	if err := bucket.Put("room-leave", h.channel); err != nil {
		t.Fatalf("bucket put failed: %v", err)
	}

	start := time.Now()
	h.cleanupUser()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected cleanupUser to return quickly, took %v", elapsed)
	}
	if got := bucket.ChannelCount(); got != 0 {
		t.Fatalf("expected local bucket detach before leave rpc completion, got %d", got)
	}
	if room := h.channel.Room.Load(); room != nil {
		t.Fatalf("expected local room cleared, got %#v", room)
	}

	waitEventually(t, time.Second, func() bool {
		return attempts.Load() == 2
	}, "expected leave retry to run and eventually succeed")
}

func TestIntegrationSharedWriterQueueFullFailureSemantic(t *testing.T) {
	manager := newSharedWriteManager(1, 1, writeBatchMaxBytes, time.Second, 1)
	h := &ProtoMessageHandler{
		server:         &ConnectNodeServer{sharedWriter: manager},
		writeSessionID: 99,
	}
	shard := manager.pickShard(h.writeSessionID)
	shard.in <- writeEvent{kind: writeEventEnqueue, sessionID: h.writeSessionID, msg: &proto.Proto{Op: 9, Seq: 1}}

	err := h.writeProto(nil, &proto.Proto{Op: 9, Seq: 2}, "response")
	if !errors.Is(err, errSharedWriterQueueFull) {
		t.Fatalf("expected errSharedWriterQueueFull, got %v", err)
	}
	if got := atomic.LoadUint64(&h.batchEnqueueQueueFull); got != 1 {
		t.Fatalf("expected queue-full counter=1, got %d", got)
	}
}

func TestIntegrationBroadcastRoomOpFiltering(t *testing.T) {
	server := newIntegrationServer(nil)
	defer server.Stop()
	bucket := server.buckets[0]

	newPushChannel := func(key, roomID string, watchOp int32, counter *int32) *Channel {
		ch := NewChannel(8, 8)
		ch.Key = key
		ch.Watch(watchOp)
		ch.SetServerPushWriter(func(*proto.Proto) error {
			atomic.AddInt32(counter, 1)
			return nil
		})
		if err := bucket.Put(roomID, ch); err != nil {
			t.Fatalf("put channel failed: %v", err)
		}
		return ch
	}

	var matched, wrongOp, wrongRoom int32
	_ = newPushChannel("room-a:match", "room-a", 2, &matched)
	_ = newPushChannel("room-a:wrong-op", "room-a", 3, &wrongOp)
	_ = newPushChannel("room-b:wrong-room", "room-b", 2, &wrongRoom)

	_, err := server.Broadcast(context.Background(), &push.BroadcastReq{
		ProtoOp: 2,
		Proto:   &proto.Proto{Roomid: "room-a", Op: 2},
	})
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	waitEventually(t, time.Second, func() bool {
		return atomic.LoadInt32(&matched) == 1
	}, "expected matched channel to receive broadcast")
	if got := atomic.LoadInt32(&wrongOp); got != 0 {
		t.Fatalf("expected wrong-op channel to receive 0 messages, got %d", got)
	}
	if got := atomic.LoadInt32(&wrongRoom); got != 0 {
		t.Fatalf("expected wrong-room channel to receive 0 messages, got %d", got)
	}
}

func TestIntegrationExtendedStopEnqueueRaceStableErrors(t *testing.T) {
	requireExtendedSuite(t)

	server := newIntegrationServer(nil)
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
					"it-stop-user",
					"it-stop-room",
				)
				if err == nil {
					continue
				}
				if errors.Is(err, ErrWorkerQueueStopping) || errors.Is(err, ErrWorkerQueueClosed) || errors.Is(err, context.DeadlineExceeded) {
					continue
				}
				t.Errorf("worker %d got unexpected enqueue error: %v", worker, err)
				return
			}
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	server.Stop()
	close(stop)
	wg.Wait()

	err := server.EnqueueLeave("it-stop-user-final", "it-stop-room")
	if !errors.Is(err, ErrWorkerQueueClosed) {
		t.Fatalf("expected ErrWorkerQueueClosed after stop, got %v", err)
	}
}

func TestIntegrationQueueStateErrorStatusCodeCompatibility(t *testing.T) {
	st := status.Convert(ErrWorkerQueueStopping)
	if st.Code() != codes.Unavailable {
		t.Fatalf("expected unavailable status code, got %s", st.Code())
	}
}

func startRealConnectNodeGRPC(t *testing.T, server *ConnectNodeServer) (push.CometClient, string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen real connect-node grpc: %v", err)
	}
	grpcServer := grpc.NewServer()
	push.RegisterCometServer(grpcServer, server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.Dial(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial real connect-node grpc: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		<-done
	}
	return push.NewCometClient(conn), lis.Addr().String(), cleanup
}

func countWrittenPackages(session *mockGettySession) int {
	total := 0
	for _, batch := range session.writeBatches() {
		total += len(batch)
	}
	return total
}

func waitForRealRoomClients(t *testing.T, sessions []*mockGettySession, want int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allReceived := true
		for _, session := range sessions {
			if countWrittenPackages(session) < want {
				allReceived = false
				break
			}
		}
		if allReceived {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	minCount := int(^uint(0) >> 1)
	maxCount := 0
	for _, session := range sessions {
		count := countWrittenPackages(session)
		if count < minCount {
			minCount = count
		}
		if count > maxCount {
			maxCount = count
		}
	}
	t.Fatalf("expected every real room client to receive %d packages, min=%d max=%d", want, minCount, maxCount)
}

func TestIntegrationRealConnectNodeRoomBroadcastThreeNodesHundredClients(t *testing.T) {
	const (
		connectNodeCount = 3
		clientCount      = 100
		messageCount     = 100
		roomID           = "room-real-chain"
	)

	servers := make([]*ConnectNodeServer, 0, connectNodeCount)
	clients := make([]push.CometClient, 0, connectNodeCount)
	cleanups := make([]func(), 0, connectNodeCount)
	for i := 0; i < connectNodeCount; i++ {
		server := newIntegrationServer(nil)
		server.nodeID = fmt.Sprintf("real-connect-node-%d", i+1)
		client, endpoint, cleanup := startRealConnectNodeGRPC(t, server)
		t.Logf("[e2e] real connect-node started node=%s endpoint=%s roomWorkers=%d buckets=%d",
			server.nodeID, endpoint, server.roomWorkerNum, len(server.buckets))
		servers = append(servers, server)
		clients = append(clients, client)
		cleanups = append(cleanups, cleanup)
	}
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		for _, server := range servers {
			server.Stop()
		}
	}()

	sessions := make([]*mockGettySession, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		server := servers[i%connectNodeCount]
		userID := fmt.Sprintf("real-client-%03d", i+1)
		handler, session := registerIntegrationHandler(t, server, userID)
		handler.channel.Watch(2)
		handler.bucket = server.buckets[0]
		handler.roomId = roomID
		handler.clientId = userID
		// Update channel.Key to use composite key (roomId:userId) to match production behavior
		handler.channel.Key = roomID + ":" + userID
		handler.auth = true
		if err := handler.bucket.Put(roomID, handler.channel); err != nil {
			t.Fatalf("put real client into room: %v", err)
		}
		t.Cleanup(func() {
			_ = server.sharedWriter.Unregister(handler.writeSessionID)
		})
		sessions = append(sessions, session)
	}
	t.Logf("[e2e] joined %d real clients into room=%s across %d connect-nodes", clientCount, roomID, connectNodeCount)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	start := time.Now()
	for i := 0; i < messageCount; i++ {
		<-ticker.C
		req := &push.BroadcastRoomReq{
			RoomID: roomID,
			Proto: &proto.Proto{
				Op:     2,
				Seq:    int32(i + 1),
				Roomid: roomID,
			},
		}
		for nodeIdx, client := range clients {
			if _, err := client.BroadcastRoom(context.Background(), req); err != nil {
				t.Fatalf("broadcast seq=%d to connect-node-%d: %v", i+1, nodeIdx+1, err)
			}
		}
	}
	t.Logf("[e2e] sent %d room messages to %d real connect-nodes in %v", messageCount, connectNodeCount, time.Since(start))

	waitForRealRoomClients(t, sessions, messageCount)
	t.Logf("[e2e] verified room=%s: all %d real clients received %d messages", roomID, clientCount, messageCount)
}
