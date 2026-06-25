package main

import (
	"errors"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

var (
	// 全局统计：业务 push 因非阻塞语义被丢弃的次数
	globalPushDropCount int64
	// 全局统计：ready signal 因通道已满被合并的次数
	globalSignalDropCount int64
	// 丢弃日志专用 logger
	dropLogger *log.Logger
	// PushBytes 在 channel 未装载 bytes-writer 时返回，调用方应回退到 proto 路径
	errNoBytesPushWriter = errors.New("channel has no bytes push writer")
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
	// Room 是当前 channel 关联的房间。原子指针，避免 Bucket 锁外读写 race。
	// 写入：Bucket.Put / ChangeRoom（持有 Bucket.cLock 时）和 cleanupUser（无锁）。
	// 读取：Bucket.Broadcast snapshot 后（无锁）、Channel.Push 日志路径（无锁）。
	Room           atomic.Pointer[Room]
	ClientReqQueue RingPWB // 改用 RingPWB 存储 ProtoWithBuffer 指针
	signal         chan *protocol.Proto
	ready          chan struct{}
	done           chan struct{}

	Next *Channel
	Prev *Channel

	Mid int64
	// Key 和 IP 在 auth 阶段设置一次，之后视为只读。
	// 使用 setKeyIPOnce 确保只设置一次，避免 race。
	Key          string
	IP           string
	setKeyIPOnce sync.Once
	watchOps     map[int32]struct{}
	mutex        sync.RWMutex
	// serverPushWriter routes server-side pushes directly to shared writer shard.
	// When nil, fallback to legacy signal-queue path.
	serverPushWriter func(*protocol.Proto) error
	// serverPushBytesWriter is the broadcast fast-path: caller has already encoded
	// the proto once and reuses the same []byte for every recipient (skips
	// per-session re-encode). Set alongside serverPushWriter; nil means the
	// channel only accepts proto pushes.
	serverPushBytesWriter func([]byte) error
	// writeSessionID 是 sharedWriter 用来路由到 shard 的会话 ID；
	// 由 ProtoMessageHandler.OnOpen 设置，room.PushMsg 批量分组时需要。
	writeSessionID uint64

	// 统计：本 Channel 的业务 push 丢弃次数
	pushDropCount int64
	// 统计：本 Channel 的 ready signal 丢弃次数
	signalDropCount int64
	closed          uint32
}

func NewChannel(cli, svr int) *Channel {
	c := new(Channel)

	c.ClientReqQueue.Init(cli)

	c.signal = make(chan *protocol.Proto, svr)
	c.ready = make(chan struct{}, 1)
	c.done = make(chan struct{})

	c.watchOps = make(map[int32]struct{})
	return c
}

// SetKeyIP 设置 channel 的 Key 和 IP，只能调用一次。
// 这确保 Key 和 IP 在设置后变为只读，避免 race。
func (c *Channel) SetKeyIP(key, ip string) {
	c.setKeyIPOnce.Do(func() {
		c.Key = key
		c.IP = ip
	})
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
		dropCount := atomic.AddInt64(&c.pushDropCount, 1)
		globalDrop := atomic.AddInt64(&globalPushDropCount, 1)
		recordCriticalDrop("push", sharedWriterDropReason(err))
		if dropCount%100 == 1 {
			dropLog("⚠️  [Channel.Push] Shared writer enqueue failed! Key=%s, Room=%s, Err=%v, DropCount=%d, GlobalDrop=%d",
				c.Key,
				func() string {
					if r := c.Room.Load(); r != nil {
						return r.ID
					}
					return "nil"
				}(),
				err,
				dropCount,
				globalDrop)
		}
		return
	}

	//select {
	//case c.signal <- p:
	//	// 成功推送
	//default:
	//	// signal channel 满了，消息被丢弃
	//	err = pkg.ErrSignalFullMsgDropped
	//	recordCriticalDrop("push", "signal_full")
	//
	//	// 增加丢弃计数
	//	dropCount := atomic.AddInt64(&c.pushDropCount, 1)
	//	globalDrop := atomic.AddInt64(&globalPushDropCount, 1)
	//
	//	// 记录详细日志（每 100 次丢弃打印一次，避免日志爆炸）
	//	if dropCount%100 == 1 {
	//		dropLog("⚠️  [Channel.Push] Signal channel FULL! Key=%s, Room=%s, ChannelLen=%d/%d, DropCount=%d, GlobalDrop=%d",
	//			c.Key,
	//			func() string {
	//				if r := c.Room.Load(); r != nil {
	//					return r.ID
	//				}
	//				return "nil"
	//			}(),
	//			len(c.signal),
	//			cap(c.signal),
	//			dropCount,
	//			globalDrop)
	//	}
	//}

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
	c.serverPushBytesWriter = nil
	c.mutex.Unlock()
}

// SetServerPushBytesWriter 装载预编码字节快路径（房间广播用）。
func (c *Channel) SetServerPushBytesWriter(writer func([]byte) error) {
	c.mutex.Lock()
	c.serverPushBytesWriter = writer
	c.mutex.Unlock()
}

// PushBytes 投递已编码好的字节帧。若 channel 未装载 bytes-writer，
// 返回 errNoBytesPushWriter 以便调用方回退到 proto 路径。
func (c *Channel) PushBytes(data []byte) error {
	c.mutex.RLock()
	w := c.serverPushBytesWriter
	c.mutex.RUnlock()
	if w == nil {
		return errNoBytesPushWriter
	}
	if err := w(data); err != nil {
		dropCount := atomic.AddInt64(&c.pushDropCount, 1)
		atomic.AddInt64(&globalPushDropCount, 1)
		recordCriticalDrop("push_bytes", sharedWriterDropReason(err))
		if dropCount%100 == 1 {
			dropLog("⚠️  [Channel.PushBytes] enqueue failed! Key=%s, Err=%v, DropCount=%d",
				c.Key, err, dropCount)
		}
		return err
	}
	return nil
}

func (c *Channel) Ready() *protocol.Proto {
	select {
	case <-c.done:
		return proto.ProtoFinish
	default:
	}

	select {
	case <-c.done:
		return proto.ProtoFinish
	case p := <-c.signal:
		return p
	case <-c.ready:
		return proto.ProtoReady
	}
}

func (c *Channel) Signal() {
	if atomic.LoadUint32(&c.closed) != 0 {
		return
	}
	select {
	case c.ready <- struct{}{}:
	default:
		atomic.AddInt64(&c.signalDropCount, 1)
		atomic.AddInt64(&globalSignalDropCount, 1)
		recordCriticalDrop("signal", "coalesced")
	}
}

func (c *Channel) Close() {
	if atomic.CompareAndSwapUint32(&c.closed, 0, 1) {
		close(c.done)
	}
}

// GetDropCount 获取当前 Channel 的业务 push 丢弃次数。
func (c *Channel) GetDropCount() int64 {
	return atomic.LoadInt64(&c.pushDropCount)
}

// GetSignalDropCount 获取当前 Channel 的 ready signal 丢弃次数。
func (c *Channel) GetSignalDropCount() int64 {
	return atomic.LoadInt64(&c.signalDropCount)
}

// GetGlobalPushDropCount 获取全局业务 push 丢弃次数。
func GetGlobalPushDropCount() int64 {
	return atomic.LoadInt64(&globalPushDropCount)
}

// GetGlobalDropCount 获取全局 ready signal 丢弃次数。
func GetGlobalDropCount() int64 {
	return atomic.LoadInt64(&globalSignalDropCount)
}

// GetGlobalSignalDropCount 获取全局 ready signal 丢弃次数。
func GetGlobalSignalDropCount() int64 {
	return atomic.LoadInt64(&globalSignalDropCount)
}

func sharedWriterDropReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if err == pkg.ErrSignalFullMsgDropped {
		return "signal_full"
	}
	if errors.Is(err, errSharedWriterQueueFull) {
		return "queue_full"
	}
	return "enqueue_failed"
}
