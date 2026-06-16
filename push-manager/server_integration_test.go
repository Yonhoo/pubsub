//go:build integration
// +build integration

package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	protocol "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type roomClientCounter struct {
	count atomic.Int32
}

type fakeConnectNodeServer struct {
	push.UnimplementedCometServer
	mu    sync.RWMutex
	rooms map[string][]*roomClientCounter
}

func newFakeConnectNodeServer() *fakeConnectNodeServer {
	return &fakeConnectNodeServer{
		rooms: make(map[string][]*roomClientCounter),
	}
}

func (f *fakeConnectNodeServer) JoinRoom(roomID string, client *roomClientCounter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rooms[roomID] = append(f.rooms[roomID], client)
}

func (f *fakeConnectNodeServer) BroadcastRoom(ctx context.Context, req *push.BroadcastRoomReq) (*push.BroadcastRoomReply, error) {
	f.mu.RLock()
	clients := append([]*roomClientCounter(nil), f.rooms[req.RoomID]...)
	f.mu.RUnlock()

	for _, client := range clients {
		client.count.Add(1)
	}
	return &push.BroadcastRoomReply{}, nil
}

func (f *fakeConnectNodeServer) Broadcast(ctx context.Context, req *push.BroadcastReq) (*push.BroadcastReply, error) {
	if req.GetProto().GetRoomid() == "" {
		return &push.BroadcastReply{}, nil
	}
	_, err := f.BroadcastRoom(ctx, &push.BroadcastRoomReq{
		RoomID: req.GetProto().GetRoomid(),
		Proto:  req.GetProto(),
	})
	return &push.BroadcastReply{}, err
}

func startFakeConnectNode(t *testing.T, svc *fakeConnectNodeServer) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake connect-node: %v", err)
	}
	grpcServer := grpc.NewServer()
	push.RegisterCometServer(grpcServer, svc)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = grpcServer.Serve(lis)
	}()

	cleanup := func() {
		grpcServer.Stop()
		_ = lis.Close()
		<-done
	}
	return lis.Addr().String(), cleanup
}

func startPushManagerForIntegration(t *testing.T, endpoints []string) (broadcast.PushServerClient, func()) {
	t.Helper()

	managerCtx, managerCancel := context.WithCancel(context.Background())
	manager := &PushManagerServer{
		broadCastClientMap: make(map[string]*BroadcastClient),
		ctx:                managerCtx,
		cancel:             managerCancel,
	}
	manager.createBroadcastClient(endpoints)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen push-manager: %v", err)
	}
	grpcServer := grpc.NewServer()
	broadcast.RegisterPushServerServer(grpcServer, manager)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial push-manager: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		manager.cleanupAllClients()
		managerCancel()
		<-done
	}
	return broadcast.NewPushServerClient(conn), cleanup
}

func waitForAllClients(t *testing.T, clients []*roomClientCounter, want int32) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allReceived := true
		for _, client := range clients {
			if client.count.Load() < want {
				allReceived = false
				break
			}
		}
		if allReceived {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	minCount := int32(1<<31 - 1)
	maxCount := int32(0)
	for _, client := range clients {
		count := client.count.Load()
		if count < minCount {
			minCount = count
		}
		if count > maxCount {
			maxCount = count
		}
	}
	t.Fatalf("expected every room client to receive %d messages, min=%d max=%d", want, minCount, maxCount)
}

func TestIntegrationBroadcastToRoomFanOutThreeConnectNodesHundredClients(t *testing.T) {
	const (
		connectNodeCount = 3
		clientCount      = 100
		messageCount     = 100
		roomID           = "room-integration"
	)

	connectNodes := make([]*fakeConnectNodeServer, 0, connectNodeCount)
	endpoints := make([]string, 0, connectNodeCount)
	cleanups := make([]func(), 0, connectNodeCount)
	for i := 0; i < connectNodeCount; i++ {
		node := newFakeConnectNodeServer()
		endpoint, cleanup := startFakeConnectNode(t, node)
		connectNodes = append(connectNodes, node)
		endpoints = append(endpoints, endpoint)
		cleanups = append(cleanups, cleanup)
	}
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	clients := make([]*roomClientCounter, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		client := &roomClientCounter{}
		connectNodes[i%connectNodeCount].JoinRoom(roomID, client)
		clients = append(clients, client)
	}

	pushClient, cleanupPushManager := startPushManagerForIntegration(t, endpoints)
	defer cleanupPushManager()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for i := 0; i < messageCount; i++ {
		<-ticker.C
		resp, err := pushClient.BroadcastToRoom(context.Background(), &broadcast.BroadCastRoomReq{
			RoomId: roomID,
			Proto: &protocol.Proto{
				Op:     2,
				Seq:    int32(i + 1),
				Roomid: roomID,
				Body:   []byte(fmt.Sprintf("msg-%03d", i+1)),
			},
		})
		if err != nil {
			t.Fatalf("broadcast message %d: %v", i+1, err)
		}
		if resp.GetCode() != "0" {
			t.Fatalf("broadcast message %d got non-ok response: %+v", i+1, resp)
		}
	}

	waitForAllClients(t, clients, messageCount)
}
