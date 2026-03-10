package main

import (
	"testing"

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
