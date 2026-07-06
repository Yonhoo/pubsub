package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"sync"
)

// 房间广播预编码器：ProtoPackageHandler 完全无状态（codec.go 验证），
// 编出的字节对所有接收者相同；这里复用一个全局实例避免每次广播分配。
var roomBroadcastEncoder = gettypkg.NewProtoPackageHandler()

type Room struct {
	ID        string
	rLock     sync.RWMutex
	next      *Channel
	drop      bool
	Online    int32 // dirty read is ok
	AllOnline int32
}

// NewRoom new a room struct, store channel room info.
func NewRoom(id string) (r *Room) {
	r = new(Room)
	r.ID = id
	r.drop = false
	r.next = nil
	r.Online = 0
	return
}

// Put put channel into the room.
func (r *Room) Put(ch *Channel) (err error) {
	r.rLock.Lock()
	if !r.drop {
		if r.next != nil {
			r.next.Prev = ch
		}
		ch.Next = r.next
		ch.Prev = nil
		r.next = ch // insert to header
		r.Online++
	} else {
		err = pkg.ErrRoomDroped
	}
	r.rLock.Unlock()
	return
}

// Del delete channel from the room.
func (r *Room) Del(ch *Channel) bool {
	r.rLock.Lock()
	if ch.Next != nil {
		// if not footer
		ch.Next.Prev = ch.Prev
	}
	if ch.Prev != nil {
		// if not header
		ch.Prev.Next = ch.Next
	} else {
		r.next = ch.Next
	}
	ch.Next = nil
	ch.Prev = nil
	r.Online--
	r.drop = r.Online == 0
	r.rLock.Unlock()
	return r.drop
}

// Push push msg to the room, if chan full discard it.
// 优化路径 v1: encode-once（同一 proto 编码一次，所有接收者复用字节）。
// 优化路径 v2: batch-enqueue（按 shard 分组，一次 chan send 给同一 shard 的所有 session，
// 从 O(房间人数) chan send → O(shard数) chan send，显著降低 channel 调度开销）。
func (r *Room) PushMsg(p *protocol.Proto) {
	r.rLock.RLock()
	if r.drop {
		r.rLock.RUnlock()
		return
	}

	// 编码一次
	data, err := roomBroadcastEncoder.Write(nil, p)
	if err != nil {
		// 编码失败回退到逐 channel 的 proto 路径
		for ch := r.next; ch != nil; ch = ch.Next {
			_ = ch.Push(p)
		}
		r.rLock.RUnlock()
		return
	}

	// 逐 channel 投递（仍是串行遍历，但已经是 encode-once + bytes 复用）
	for ch := r.next; ch != nil; ch = ch.Next {
		if perr := ch.PushBytes(data); perr == errNoBytesPushWriter {
			_ = ch.Push(p)
		}
	}
	r.rLock.RUnlock()
}

// PushMsgBatch 批量入队优化版：编码一次 + 按 shard 分组批量投递，
// 从 O(房间人数) chan send 降到 O(shard数) chan send。
// 调用方需提供 sharedWriter 实例，否则回退到 PushMsg。
func (r *Room) PushMsgBatch(p *protocol.Proto, sw *sharedWriteManager) {
	if sw == nil {
		r.PushMsg(p)
		return
	}

	r.rLock.RLock()
	if r.drop {
		r.rLock.RUnlock()
		return
	}

	// 编码一次
	data, err := roomBroadcastEncoder.Write(nil, p)
	if err != nil {
		// 编码失败回退
		for ch := r.next; ch != nil; ch = ch.Next {
			_ = ch.Push(p)
		}
		r.rLock.RUnlock()
		return
	}

	// 按 shard 分组：遍历房间内所有 channel，把 writeSessionID 按其归属 shard 分组
	shardCount := sw.ShardCount()
	groups := make([][]uint64, shardCount)
	for ch := r.next; ch != nil; ch = ch.Next {
		if ch.writeSessionID == 0 {
			// 未装载 sharedWriter（旧版或未升级）→ 回退到单投
			_ = ch.PushBytes(data)
			continue
		}
		shardID := sw.PickShardID(ch.writeSessionID)
		groups[shardID] = append(groups[shardID], ch.writeSessionID)
	}
	r.rLock.RUnlock()

	// 批量投递：每个非空 shard 一次 chan send
	for shardID, sids := range groups {
		if len(sids) > 0 {
			if err := sw.EnqueueBatch(shardID, sids, data); err != nil {
				recordCriticalDrop("shared_writer_batch", sharedWriterDropReason(err))
			}
		}
	}
}

// Close close the room.
func (r *Room) Close() {
	r.rLock.Lock()
	r.drop = true
	for ch := r.next; ch != nil; ch = ch.Next {
		ch.Close()
	}
	r.rLock.Unlock()
}

// OnlineNum the room all online.
func (r *Room) OnlineNum() int32 {
	if r.AllOnline > 0 {
		return r.AllOnline
	}
	return r.Online
}
