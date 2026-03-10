package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"sync"
	"time"
)

type Bucket struct {
	c     *config.BucketConfig
	cLock sync.RWMutex        // protect the channels for chs
	chs   map[string]*Channel // map sub key to a channel
	// room
	rooms map[string]*Room // bucket room channels

	ipCnts map[string]int32

	lastPushErrLogNano  int64
	lastRoomMissLogNano int64
}

func NewBucket(c *config.BucketConfig) (b *Bucket) {

	b = new(Bucket)
	b.chs = make(map[string]*Channel, c.Channel)
	b.ipCnts = make(map[string]int32)
	b.c = c
	b.rooms = make(map[string]*Room, c.Room)
	return
}

func (b *Bucket) ChannelCount() int {
	return len(b.chs)
}

func (b *Bucket) RoomCount() int {
	return len(b.rooms)
}

func (b *Bucket) RoomsCount() (res map[string]int32) {
	var (
		roomID string
		room   *Room
	)

	b.cLock.RLock()

	res = make(map[string]int32)

	for roomID, room = range b.rooms {
		if room.Online > 0 {
			res[roomID] = room.Online
		}
	}

	b.cLock.RUnlock()
	return
}

// one channel change room
func (b *Bucket) ChangeRoom(newRoomID string, channel *Channel) (err error) {

	var (
		newRoom      *Room
		ok           bool
		originalRoom = channel.Room
	)

	if newRoomID == "" {
		if originalRoom != nil && originalRoom.Del(channel) {
			b.DelRoom(originalRoom)
		}

		channel.Room = nil
		return
	}

	b.cLock.Lock()
	if newRoom, ok = b.rooms[newRoomID]; !ok {
		newRoom = NewRoom(newRoomID)
		b.rooms[newRoomID] = newRoom
	}
	b.cLock.Unlock()

	if originalRoom != nil && originalRoom.Del(channel) {
		b.DelRoom(originalRoom)
	}

	if err = newRoom.Put(channel); err != nil {
		return
	}

	channel.Room = newRoom
	return

}

func (b *Bucket) Put(roomId string, channel *Channel) (err error) {

	var (
		room *Room
		ok   bool
	)

	b.cLock.Lock()

	if oldChannel := b.chs[channel.Key]; oldChannel != nil {
		oldChannel.Close()
	}

	b.chs[channel.Key] = channel

	if roomId != "" {
		if room, ok = b.rooms[roomId]; !ok {
			room = NewRoom(roomId)
			b.rooms[roomId] = room
		}

		channel.Room = room
	}

	b.ipCnts[channel.IP]++
	b.cLock.Unlock()
	if room != nil {
		err = room.Put(channel)
	}

	return
}

func (b *Bucket) Del(dch *Channel) {
	room := dch.Room

	b.cLock.Lock()

	if ch, ok := b.chs[dch.Key]; ok {
		if ch == dch {
			delete(b.chs, ch.Key)
		}

		if b.ipCnts[ch.IP] > 1 {
			b.ipCnts[ch.IP]--
		} else {
			delete(b.ipCnts, ch.IP)
		}
	}

	b.cLock.Unlock()

	if room != nil && room.Del(dch) {
		// if room channel is empty , then drop room
		b.DelRoom(room)
	}

}

func (b *Bucket) Channel(key string) (ch *Channel) {
	b.cLock.RLock()
	ch = b.chs[key]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) Broadcast(p *protocol.Proto, op int32) {
	var ch *Channel
	matchedCount := 0
	skippedByOp := 0
	skippedByRoom := 0

	b.cLock.RLock()
	for _, ch = range b.chs {
		if !ch.NeedPush(op) {
			skippedByOp++
			continue
		}

		// 只有当 channel 的 room 与消息的 roomId 匹配时才推送
		// 如果消息没有指定 roomId（空字符串），则广播给所有客户端
		if p.Roomid != "" && ch.Room != nil && ch.Room.ID != p.Roomid {
			skippedByRoom++
			continue
		}

		if err := ch.Push(p); err != nil {
			hotLogEvery(&b.lastPushErrLogNano, time.Second, "[Bucket] Push failed: err=%v", err)
		} else {
			matchedCount++
		}
	}

	b.cLock.RUnlock()
	hotLogf("[Bucket] Broadcast done: matched=%d skip_op=%d skip_room=%d", matchedCount, skippedByOp, skippedByRoom)
}

func (b *Bucket) Room(roomId string) (room *Room) {
	b.cLock.RLock()
	room = b.rooms[roomId]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) DelRoom(room *Room) {
	b.cLock.Lock()
	delete(b.rooms, room.ID)
	b.cLock.Unlock()
	room.Close()
}

func (b *Bucket) BroadcastRoom(arg *push.BroadcastRoomReq) {
	if room := b.Room(arg.RoomID); room != nil {
		room.PushMsg(arg.Proto)
	} else {
		hotLogEvery(&b.lastRoomMissLogNano, 3*time.Second, "[Bucket] Room missing: roomID=%s", arg.RoomID)
	}
}

func (b *Bucket) Rooms() (res map[string]struct{}) {

	var (
		roomID string
		room   *Room
	)

	res = make(map[string]struct{})

	b.cLock.RLock()

	for roomID, room = range b.rooms {
		if room.Online > 0 {
			res[roomID] = struct{}{}
		}
	}

	b.cLock.RUnlock()
	return

}

func (b *Bucket) UpRoomsCount(roomCountMap map[string]int32) {
	var (
		roomID string
		room   *Room
	)

	b.cLock.RLock()

	for roomID, room = range b.rooms {
		room.AllOnline = roomCountMap[roomID]
	}

	b.cLock.RUnlock()

}
