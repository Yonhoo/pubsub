package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

var (
	// 全局统计：signal channel 满导致的消息丢弃次数
	globalSignalDropCount int64
	// 丢弃日志专用 logger
	dropLogger *log.Logger
)

// InitDropLogger 初始化丢弃日志记录器
func InitDropLogger(logFile string) error {
	if logFile == "" {
		dropLogger = log.New(os.Stdout, "[DROP] ", log.LstdFlags)
		return nil
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// 只写入文件，不输出到控制台
	dropLogger = log.New(f, "", log.LstdFlags)
	return nil
}

// dropLog 记录丢弃日志
func dropLog(format string, v ...interface{}) {
	if dropLogger != nil {
		dropLogger.Printf(format, v...)
	} else {
		log.Printf("[DROP] "+format, v...)
	}
}

type Channel struct {
	Room           *Room
	ClientReqQueue RingPWB // 改用 RingPWB 存储 ProtoWithBuffer 指针
	signal         chan *protocol.Proto

	Next *Channel
	Prev *Channel

	Mid      int64
	Key      string
	IP       string
	watchOps map[int32]struct{}
	mutex    sync.RWMutex
	// serverPushWriter routes server-side pushes directly to shared writer shard.
	// When nil, fallback to legacy signal-queue path.
	serverPushWriter func(*protocol.Proto) error

	// 统计：本 Channel 的消息丢弃次数
	dropCount int64
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
	c.mutex.RLock()
	pushWriter := c.serverPushWriter
	c.mutex.RUnlock()

	if pushWriter != nil {
		if err = pushWriter(p); err == nil {
			return
		}
		// 直写共享写队列失败，按丢弃处理（保持非阻塞语义）
		dropCount := atomic.AddInt64(&c.dropCount, 1)
		globalDrop := atomic.AddInt64(&globalSignalDropCount, 1)
		if dropCount%100 == 1 {
			dropLog("⚠️  [Channel.Push] Shared writer enqueue failed! Key=%s, Room=%s, Err=%v, DropCount=%d, GlobalDrop=%d",
				c.Key,
				func() string {
					if c.Room != nil {
						return c.Room.ID
					}
					return "nil"
				}(),
				err,
				dropCount,
				globalDrop)
		}
		return
	}

	select {
	case c.signal <- p:
		// 成功推送
	default:
		// signal channel 满了，消息被丢弃
		err = pkg.ErrSignalFullMsgDropped

		// 增加丢弃计数
		dropCount := atomic.AddInt64(&c.dropCount, 1)
		globalDrop := atomic.AddInt64(&globalSignalDropCount, 1)

		// 记录详细日志（每 100 次丢弃打印一次，避免日志爆炸）
		if dropCount%100 == 1 {
			dropLog("⚠️  [Channel.Push] Signal channel FULL! Key=%s, Room=%s, ChannelLen=%d/%d, DropCount=%d, GlobalDrop=%d",
				c.Key,
				func() string {
					if c.Room != nil {
						return c.Room.ID
					}
					return "nil"
				}(),
				len(c.signal),
				cap(c.signal),
				dropCount,
				globalDrop)
		}
	}

	return
}

func (c *Channel) SetServerPushWriter(writer func(*protocol.Proto) error) {
	c.mutex.Lock()
	c.serverPushWriter = writer
	c.mutex.Unlock()
}

func (c *Channel) ClearServerPushWriter() {
	c.mutex.Lock()
	c.serverPushWriter = nil
	c.mutex.Unlock()
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

// GetDropCount 获取当前 Channel 的消息丢弃次数
func (c *Channel) GetDropCount() int64 {
	return atomic.LoadInt64(&c.dropCount)
}

// GetGlobalDropCount 获取全局消息丢弃次数
func GetGlobalDropCount() int64 {
	return atomic.LoadInt64(&globalSignalDropCount)
}
