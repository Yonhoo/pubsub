package main

import (
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
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
// 优化路径：把同一 proto 在循环外编码一次（handler.Write 无 session 依赖），
// 然后按字节复用给房间内每个接收者，跳过 N 次重复编码。
// 任何 channel 未装载 bytes-writer 或入队失败，自动回退到 proto 路径。
func (r *Room) PushMsg(p *protocol.Proto) {
	r.rLock.RLock()
	// 如果房间已标记为 drop，不再推送消息
	if r.drop {
		r.rLock.RUnlock()
		return
	}

	// 编码一次（提前完成，避免持锁时间被编码成本拖长）
	data, err := roomBroadcastEncoder.Write(nil, p)
	if err != nil {
		// 编码失败回退到逐 channel 的 proto 路径（其内部会再次尝试编码）
		for ch := r.next; ch != nil; ch = ch.Next {
			_ = ch.Push(p)
		}
		r.rLock.RUnlock()
		return
	}

	for ch := r.next; ch != nil; ch = ch.Next {
		// 优先走预编码字节路径；若 channel 未装载（旧/未升级）则回退到 proto 路径
		if perr := ch.PushBytes(data); perr == errNoBytesPushWriter {
			_ = ch.Push(p)
		}
	}
	r.rLock.RUnlock()
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
