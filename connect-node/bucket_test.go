package main

import (
	"sync"
	"testing"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
)

func newTestBucket() *Bucket {
	return NewBucket(&config.BucketConfig{
		Channel: 32,
		Room:    32,
	})
}

func TestBucketDelRoomDeletesEntry(t *testing.T) {
	b := newTestBucket()
	room := NewRoom("room-1")

	b.cLock.Lock()
	b.rooms[room.ID] = room
	b.cLock.Unlock()

	b.DelRoom(room)

	if got := b.Room(room.ID); got != nil {
		t.Fatalf("expected room to be removed, got %#v", got)
	}
}

func TestBucketDelRoomConcurrentReadersRaceSafe(t *testing.T) {
	b := newTestBucket()
	roomID := "race-room"
	room := NewRoom(roomID)

	const iterations = 5000
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			b.cLock.Lock()
			b.rooms[roomID] = room
			b.cLock.Unlock()
			b.DelRoom(room)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = b.Room(roomID)
			_ = b.RoomsCount()
		}
	}()

	wg.Wait()
}
