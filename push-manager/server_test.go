package main

import (
	"context"
	"testing"

	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	protocol "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"google.golang.org/grpc"
)

type stubCometClient struct{}

func (stubCometClient) PushMsg(context.Context, *push.PushMsgReq, ...grpc.CallOption) (*push.PushMsgReply, error) {
	return &push.PushMsgReply{}, nil
}

func (stubCometClient) Broadcast(context.Context, *push.BroadcastReq, ...grpc.CallOption) (*push.BroadcastReply, error) {
	return &push.BroadcastReply{}, nil
}

func (stubCometClient) BroadcastRoom(context.Context, *push.BroadcastRoomReq, ...grpc.CallOption) (*push.BroadcastRoomReply, error) {
	return &push.BroadcastRoomReply{}, nil
}

func (stubCometClient) Rooms(context.Context, *push.RoomsReq, ...grpc.CallOption) (*push.RoomsReply, error) {
	return &push.RoomsReply{}, nil
}

func newTestPushManagerServer(clientCount int, queueSize int) (*PushManagerServer, []*BroadcastClient) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &PushManagerServer{
		broadCastClientMap: make(map[string]*BroadcastClient),
		ctx:                ctx,
		cancel:             cancel,
	}
	clients := make([]*BroadcastClient, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		clientCtx, clientCancel := context.WithCancel(ctx)
		client := &BroadcastClient{
			serverID:      string(rune('a' + i)),
			client:        stubCometClient{},
			broadcastChan: make(chan *broadcastTask, queueSize),
			ctx:           clientCtx,
			cancel:        clientCancel,
		}
		s.broadCastClientMap[client.serverID] = client
		clients = append(clients, client)
	}
	return s, clients
}

func TestBroadcastToRoomFansOutRoomTaskToEveryConnectNode(t *testing.T) {
	server, clients := newTestPushManagerServer(3, 1)
	defer server.cancel()

	msg := &protocol.Proto{Op: 2, Seq: 7}
	reply, err := server.BroadcastToRoom(context.Background(), &broadcast.BroadCastRoomReq{
		RoomId: "room-fanout",
		Proto:  msg,
	})
	if err != nil {
		t.Fatalf("broadcast to room: %v", err)
	}
	if reply.GetCode() != "0" {
		t.Fatalf("expected ok reply, got %+v", reply)
	}

	for _, client := range clients {
		select {
		case task := <-client.broadcastChan:
			if task.roomReq == nil {
				t.Fatalf("client %s got non-room task: %+v", client.serverID, task)
			}
			if task.roomReq.RoomID != "room-fanout" {
				t.Fatalf("client %s got room %q", client.serverID, task.roomReq.RoomID)
			}
			if task.roomReq.Proto != msg {
				t.Fatalf("client %s got different proto pointer", client.serverID)
			}
			if task.proto != nil {
				t.Fatalf("client %s should not get legacy broadcast task", client.serverID)
			}
		default:
			t.Fatalf("client %s did not receive fan-out task", client.serverID)
		}
	}
}

func TestLegacyBroadcastWithRoomIDRoutesToRoomFanOut(t *testing.T) {
	server, clients := newTestPushManagerServer(2, 1)
	defer server.cancel()

	msg := &protocol.Proto{Op: 2, Seq: 8, Roomid: "room-legacy"}
	server.EnqueueBroadcastMsg(&broadcast.BroadCastReq{Proto: msg})

	for _, client := range clients {
		select {
		case task := <-client.broadcastChan:
			if task.roomReq == nil {
				t.Fatalf("client %s got non-room task: %+v", client.serverID, task)
			}
			if task.roomReq.RoomID != "room-legacy" {
				t.Fatalf("client %s got room %q", client.serverID, task.roomReq.RoomID)
			}
			if task.roomReq.Proto != msg {
				t.Fatalf("client %s got different proto pointer", client.serverID)
			}
		default:
			t.Fatalf("client %s did not receive legacy room fan-out task", client.serverID)
		}
	}
}

func TestLegacyBroadcastWithoutRoomIDUsesGlobalBroadcastTask(t *testing.T) {
	server, clients := newTestPushManagerServer(1, 1)
	defer server.cancel()

	msg := &protocol.Proto{Op: 3, Seq: 9}
	server.EnqueueBroadcastMsg(&broadcast.BroadCastReq{Proto: msg})

	select {
	case task := <-clients[0].broadcastChan:
		if task.proto == nil {
			t.Fatalf("expected global broadcast task, got %+v", task)
		}
		if task.proto.Proto != msg {
			t.Fatalf("got different proto pointer")
		}
		if task.proto.ProtoOp != msg.Op {
			t.Fatalf("expected proto op %d, got %d", msg.Op, task.proto.ProtoOp)
		}
		if task.roomReq != nil {
			t.Fatalf("global broadcast should not include room request")
		}
	default:
		t.Fatal("client did not receive global broadcast task")
	}
}
