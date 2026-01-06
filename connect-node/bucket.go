package main

import (
	"log"
	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"sync"
	"sync/atomic"
)

type Bucket struct {
	c     *config.BucketConfig
	cLock sync.RWMutex        // protect the channels for chs
	chs   map[string]*Channel // map sub key to a channel
	// room
	rooms       map[string]*Room // bucket room channels
	routines    []chan *push.BroadcastRoomReq
	routinesNum uint64

	ipCnts map[string]int32
}

func NewBucket(c *config.BucketConfig) (b *Bucket) {

	b = new(Bucket)
	b.chs = make(map[string]*Channel, c.Channel)
	b.ipCnts = make(map[string]int32)
	b.c = c
	b.rooms = make(map[string]*Room, c.Room)
	b.routines = make([]chan *push.BroadcastRoomReq, c.RoutineAmount)
	for i := uint64(0); i < c.RoutineAmount; i++ {
		c := make(chan *push.BroadcastRoomReq, c.RoutineSize)
		b.routines[i] = c
		go b.roomprc(c) // 启动房间广播处理 goroutine
	}
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
	log.Printf("📢 [Bucket] Broadcast 被调用: op=%d, roomId=%s, 总channels=%d", op, p.Roomid, len(b.chs))
	
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
			log.Printf("⚠️  [Bucket] Push 失败: err=%v", err)
		} else {
			matchedCount++
		}
	}

	b.cLock.RUnlock()
	log.Printf("✅ [Bucket] Broadcast 完成: 成功=%d, 跳过(op不匹配)=%d, 跳过(room不匹配)=%d", matchedCount, skippedByOp, skippedByRoom)
}

func (b *Bucket) Room(roomId string) (room *Room) {
	b.cLock.RLock()
	room = b.rooms[roomId]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) DelRoom(room *Room) {
	b.cLock.RLock()
	delete(b.rooms, room.ID)
	b.cLock.RUnlock()
	room.Close()
}

func (b *Bucket) BroadcastRoom(arg *push.BroadcastRoomReq) {
	log.Printf("🔔 [Bucket] BroadcastRoom 被调用: roomID=%s, proto=%+v", arg.RoomID, arg.Proto)
	num := atomic.AddUint64(&b.routinesNum, 1) % b.c.RoutineAmount
	log.Printf("🔔 [Bucket] 消息放入 routine %d", num)

	b.routines[num] <- arg
	log.Printf("🔔 [Bucket] 消息已放入 channel")
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

func (b *Bucket) roomprc(c chan *push.BroadcastRoomReq) {
	log.Printf("🚀 [Bucket] roomprc goroutine 已启动")
	for {
		arg := <-c
		log.Printf("📨 [Bucket] roomprc 收到广播请求: roomID=%s", arg.RoomID)
		if room := b.Room(arg.RoomID); room != nil {
			log.Printf("✅ [Bucket] 找到房间，推送消息: roomID=%s", arg.RoomID)
			room.PushMsg(arg.Proto)
		} else {
			log.Printf("❌ [Bucket] 房间不存在: roomID=%s", arg.RoomID)
		}
	}

}
