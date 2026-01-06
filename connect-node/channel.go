package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"sync"
)

type Channel struct {
	Room           *Room
	ClientReqQueue Ring
	signal         chan *protocol.Proto

	Next *Channel
	Prev *Channel

	Mid      int64
	Key      string
	IP       string
	watchOps map[int32]struct{}
	mutex    sync.RWMutex
}

func NewChannel(cli, svr int) *Channel {
	c := new(Channel)

	c.ClientReqQueue.Init(cli)

	c.signal = make(chan *protocol.Proto, svr)

	c.watchOps = make(map[int32]struct{})
	return c
}

// watch is sub channel
func (c *Channel) Watch(accepts ...int32) {
	c.mutex.Lock()

	for _, op := range accepts {
		c.watchOps[op] = struct{}{}
	}

	c.mutex.Unlock()
}

func (c *Channel) UnWatch(accepts ...int32) {
	c.mutex.Lock()

	for _, op := range accepts {
		delete(c.watchOps, op)
	}

	c.mutex.Unlock()
}

func (c *Channel) NeedPush(op int32) bool {
	c.mutex.RLock()

	if _, ok := c.watchOps[op]; ok {
		c.mutex.RUnlock()
		return true
	}
	c.mutex.RUnlock()
	return false

}

func (c *Channel) Push(p *protocol.Proto) (err error) {
	select {
	case c.signal <- p:
	default:
		err = pkg.ErrSignalFullMsgDropped
	}

	return
}

func (c *Channel) Ready() *protocol.Proto {
	return <-c.signal
}

func (c *Channel) Signal() {
	c.signal <- proto.ProtoReady
}

func (c *Channel) Close() {
	c.signal <- proto.ProtoFinish
}
