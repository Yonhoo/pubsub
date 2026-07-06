// bench_client.go
package main

// 用法示例：
// go run client.go -room=room-001 -users=1000 -ws=ws://localhost:8083/connect
//
// -room     房间 ID
// -users    模拟用户数量
// -ws       Connect-Node WebSocket 地址（完整 URL，包含 /connect 路径）
// -hb       心跳间隔（毫秒，默认 30000ms）
// -log      日志文件路径（默认 client.log，同时输出到控制台）
// -stat     统计日志文件路径（默认 stat.log，同时输出到控制台）

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	getty "github.com/AlexStocks/getty/transport"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

// clientLogger 客户端专用的日志记录器
var clientLogger *log.Logger

// initClientLogger 初始化客户端日志记录器
// 使用与标准 log 相同的 writer，确保所有日志写入同一个文件
func initClientLogger(logFile string) error {
	// 使用标准 log 的 writer（已经通过 log.SetOutput 设置好了）
	// 这样 clientLogger 和标准 log 会写入同一个地方
	clientLogger = log.New(log.Writer(), "", log.LstdFlags)
	return nil
}

// clientLog 客户端日志输出函数
func clientLog(format string, v ...interface{}) {
	if clientLogger != nil {
		clientLogger.Printf(format, v...)
	} else {
		log.Printf(format, v...)
	}
}

// BenchmarkClient Getty WebSocket 客户端（用于压测）
type BenchmarkClient struct {
	session    getty.Session
	userID     string
	userName   string
	roomID     string
	countDown  *int64 // 累计收到的消息总数
	aliveCount *int64 // 当前在线的客户端数量
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.RWMutex
	seq        int32
	seqMu      sync.Mutex
	joinOnce   sync.Once
	joinResult chan error
}

func main() {
	roomID := flag.String("room", "room-001", "room id")
	users := flag.Int("users", 1000, "number of users")
	wsBase := flag.String("ws", "ws://localhost:8083/connect", "connect-node websocket base url")
	hbMs := flag.Int("hb", 30000, "heartbeat interval (milliseconds)")
	logFile := flag.String("log", "client.log", "log file path (default client.log)")
	statFile := flag.String("stat", "stat.log", "statistics log file path (default stat.log)")
	userPrefix := flag.String("prefix", "user", "user id prefix")
	maxDelay := flag.Int("maxdelay", 0, "max random delay seconds for join (0=no delay)")
	flag.Parse()

	// 设置日志输出（同时影响标准 log 和 Getty 库的日志）
	f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("[client] failed to open log file %s: %v", *logFile, err)
	}
	defer f.Close()

	// 同时输出到控制台和文件
	logWriter := f
	// 设置标准 log 的输出（这样 Getty 库的 ProtoHandler 日志也会写入文件）
	log.SetOutput(logWriter)
	log.Printf("[client] 日志已写入文件: %s (包含 ProtoHandler 编解码日志)", *logFile)

	// 初始化客户端专用日志记录器（使用同一个 writer）
	if err := initClientLogger(*logFile); err != nil {
		log.Fatalf("[client] 初始化日志失败: %v", err)
	}

	clientLog("[client] room=%s users=%d ws=%s hb=%dms",
		*roomID, *users, *wsBase, *hbMs)

	// 设置统计日志输出
	sf, err := os.OpenFile(*statFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("[client] failed to open stat file %s: %v", *statFile, err)
	}
	defer sf.Close()
	statWriter := io.MultiWriter(os.Stdout, sf) // 同时输出到控制台和文件
	clientLog("[client] 统计日志已写入文件: %s", *statFile)

	var countDown int64  // 累计收到的消息总数
	var aliveCount int64 // 当前在线的客户端数量

	// 统计协程：每 5 秒打印一次统计信息
	go func() {
		statLogger := log.New(statWriter, "", log.LstdFlags)
		var (
			lastTimes int64
			interval  = int64(5) // 每 5 秒统计一次
		)
		for {
			time.Sleep(time.Second * time.Duration(interval))
			nowCount := atomic.LoadInt64(&countDown)
			nowAlive := atomic.LoadInt64(&aliveCount)
			diff := nowCount - lastTimes
			lastTimes = nowCount
			statLogger.Printf("[stat] alive:%d down:%d down/s:%d",
				nowAlive, nowCount, diff/interval)
		}
	}()

	// 启动多个用户连接
	for i := 0; i < *users; i++ {
		uid := fmt.Sprintf("%s-%06d", *userPrefix, i+1)
		go func(userID string) {
			if *maxDelay > 0 {
				time.Sleep(time.Duration(rand.Intn(*maxDelay*1000)) * time.Millisecond)
			}
			startClient(*wsBase, *roomID, userID, userID, &countDown, &aliveCount, *hbMs)
		}(uid)
		// 避免同时创建太多连接，稍微错开
		if i%100 == 0 && i > 0 {
			time.Sleep(1000 * time.Millisecond)
		}
	}

	// 等待 Ctrl+C 退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	clientLog("[client] 退出")
}

func startClient(wsBase, roomID, userID, userName string, countDown, aliveCount *int64, hbMs int) {
	// 构建完整的 WebSocket URL（包含 query 参数）
	wsURL, err := url.Parse(wsBase)
	if err != nil {
		clientLog("[client %s] parse url err: %v", userID, err)
		return
	}

	// Getty 要求地址格式为 ws://host:port/path
	if wsURL.Scheme == "" {
		wsURL.Scheme = "ws"
	}
	if wsURL.Host == "" {
		clientLog("[client %s] invalid url: %s", userID, wsBase)
		return
	}

	// 设置 query 参数
	q := wsURL.Query()
	q.Set("user_id", userID)
	q.Set("user_name", userName)
	q.Set("room_id", roomID)
	wsURL.RawQuery = q.Encode()

	retryDelay := time.Second
	for {
		client := &BenchmarkClient{
			userID:     userID,
			userName:   userName,
			roomID:     roomID,
			countDown:  countDown,
			aliveCount: aliveCount,
			done:       make(chan struct{}),
			seq:        1,
			joinResult: make(chan error, 1),
		}

		wsClient := getty.NewWSClient(
			getty.WithServerAddress(wsURL.String()),
			getty.WithConnectionNumber(1),
		)

		wsClient.RunEventLoop(func(session getty.Session) error {
			session.SetName(fmt.Sprintf("bench-client-%s", userID))
			session.SetMaxMsgLen(1024 * 1024)
			session.SetPkgHandler(gettypkg.NewProtoPackageHandler())
			session.SetEventListener(client)
			session.SetReadTimeout(60 * time.Second)
			session.SetWriteTimeout(60 * time.Second)
			session.SetCronPeriod(hbMs)
			session.SetWaitTime(60 * time.Second)

			client.mu.Lock()
			client.session = session
			client.mu.Unlock()

			return nil
		})

		var session getty.Session
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			client.mu.RLock()
			session = client.session
			client.mu.RUnlock()
			if session != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if session == nil {
			clientLog("[client %s] connection failed: session not created", userID)
			sleepJoinRetry(&retryDelay)
			continue
		}

		if err := client.JoinRoom(); err != nil {
			clientLog("[client %s] join room err: %v", userID, err)
			session.Close()
			sleepJoinRetry(&retryDelay)
			continue
		}

		select {
		case joinErr := <-client.joinResult:
			if joinErr != nil {
				clientLog("[client %s] join room ack err: %v", userID, joinErr)
				session.Close()
				sleepJoinRetry(&retryDelay)
				continue
			}
		case <-time.After(15 * time.Second):
			clientLog("[client %s] join room ack timeout", userID)
			session.Close()
			sleepJoinRetry(&retryDelay)
			continue
		}

		retryDelay = time.Second
		atomic.AddInt64(aliveCount, 1)
		time.Sleep(500 * time.Millisecond)
		<-client.done
		atomic.AddInt64(aliveCount, -1)
		clientLog("[client %s] connection closed, reconnecting...", userID)
		time.Sleep(time.Second)
	}
}

func sleepJoinRetry(delay *time.Duration) {
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
	time.Sleep(*delay + jitter)
	if *delay < 30*time.Second {
		*delay *= 2
		if *delay > 30*time.Second {
			*delay = 30 * time.Second
		}
	}
}

// OnOpen Getty 会话打开回调
func (c *BenchmarkClient) OnOpen(session getty.Session) error {
	return nil
}

// OnError Getty 错误回调
func (c *BenchmarkClient) OnError(session getty.Session, err error) {
	clientLog("[client %s] session error: %v", c.userID, err)
}

// OnClose Getty 会话关闭回调
func (c *BenchmarkClient) OnClose(session getty.Session) {
	clientLog("[client %s] session closed", c.userID)
	c.signalJoinResult(fmt.Errorf("session closed before join ack"))
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

// OnMessage Getty 消息回调
func (c *BenchmarkClient) OnMessage(session getty.Session, pkg interface{}) {
	// 处理 ProtoWithBuffer 类型（包含 buffer pool 优化）
	protoWithBuf, ok := pkg.(*gettypkg.ProtoWithBuffer)
	if !ok {
		clientLog("[client %s] ⚠️ received non-proto message: %T", c.userID, pkg)
		return
	}

	// 使用完后释放 buffer
	defer protoWithBuf.Release()

	// 获取实际的 Proto 消息
	protoMsg := protoWithBuf.Proto
	if protoMsg == nil {
		clientLog("[client %s] ⚠️ received nil proto message", c.userID)
		return
	}

	if protoMsg.Op == 2 && c.handleJoinResponse(protoMsg) {
		return
	}

	// 排除加入房间响应（op=2, body="join room success"）
	// 只统计真正的广播消息（op=2 且 body 不是 "join room success"）
	if protoMsg.Op == 2 && len(protoMsg.Body) > 0 {
		// 检查是否是加入房间响应
		bodyStr := string(protoMsg.Body)
		if bodyStr != "join room success" {
			atomic.AddInt64(c.countDown, 1)
		}
	} else if protoMsg.Op == 6 {
		// 心跳响应，不统计
	}
}

func (c *BenchmarkClient) signalJoinResult(err error) {
	c.joinOnce.Do(func() {
		c.joinResult <- err
	})
}

func (c *BenchmarkClient) handleJoinResponse(protoMsg *protocol.Proto) bool {
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(protoMsg.Body, &resp); err != nil {
		return false
	}
	if resp.Message == "" && resp.Text == "" && resp.Code == 0 {
		return false
	}
	if resp.Code != 0 {
		msg := resp.Message
		if resp.Text != "" {
			msg = msg + " " + resp.Text
		}
		c.signalJoinResult(fmt.Errorf("%s", msg))
		return true
	}
	c.signalJoinResult(nil)
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// OnCron Getty 定时回调（心跳）
func (c *BenchmarkClient) OnCron(session getty.Session) {
	if err := c.SendHeartbeat(); err != nil {
		clientLog("[client %s] heartbeat err: %v", c.userID, err)
	}
}

// JoinRoom 加入房间
func (c *BenchmarkClient) JoinRoom() error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("session not connected")
	}

	// 获取并递增 seq
	c.seqMu.Lock()
	seq := c.seq
	c.seq++
	c.seqMu.Unlock()

	// 构造加入房间的 Proto 消息
	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     1, // 1 = 加入房间
		Seq:    seq,
		Roomid: c.roomID,
		Userid: c.userID,
		Body:   []byte(c.userName),
	}

	// 打印发送的请求详情
	clientLog("📤 [Client %s] 发送 JoinRoom 请求: ver=%d, op=%d, seq=%d, roomId=%s, userId=%s, userName=%s, bodyLen=%d",
		c.userID, protoMsg.Ver, protoMsg.Op, protoMsg.Seq, protoMsg.Roomid, protoMsg.Userid, c.userName, len(protoMsg.Body))

	// 发送消息
	_, _, err := session.WritePkg(protoMsg, 5*time.Second)
	if err != nil {
		clientLog("❌ [Client %s] 发送 JoinRoom 请求失败: %v", c.userID, err)
	}
	return err
}

// SendHeartbeat 发送心跳
func (c *BenchmarkClient) SendHeartbeat() error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("session not connected")
	}

	// 获取并递增 seq
	c.seqMu.Lock()
	seq := c.seq
	c.seq++
	c.seqMu.Unlock()

	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     5, // 5 = 心跳
		Seq:    seq,
		Roomid: c.roomID,
		Userid: c.userID,
	}

	_, _, err := session.WritePkg(protoMsg, 5*time.Second)
	return err
}
