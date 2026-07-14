package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
)

func TestRoomFanoutAggregatorBatchesBeforeBucketTraversal(t *testing.T) {
	bucketCfg := &config.BucketConfig{Size: 1, Channel: 8, Room: 8, RoutineAmount: 1, RoutineSize: 8}
	bucket := NewBucket(bucketCfg, nil)
	room := NewRoom("room-batch")
	bucket.cLock.Lock()
	bucket.rooms[room.ID] = room
	bucket.cLock.Unlock()

	var pushes atomic.Int32
	for i := 0; i < 2; i++ {
		ch := NewChannel(8, 8)
		ch.Key = "room-batch-user"
		ch.SetServerPushWriter(func(*proto.Proto) error {
			pushes.Add(1)
			return nil
		})
		if err := room.Put(ch); err != nil {
			t.Fatalf("put channel failed: %v", err)
		}
	}

	server := &ConnectNodeServer{buckets: []*Bucket{bucket}, bucketIdx: 1}
	server.queueState = queueStateRunning
	server.roomFanoutAggregator = newRoomFanoutAggregatorManager(server, &config.RoomFanoutAggregatorConfig{BatchSize: 2, FlushInterval: time.Second, QueueSize: 8})
	defer server.roomFanoutAggregator.Stop()

	if _, err := server.BroadcastRoom(context.Background(), &push.BroadcastRoomReq{RoomID: "room-batch", Proto: &proto.Proto{Op: 2, Seq: 1}}); err != nil {
		t.Fatalf("first broadcast failed: %v", err)
	}
	if _, err := server.BroadcastRoom(context.Background(), &push.BroadcastRoomReq{RoomID: "room-batch", Proto: &proto.Proto{Op: 2, Seq: 2}}); err != nil {
		t.Fatalf("second broadcast failed: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pushes.Load() == 4 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected 4 pushes after aggregated flush, got %d", pushes.Load())
}

func TestRoomBroadcastStripsRouteFieldsWhenEnabled(t *testing.T) {
	old := stripRoomBroadcastRouteFields
	stripRoomBroadcastRouteFields = true
	defer func() { stripRoomBroadcastRouteFields = old }()

	room := NewRoom("very-long-routing-room-id")
	ch := NewChannel(8, 8)
	var captured []byte
	ch.SetServerPushBytesWriter(func(data []byte) error {
		captured = append([]byte(nil), data...)
		return nil
	})
	if err := room.Put(ch); err != nil {
		t.Fatalf("put channel failed: %v", err)
	}

	room.PushMsg(&proto.Proto{Op: 2, Seq: 7, Roomid: room.ID, Userid: "routing-user", Body: []byte("payload")})
	if len(captured) == 0 {
		t.Fatal("expected encoded broadcast frame")
	}

	decoded, _, err := roomBroadcastEncoder.Read(nil, captured)
	if err != nil {
		t.Fatalf("decode broadcast frame: %v", err)
	}
	withBuffer, ok := decoded.(*gettypkg.ProtoWithBuffer)
	if !ok {
		t.Fatalf("expected ProtoWithBuffer, got %T", decoded)
	}
	defer withBuffer.Release()
	got := withBuffer.Proto
	if got.Roomid != "" || got.Userid != "" {
		t.Fatalf("expected route fields stripped, got room=%q user=%q", got.Roomid, got.Userid)
	}
	if string(got.Body) != "payload" || got.Op != 2 || got.Seq != 7 {
		t.Fatalf("unexpected payload after strip: op=%d seq=%d body=%q", got.Op, got.Seq, string(got.Body))
	}
}

func TestRoomShardGroupsCacheInvalidatesOnMembershipChange(t *testing.T) {
	room := NewRoom("cache-room")
	sw := newSharedWriteManager(4, 8, 1024, time.Hour, 64)
	defer sw.Stop()

	ch1 := NewChannel(8, 8)
	ch1.writeSessionID = 1
	if err := room.Put(ch1); err != nil {
		t.Fatalf("put first channel failed: %v", err)
	}

	room.PushMsgBatchMany([]*proto.Proto{{Op: 2, Seq: 1}, {Op: 2, Seq: 2}}, sw)
	if room.shardGroupsDirty {
		t.Fatal("expected shard group cache to be clean after first batch push")
	}

	ch2 := NewChannel(8, 8)
	ch2.writeSessionID = 2
	if err := room.Put(ch2); err != nil {
		t.Fatalf("put second channel failed: %v", err)
	}
	if !room.shardGroupsDirty {
		t.Fatal("expected put to mark shard group cache dirty")
	}

	room.PushMsgBatchMany([]*proto.Proto{{Op: 2, Seq: 3}, {Op: 2, Seq: 4}}, sw)
	if room.shardGroupsDirty {
		t.Fatal("expected shard group cache to be clean after rebuild")
	}

	room.Del(ch2)
	if !room.shardGroupsDirty {
		t.Fatal("expected del to mark shard group cache dirty")
	}
}
