package main

import (
	"errors"
	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"sync"
	"sync/atomic"
	"time"
)

var (
	errBucketQueueFull = errors.New("bucket routine queue full")
	errBucketStopped   = errors.New("bucket routine stopped")
)

type roomBroadcastTask struct {
	RoomID string
	Protos []*protocol.Proto
}

type Bucket struct {
	c     *config.BucketConfig
	cLock sync.RWMutex        // protect the channels for chs
	chs   map[string]*Channel // map sub key to a channel
	// room
	rooms map[string]*Room // bucket room channels

	ipCnts map[string]int32

	lastPushErrLogNano  int64
	lastRoomMissLogNano int64

	snapshotPool sync.Pool

	// goim-style worker pool for room broadcast
	routines    []chan *roomBroadcastTask
	routinesNum uint64
	sw          *sharedWriteManager
	routineWG   sync.WaitGroup
	stopOnce    sync.Once
	stopped     uint32 // atomic flag: 1 = stopped
}

// NewBucket creates a bucket with N worker routines (goim comet model).
// Pass the server's sharedWriter for batch-enqueue optimization.
func NewBucket(c *config.BucketConfig, sw *sharedWriteManager) (b *Bucket) {
	b = new(Bucket)
	b.chs = make(map[string]*Channel, c.Channel)
	b.ipCnts = make(map[string]int32)
	b.c = c
	b.rooms = make(map[string]*Room, c.Room)
	b.snapshotPool.New = func() any {
		chs := make([]*Channel, 0, c.Channel)
		return &chs
	}
	b.sw = sw

	// Start RoutineAmount worker goroutines
	routineAmount := c.RoutineAmount
	if routineAmount == 0 {
		routineAmount = 32
	}
	routineSize := c.RoutineSize
	if routineSize <= 0 {
		routineSize = 1024
	}

	b.routines = make([]chan *roomBroadcastTask, routineAmount)
	for i := uint64(0); i < routineAmount; i++ {
		ch := make(chan *roomBroadcastTask, routineSize)
		b.routines[i] = ch
		b.routineWG.Add(1)
		go b.roomproc(ch)
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
		originalRoom = channel.Room.Load()
	)

	if newRoomID == "" {
		if originalRoom != nil && originalRoom.Del(channel) {
			b.DelRoom(originalRoom)
		}

		channel.Room.Store(nil)
		return
	}

	lockStart := time.Now()
	b.cLock.Lock()
	lockBlock := time.Since(lockStart)
	if newRoom, ok = b.rooms[newRoomID]; !ok {
		newRoom = NewRoom(newRoomID)
		b.rooms[newRoomID] = newRoom
	}
	b.cLock.Unlock()
	recordCriticalLockBlock("bucket", "change_room", lockBlock)

	if originalRoom != nil && originalRoom.Del(channel) {
		b.DelRoom(originalRoom)
	}

	if err = newRoom.Put(channel); err != nil {
		return
	}

	channel.Room.Store(newRoom)
	return

}

func (b *Bucket) Put(roomId string, channel *Channel) (err error) {

	var (
		room       *Room
		ok         bool
		oldChannel *Channel
		oldRoom    *Room
	)

	lockStart := time.Now()
	b.cLock.Lock()
	lockBlock := time.Since(lockStart)

	oldChannel = b.chs[channel.Key]
	b.chs[channel.Key] = channel

	if roomId != "" {
		if room, ok = b.rooms[roomId]; !ok {
			room = NewRoom(roomId)
			b.rooms[roomId] = room
		}

		channel.Room.Store(room)
	}

	b.ipCnts[channel.IP]++
	b.cLock.Unlock()
	recordCriticalLockBlock("bucket", "put", lockBlock)

	// 在锁外关闭旧连接并从其 Room 链表中移除
	if oldChannel != nil {
		oldChannel.Close()
		// 从旧 channel 的 Room 链表中移除
		if oldRoom = oldChannel.Room.Load(); oldRoom != nil {
			if oldRoom.Del(oldChannel) {
				// 如果房间为空，删除房间
				b.DelRoom(oldRoom)
			}
		}
	}

	if room != nil {
		err = room.Put(channel)
	}

	return
}

func (b *Bucket) Del(dch *Channel) {
	room := dch.Room.Load()

	lockStart := time.Now()
	b.cLock.Lock()
	lockBlock := time.Since(lockStart)

	if ch, ok := b.chs[dch.Key]; ok && ch == dch {
		delete(b.chs, ch.Key)

		// 只有当删除的确实是这个 channel 时才减少 ipCnts
		// 使用 dch.IP（被删除的连接的 IP），而不是 ch.IP
		if b.ipCnts[dch.IP] > 1 {
			b.ipCnts[dch.IP]--
		} else {
			delete(b.ipCnts, dch.IP)
		}
	}

	b.cLock.Unlock()
	recordCriticalLockBlock("bucket", "del", lockBlock)

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

func (b *Bucket) broadcastSnapshot() ([]*Channel, func()) {
	bufp, _ := b.snapshotPool.Get().(*[]*Channel)
	if bufp == nil {
		chs := make([]*Channel, 0, b.c.Channel)
		bufp = &chs
	}

	lockStart := time.Now()
	b.cLock.RLock()
	lockBlock := time.Since(lockStart)
	snapshot := (*bufp)[:0]
	if cap(snapshot) < len(b.chs) {
		snapshot = make([]*Channel, 0, len(b.chs))
	}
	for _, ch := range b.chs {
		snapshot = append(snapshot, ch)
	}
	b.cLock.RUnlock()
	recordCriticalLockBlock("bucket", "broadcast_snapshot", lockBlock)

	*bufp = snapshot
	release := func() {
		for i := range snapshot {
			snapshot[i] = nil
		}
		*bufp = snapshot[:0]
		b.snapshotPool.Put(bufp)
	}

	return snapshot, release
}

func (b *Bucket) Broadcast(p *protocol.Proto, op int32) {
	var ch *Channel
	matchedCount := 0
	skippedByOp := 0
	skippedByRoom := 0

	snapshot, release := b.broadcastSnapshot()
	defer release()

	for _, ch = range snapshot {
		if !ch.NeedPush(op) {
			skippedByOp++
			continue
		}

		// 只有当 channel 的 room 与消息的 roomId 匹配时才推送
		// 如果消息没有指定 roomId（空字符串），则广播给所有客户端
		if p.Roomid != "" {
			if r := ch.Room.Load(); r != nil && r.ID != p.Roomid {
				skippedByRoom++
				continue
			}
		}

		if err := ch.Push(p); err != nil {
			hotLogEvery(&b.lastPushErrLogNano, time.Second, "[Bucket] Push failed: err=%v", err)
		} else {
			matchedCount++
		}
	}

	hotLogf("[Bucket] Broadcast done: matched=%d skip_op=%d skip_room=%d", matchedCount, skippedByOp, skippedByRoom)
}

func (b *Bucket) Room(roomId string) (room *Room) {
	b.cLock.RLock()
	room = b.rooms[roomId]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) DelRoom(room *Room) {
	lockStart := time.Now()
	b.cLock.Lock()
	lockBlock := time.Since(lockStart)
	delete(b.rooms, room.ID)
	b.cLock.Unlock()
	recordCriticalLockBlock("bucket", "del_room", lockBlock)
	room.Close()
}

func (b *Bucket) BroadcastRoom(arg *push.BroadcastRoomReq) error {
	if arg == nil || arg.RoomID == "" || arg.Proto == nil {
		return nil
	}
	return b.BroadcastRoomBatch(&roomBroadcastTask{RoomID: arg.RoomID, Protos: []*protocol.Proto{arg.Proto}})
}

func (b *Bucket) BroadcastRoomBatch(task *roomBroadcastTask) (err error) {
	if task == nil || task.RoomID == "" || len(task.Protos) == 0 {
		return nil
	}
	if atomic.LoadUint32(&b.stopped) != 0 {
		return errBucketStopped
	}
	num := atomic.AddUint64(&b.routinesNum, 1) % uint64(len(b.routines))
	ch := b.routines[num]
	defer func() {
		if recover() != nil {
			err = errBucketStopped
		}
	}()
	select {
	case ch <- task:
		return nil
	default:
		recordCriticalDrop("bucket_room_queue", "queue_full")
		hotLogEvery(&b.lastRoomMissLogNano, 3*time.Second, "[Bucket] routine queue full: roomID=%s", task.RoomID)
		return errBucketQueueFull
	}
}

// roomproc is the worker loop ? reads from routine chan and pushes to room.
func (b *Bucket) roomproc(ch chan *roomBroadcastTask) {
	defer b.routineWG.Done()
	for arg := range ch {
		if arg == nil || arg.RoomID == "" || len(arg.Protos) == 0 {
			continue
		}
		if room := b.Room(arg.RoomID); room != nil {
			room.PushMsgBatchMany(arg.Protos, b.sw)
		} else {
			hotLogEvery(&b.lastRoomMissLogNano, 3*time.Second, "[Bucket] Room missing: roomID=%s", arg.RoomID)
		}
	}
}

// Shutdown stops all worker routines and waits for them to drain.
func (b *Bucket) Shutdown() {
	b.stopOnce.Do(func() {
		atomic.StoreUint32(&b.stopped, 1)
		for _, ch := range b.routines {
			if ch != nil {
				close(ch)
			}
		}
		b.routineWG.Wait()
	})
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
