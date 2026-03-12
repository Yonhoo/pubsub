package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

func newTestBucket() *Bucket {
	return NewBucket(&config.BucketConfig{
		Size:    1,
		Channel: 16,
		Room:    16,
	})
}

func TestDelRoomRemovesRoomFromBucket(t *testing.T) {
	b := newTestBucket()
	r := NewRoom("room-del")

	b.cLock.Lock()
	b.rooms[r.ID] = r
	b.cLock.Unlock()

	b.DelRoom(r)

	if got := b.Room(r.ID); got != nil {
		t.Fatalf("expected room to be deleted, got %#v", got)
	}
}

func TestDelRoomClosesRoomChannels(t *testing.T) {
	b := newTestBucket()
	r := NewRoom("room-close")
	ch := NewChannel(1, 1)

	if err := r.Put(ch); err != nil {
		t.Fatalf("put channel into room failed: %v", err)
	}

	b.cLock.Lock()
	b.rooms[r.ID] = r
	b.cLock.Unlock()

	b.DelRoom(r)

	if got := ch.Ready(); got != proto.ProtoFinish {
		t.Fatalf("expected ProtoFinish, got %#v", got)
	}
}

func BenchmarkDelRoom(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bucket := newTestBucket()
		room := NewRoom("bench-room")

		bucket.cLock.Lock()
		bucket.rooms[room.ID] = room
		bucket.cLock.Unlock()

		bucket.DelRoom(room)
	}
}

func BenchmarkDelRoomWithOneChannel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bucket := newTestBucket()
		room := NewRoom("bench-room-ch")
		ch := NewChannel(1, 1)
		if err := room.Put(ch); err != nil {
			b.Fatalf("put channel into room failed: %v", err)
		}

		bucket.cLock.Lock()
		bucket.rooms[room.ID] = room
		bucket.cLock.Unlock()

		bucket.DelRoom(room)
		_ = ch.Ready()
	}
}

// BenchmarkDelRoomReuseBucket isolates DelRoom map/lock cost by reusing objects.
func BenchmarkDelRoomReuseBucket(b *testing.B) {
	bucket := newTestBucket()
	room := NewRoom("bench-room-reuse")
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bucket.cLock.Lock()
		bucket.rooms[room.ID] = room
		bucket.cLock.Unlock()
		bucket.DelRoom(room)
	}
}

// BenchmarkDelRoomContention exercises read/write contention on Bucket.cLock.
func BenchmarkDelRoomContention(b *testing.B) {
	bucket := newTestBucket()
	rooms := make([]*Room, 64)
	for i := range rooms {
		room := NewRoom("bench-room-contention-" + string(rune('a'+(i%26))) + string(rune('A'+(i/26))))
		rooms[i] = room
		bucket.cLock.Lock()
		bucket.rooms[room.ID] = room
		bucket.cLock.Unlock()
	}

	var counter uint64
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&counter, 1)
			room := rooms[idx%uint64(len(rooms))]

			// Bias toward readers while still forcing regular write contention.
			if idx%8 == 0 {
				bucket.cLock.Lock()
				bucket.rooms[room.ID] = room
				bucket.cLock.Unlock()
				bucket.DelRoom(room)
				continue
			}

			_ = bucket.Room(room.ID)
			_ = bucket.RoomsCount()
		}
	})
}

func TestBroadcastReleasesBucketLockBeforePush(t *testing.T) {
	bucket := newTestBucket()
	ch := NewChannel(1, 1)
	ch.Key = "broadcast-lock"
	ch.Watch(1)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ch.SetServerPushWriter(func(*proto.Proto) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})

	if err := bucket.Put("", ch); err != nil {
		t.Fatalf("put channel failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		bucket.Broadcast(&proto.Proto{Op: 1}, 1)
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("broadcast never reached push path")
	}

	locked := make(chan struct{})
	go func() {
		bucket.cLock.Lock()
		close(locked)
		bucket.cLock.Unlock()
	}()

	select {
	case <-locked:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bucket.cLock remained held during push")
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not finish after releasing push")
	}
}

func TestBroadcastSnapshotPreservesRoomAndOpFiltering(t *testing.T) {
	bucket := newTestBucket()

	newPushChannel := func(key, roomID string, watchOp int32, counter *int32) *Channel {
		ch := NewChannel(1, 1)
		ch.Key = key
		ch.Watch(watchOp)
		ch.SetServerPushWriter(func(*proto.Proto) error {
			atomic.AddInt32(counter, 1)
			return nil
		})
		if err := bucket.Put(roomID, ch); err != nil {
			t.Fatalf("put channel %s failed: %v", key, err)
		}
		return ch
	}

	var matched, wrongOp, wrongRoom int32
	_ = newPushChannel("match", "room-a", 1, &matched)
	_ = newPushChannel("wrong-op", "room-a", 2, &wrongOp)
	_ = newPushChannel("wrong-room", "room-b", 1, &wrongRoom)

	bucket.Broadcast(&proto.Proto{Roomid: "room-a", Op: 1}, 1)

	if got := atomic.LoadInt32(&matched); got != 1 {
		t.Fatalf("expected matched channel to receive 1 push, got %d", got)
	}
	if got := atomic.LoadInt32(&wrongOp); got != 0 {
		t.Fatalf("expected wrong-op channel to receive 0 pushes, got %d", got)
	}
	if got := atomic.LoadInt32(&wrongRoom); got != 0 {
		t.Fatalf("expected wrong-room channel to receive 0 pushes, got %d", got)
	}
}

func newBroadcastBenchmarkBucket(channelCount int, roomID string, filtered bool) *Bucket {
	bucket := newTestBucket()
	for i := 0; i < channelCount; i++ {
		ch := NewChannel(1, 1)
		ch.Key = fmt.Sprintf("bench-broadcast-%d", i)
		ch.Watch(1)
		ch.SetServerPushWriter(func(*proto.Proto) error { return nil })

		targetRoom := roomID
		if filtered && i%2 == 1 {
			targetRoom = "other-room"
		}
		if err := bucket.Put(targetRoom, ch); err != nil {
			panic(err)
		}
	}
	return bucket
}

func BenchmarkBroadcastSnapshotParallel(b *testing.B) {
	bucket := newBroadcastBenchmarkBucket(256, "", false)
	msg := &proto.Proto{Op: 1}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucket.Broadcast(msg, 1)
		}
	})
}

func BenchmarkBroadcastSnapshotRoomFilteredParallel(b *testing.B) {
	bucket := newBroadcastBenchmarkBucket(256, "room-a", true)
	msg := &proto.Proto{Op: 1, Roomid: "room-a"}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucket.Broadcast(msg, 1)
		}
	})
}

func BenchmarkBroadcastSnapshotOptimizedParallel(b *testing.B) {
	bucket := newBroadcastBenchmarkBucket(1024, "", false)
	msg := &proto.Proto{Op: 1}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucket.Broadcast(msg, 1)
		}
	})
}

func BenchmarkBroadcastSnapshotOptimizedRoomFilteredParallel(b *testing.B) {
	bucket := newBroadcastBenchmarkBucket(1024, "room-a", true)
	msg := &proto.Proto{Op: 1, Roomid: "room-a"}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bucket.Broadcast(msg, 1)
		}
	})
}

type churnSlot struct {
	mu     sync.Mutex
	ch     *Channel
	roomID string
	baseID string
	altID  string
}

func BenchmarkBucketPutChurnParallel(b *testing.B) {
	bucket := newTestBucket()
	var seq uint64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := atomic.AddUint64(&seq, 1)
			ch := NewChannel(1, 1)
			ch.Key = fmt.Sprintf("put-churn-%d", id)
			ch.IP = "127.0.0.1"
			if err := bucket.Put("", ch); err != nil {
				b.Fatalf("put failed: %v", err)
			}
			bucket.Del(ch)
		}
	})
}

func BenchmarkBucketDelChurnParallel(b *testing.B) {
	bucket := newTestBucket()
	slots := make([]*churnSlot, 512)
	for i := range slots {
		ch := NewChannel(1, 1)
		ch.Key = fmt.Sprintf("del-churn-init-%d", i)
		ch.IP = "127.0.0.1"
		if err := bucket.Put("", ch); err != nil {
			b.Fatalf("put failed: %v", err)
		}
		slots[i] = &churnSlot{ch: ch, roomID: ""}
	}

	var seq uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := atomic.AddUint64(&seq, 1)
			slot := slots[id%uint64(len(slots))]
			slot.mu.Lock()
			bucket.Del(slot.ch)
			replacement := NewChannel(1, 1)
			replacement.Key = fmt.Sprintf("del-churn-%d", id)
			replacement.IP = "127.0.0.1"
			if err := bucket.Put(slot.roomID, replacement); err != nil {
				slot.mu.Unlock()
				b.Fatalf("replacement put failed: %v", err)
			}
			slot.ch = replacement
			slot.mu.Unlock()
		}
	})
}

func BenchmarkBucketChangeRoomChurnParallel(b *testing.B) {
	bucket := newTestBucket()
	slots := make([]*churnSlot, 512)
	for i := range slots {
		roomA := fmt.Sprintf("change-room-a-%d", i)
		roomB := fmt.Sprintf("change-room-b-%d", i)
		ch := NewChannel(1, 1)
		ch.Key = fmt.Sprintf("change-room-init-%d", i)
		ch.IP = "127.0.0.1"
		if err := bucket.Put(roomA, ch); err != nil {
			b.Fatalf("put failed: %v", err)
		}
		slots[i] = &churnSlot{ch: ch, roomID: roomA, baseID: roomA, altID: roomB}
	}

	var seq uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := atomic.AddUint64(&seq, 1)
			slot := slots[id%uint64(len(slots))]
			slot.mu.Lock()
			nextRoom := slot.altID
			if slot.roomID == slot.altID {
				nextRoom = slot.baseID
			}
			if err := bucket.ChangeRoom(nextRoom, slot.ch); err != nil {
				slot.mu.Unlock()
				b.Fatalf("change room failed: %v", err)
			}
			slot.roomID = nextRoom
			slot.mu.Unlock()
		}
	})
}
