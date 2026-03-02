package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	getty "github.com/AlexStocks/getty/transport"
	gxnet "github.com/AlexStocks/goext/net"
	gxsync "github.com/dubbogo/gost/sync"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxInt = 1<<31 - 1
	r      = 0
)

const (
	writeBatchSize      = 32
	writeBatchTimeout   = 500 * time.Millisecond
	writeBatchQueueSize = 1024
)

// ProtoResponse Proto 消息的响应结构体
// Body 字段使用此结构体的 JSON 序列化
type ProtoResponse struct {
	Code    int    `json:"code"`           // 响应码：0=成功，非0=失败
	Message string `json:"message"`        // 响应消息（简短描述）
	Text    string `json:"text,omitempty"` // 详细文本（可选，用于错误详情或额外信息）
}

// sendProtoResponse 发送 Proto 响应给客户端
func (h *ProtoMessageHandler) sendProtoResponse(session getty.Session, req *proto.Proto, resp *ProtoResponse) error {
	// 序列化响应为 JSON
	bodyBytes, err := json.Marshal(resp)
	if err != nil {
		wsLog("❌ [ProtoHandler] 序列化响应失败: %v", err)
		return fmt.Errorf("序列化响应失败: %w", err)
	}

	protoResp := &proto.Proto{
		Ver:    req.Ver,
		Op:     req.Op + 1, // 回复 op
		Seq:    req.Seq,
		Roomid: req.Roomid,
		Userid: req.Userid,
		Body:   bodyBytes,
	}

	if err = h.writeProto(session, protoResp, "response"); err != nil {
		wsLog("❌ [ProtoHandler] 发送响应失败: %v", err)
		return err
	}
	return nil
}

// newSuccessResponse 创建成功响应
func newSuccessResponse(message string, text string) *ProtoResponse {
	return &ProtoResponse{
		Code:    0,
		Message: message,
		Text:    text,
	}
}

// newErrorResponse 创建错误响应
func newErrorResponse(code int, message string, text string) *ProtoResponse {
	if code == 0 {
		code = -1 // 默认错误码
	}
	return &ProtoResponse{
		Code:    code,
		Message: message,
		Text:    text,
	}
}

var (
	serverList []getty.Server
	wsLogger   *log.Logger // WebSocket 专用 logger
	// CONNECT_NODE_WRITE_TRACE_LOG=1 enables verbose write-tracing logs.
	enableWriteTraceLog = os.Getenv("CONNECT_NODE_WRITE_TRACE_LOG") == "1"
)

// InitWebsocketLogger 初始化 WebSocket 日志输出到文件
// 如果 logFile 为空，则只输出到控制台
func InitWebsocketLogger(logFile string) error {
	var logWriter io.Writer = os.Stdout

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open websocket log file %s: %w", logFile, err)
		}
		// 同时输出到控制台和文件
		logWriter = io.MultiWriter(os.Stdout, f)
		log.Printf("[server_websocket] 日志已写入文件: %s", logFile)
	}

	wsLogger = log.New(logWriter, "", log.LstdFlags)
	return nil
}

// wsLog 安全地使用 wsLogger，如果未初始化则使用标准 log
func wsLog(format string, v ...interface{}) {
	if wsLogger != nil {
		wsLogger.Printf(format, v...)
	} else {
		log.Printf(format, v...)
	}
}

func InitWebsocket(server *ConnectNodeServer, addrs []string, accept int) (err error) {

	newSessionFunc := func(session getty.Session) error {
		var (
			flag1, flag2 bool
			tcpConn      *net.TCPConn
			r            int
		)

		_, flag1 = session.Conn().(*tls.Conn)
		tcpConn, flag2 = session.Conn().(*net.TCPConn)
		if !flag1 && !flag2 {
			panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp/tls connection\n", session.Stat(), session.Conn()))
		}

		if server.config.GettyConfig.GettySessionParam.CompressEncoding {
			session.SetCompressType(getty.CompressZip)
		}

		if flag2 {
			if err := tcpConn.SetNoDelay(server.config.GettyConfig.GettySessionParam.TcpNoDelay); err != nil {
				wsLog("⚠️  SetNoDelay 失败: %v", err)
			}
			if err := tcpConn.SetKeepAlive(server.config.GettyConfig.GettySessionParam.TcpKeepAlive); err != nil {
				wsLog("⚠️  SetKeepAlive 失败: %v", err)
			}
			if err := tcpConn.SetReadBuffer(server.config.GettyConfig.GettySessionParam.TcpRBufSize); err != nil {
				wsLog("⚠️  SetReadBuffer 失败: %v", err)
			}
			if err := tcpConn.SetWriteBuffer(server.config.GettyConfig.GettySessionParam.TcpWBufSize); err != nil {
				wsLog("⚠️  SetWriteBuffer 失败: %v", err)
			}
		}

		// 根据 session ID 选择 round（用于负载均衡）
		// 可以使用 session ID 的哈希值
		//r = 0 // 可以根据 session ID 计算哈希

		// 从 round 获取 pool
		//tr := server.round.Timer(r)
		rp := server.round.Reader(r)
		wp := server.round.Writer(r)

		// new session 的时候 ，确定好 对应 bucket， 创建 channel，初始化 上下文 ctx

		// 创建 ProtoPackageHandler
		protoPkgHandler := gettypkg.NewProtoPackageHandler(rp, wp)

		//server.sessionMap[&session] = &clientProtoSession{
		//	session: session,
		//}

		channel := NewChannel(server.config.Protocol.CliProto, server.config.Protocol.SvrProto)

		protoMsgHandler := newProtoMessageHandler(server, channel, protoPkgHandler)

		//protoMsgHandler := &ProtoMessageHandler{}

		//session.SetAttribute("protoPkgHandler",protoPkgHandler)
		//session.SetAttribute("channel",channel)
		//session.SetAttribute("server",server)
		// 将 handler 存储到 session 的 context 中，以便在关闭时归还 buffer
		// 注意：getty session 可能没有直接的 context，我们需要通过其他方式管理
		// 这里我们通过 message handler 来管理

		session.SetName(server.config.GettyConfig.GettySessionParam.SessionName)
		session.SetMaxMsgLen(server.config.GettyConfig.GettySessionParam.MaxMsgLen)
		session.SetPkgHandler(protoPkgHandler)
		session.SetEventListener(protoMsgHandler)
		session.SetReadTimeout(server.config.GettyConfig.GettySessionParam.TcpReadTimeout)
		session.SetWriteTimeout(server.config.GettyConfig.GettySessionParam.TcpWriteTimeout)
		session.SetCronPeriod((int)(server.config.GettyConfig.HeartbeatPeriod.Nanoseconds() / 1e6))
		session.SetWaitTime(server.config.GettyConfig.GettySessionParam.WaitTimeout)

		r = r + 1
		// 将 handler 存储到 message handler 中，以便在关闭时归还 buffer
		//protoMsgHandler.StoreHandler(session, protoPkgHandler, server)

		return nil
	}

	taskPool := gxsync.NewTaskPoolSimple(10)

	for _, port := range addrs {
		// addr = host + ":" + port
		// 使用 GettyConfig.Host 作为监听地址
		host := server.config.GettyConfig.Host
		if host == "" {
			host = "0.0.0.0"
		}
		addr := gxnet.HostAddress2(host, port)
		wsLog("🔌 启动 Getty WebSocket 服务器: %s (路径: /connect)\n", addr)
		wsserver := getty.NewWSServer(
			getty.WithLocalAddress(addr),
			getty.WithWebsocketServerPath("/connect"),
			getty.WithServerTaskPool(taskPool),
		)
		wsserver.RunEventLoop(newSessionFunc)

		serverList = append(serverList, wsserver)
	}

	return
}

var (
	errTooManySessions = errors.New("too many sessions")
)

////////////////////////////////////////////
// message handler
////////////////////////////////////////////

////////////////////////////////////////////
// ProtoMessageHandler
////////////////////////////////////////////

type clientProtoSession struct {
	session getty.Session
	channel *Channel

	reqNum     int32
	transScene string
}

type ProtoMessageHandler struct {
	rwlock              sync.RWMutex
	server              *ConnectNodeServer
	protoPackageHandler *gettypkg.ProtoPackageHandler

	roomId   string
	clientId string
	bucket   *Bucket
	auth     bool
	channel  *Channel

	// 记录最后一次写入时间（用于超时检查）
	// Getty 的 GetActive() 只在读取时更新，写入不会更新
	// 所以我们需要单独记录写入时间（UnixNano）
	lastWriteTimeNano int64

	batcher *writeBatcher

	// 写入竞争统计（用于定位高并发写锁争用）
	writeInFlight        int32
	writeTotal           uint64
	writeConcurrent      uint64
	writeLastLogNano     int64
	writeLastStatLogNano int64

	// Batch 写入验证统计（用于确认 WriteBytesArray 合并发送是否生效）
	batchEnqueued         uint64
	batchFlushes          uint64
	batchFlushedPkgs      uint64
	batchFlushedBytes     uint64
	batchMaxPkgsPerFlush  uint32
	batchLastFlushLogNano int64

	closeOnce sync.Once
}

// TODO 之前的 server_websocket 是客户端写入的很多消息，一次性合并等所有消息都处理完，拿到 server 的 resp 之后，
// TODO 在进行合并返回给客户端 处理后的结果
// TODO 而服务端主动推送的消息，是直接进行 flush 的

func newProtoMessageHandler(server *ConnectNodeServer, channel *Channel,
	protoPackageHandler *gettypkg.ProtoPackageHandler) *ProtoMessageHandler {

	return &ProtoMessageHandler{
		// session 相当于 channel
		channel:             channel,
		protoPackageHandler: protoPackageHandler,
		server:              server,
		auth:                false,
	}
}

// RemoveHandler 移除并归还 buffer（内部不加锁，由调用者保证线程安全）

func (h *ProtoMessageHandler) OnOpen(session getty.Session) error {

	// 初始化写入时间为当前时间（连接建立时）
	atomic.StoreInt64(&h.lastWriteTimeNano, time.Now().UnixNano())

	h.batcher = newWriteBatcher(session, h, h.protoPackageHandler, writeBatchSize, writeBatchTimeout)
	h.batcher.Start()

	// 启动 dispatchWebsocket 协程处理客户端消息
	go h.dispatchWebsocket(session)

	return nil
}

func (h *ProtoMessageHandler) dispatchWebsocket(session getty.Session) {
	var (
		err    error
		p      *proto.Proto
		pwb    *gettypkg.ProtoWithBuffer
		finish bool
	)

	for {
		// 1. 等待信号（阻塞直到有新消息或关闭）
		p = h.channel.Ready()

		switch p {
		case proto.ProtoFinish:
			finish = true
			goto close

		case proto.ProtoReady:
			for {
				// 2. Get() 获取 rp 位置的 ProtoWithBuffer 指针
				pwb, err = h.channel.ClientReqQueue.Get()
				if err != nil {
					// Ring Buffer 空了，跳出内层循环，继续等待信号
					break
				}

				// 获取 Proto
				p = pwb.Proto

				// 3. 处理消息（根据 op 路由到不同的 handler）
				if err = h.processClientRequest(session, p); err != nil {
					wsLog("❌ [ProtoHandler] 处理消息失败: op=%d seq=%d err=%v", p.Op, p.Seq, err)
				}

				// 4. GetAdv() 推进 rp 指针
				h.channel.ClientReqQueue.GetAdv()

				// 5. 释放 Buffer（消息处理完成）
				pwb.Release()
			}

		default:
			// 服务端推送的消息（通过 Broadcast/BroadcastRoom 推送）
			if err := h.writeProto(session, p, "broadcast"); err != nil {
				wsLog("❌ [ProtoHandler] 发送推送消息失败: %v", err)
			} else {
				h.markWriteTime()
			}
		}

	}

close:
	if finish {
		session.Close()
		//h.protoPackageHandler.Close()
	}
}

// processClientRequest 处理客户端请求
func (h *ProtoMessageHandler) processClientRequest(session getty.Session, p *proto.Proto) error {
	switch p.Op {
	case 1: // 加入房间
		// 这里可以调用具体的业务逻辑

		// 从 Body 中获取 UserName（客户端发送的 body 是 userName）
		userName := string(p.Body)
		if userName == "" {
			userName = p.Userid // 如果没有 body，使用 userId 作为 userName
		}

		// 初始化 Metadata（如果为 nil）
		metadata := make(map[string]string)

		// 验证关键字段
		if p.Roomid == "" {
			wsLog("❌ [ProtoHandler] Roomid 为空！")
			return fmt.Errorf("roomid is empty")
		}
		if p.Userid == "" {
			wsLog("❌ [ProtoHandler] Userid 为空！")
			return fmt.Errorf("userid is empty")
		}
		if h.server.nodeID == "" {
			wsLog("❌ [ProtoHandler] nodeID 为空！server.nodeID=%q", h.server.nodeID)
			return fmt.Errorf("nodeID is empty")
		}
		if userName == "" {
			wsLog("⚠️  [ProtoHandler] userName 为空，使用 userId 作为 userName")
			userName = p.Userid
		}

		joinRoomRequest := controller.JoinRoomRequest{
			RoomId:   p.Roomid,
			UserId:   p.Userid,
			UserName: userName,
			NodeId:   h.server.nodeID, // 使用当前 connect-node 的 ID
			Metadata: metadata,        // 显式初始化，避免 nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		startTime := time.Now()
		resp, err := h.server.controllerClient.JoinRoom(ctx, &joinRoomRequest,
			grpc.WaitForReady(true))
		elapsed := time.Since(startTime)

		if err != nil {
			wsLog("❌ [ProtoHandler] JoinRoom 失败 room=%s user=%s elapsed=%v err=%v", p.Roomid, p.Userid, elapsed, err)
			// 检查是否是 gRPC 状态错误
			errorCode := -1
			errorMessage := "Internal Server Error"
			errorText := "gRPC 调用失败"
			if st, ok := status.FromError(err); ok {
				errorMessage = st.Message()
				errorText = fmt.Sprintf("gRPC 调用失败: %s", st.Message())
			}

			// 发送错误响应给客户端
			errorResp := newErrorResponse(errorCode, errorMessage, errorText)
			if sendErr := h.sendProtoResponse(session, p, errorResp); sendErr != nil {
				return fmt.Errorf("发送错误响应失败: %w", sendErr)
			}

			return fmt.Errorf("gRPC 调用失败: %w", err)
		}

		if resp == nil {
			errorResp := newErrorResponse(-1, "Empty Response", "JoinRoom 响应为空")
			if sendErr := h.sendProtoResponse(session, p, errorResp); sendErr != nil {
				return fmt.Errorf("发送错误响应失败: %w", sendErr)
			}
			return fmt.Errorf("JoinRoom 响应为空")
		}

		if !resp.Success {
			errorMessage := "Join Room Failed"
			if resp.Message != "" {
				errorMessage = resp.Message
			}
			// 发送错误响应给客户端
			errorResp := newErrorResponse(-1, errorMessage, resp.Message)
			if sendErr := h.sendProtoResponse(session, p, errorResp); sendErr != nil {
				return fmt.Errorf("发送错误响应失败: %w", sendErr)
			}
			return fmt.Errorf("JoinRoom 失败: %s", resp.Message)
		}

		wsLog("✅ [ProtoHandler] JoinRoom 成功 room=%s user=%s elapsed=%v", p.Roomid, p.Userid, elapsed)

		bucket := h.server.Bucket(h.channel.Key)
		if bucket != nil {
			if err := bucket.ChangeRoom(p.Roomid, h.channel); err != nil {
				wsLog("❌ [ProtoHandler] 设置 channel.Room 失败: %v", err)
			}
		}

		h.channel.Watch(2)

		successMessage := "Join Room Success"
		if resp.Message != "" {
			successMessage = resp.Message
		}
		successResp := newSuccessResponse(successMessage, "成功加入房间")
		if err := h.sendProtoResponse(session, p, successResp); err != nil {
			return err
		}
		h.markWriteTime()

	case 5: // 心跳包
		return nil

	default:
		wsLog("⚠️  [ProtoHandler] 未知 op: %d", p.Op)
		return nil
	}

	return nil
}

func (h *ProtoMessageHandler) OnError(session getty.Session, err error) {
	wsLog("❌ [ProtoHandler] Session 错误: %s err=%v", session.RemoteAddr(), err)
	h.closeSessionResources()
}

func (h *ProtoMessageHandler) OnClose(session getty.Session) {
	wsLog("👋 [ProtoHandler] Session 关闭: %s", session.RemoteAddr())
	h.closeSessionResources()
}

func (h *ProtoMessageHandler) closeSessionResources() {
	h.closeOnce.Do(func() {
		h.cleanupUser()
		if h.channel != nil {
			h.channel.Close()
		}
		if h.batcher != nil {
			h.batcher.Stop()
		}
		if h.protoPackageHandler != nil {
			h.protoPackageHandler.Close()
		}
	})
}

// cleanupUser 清理用户（调用 Controller.LeaveRoom 并从 Bucket/Room 中移除）
// 只有在用户已认证（已加入房间）时才调用
func (h *ProtoMessageHandler) cleanupUser() {
	h.rwlock.RLock()
	auth := h.auth
	roomId := h.roomId
	userId := h.clientId
	bucket := h.bucket
	channel := h.channel
	h.rwlock.RUnlock()

	if !auth || roomId == "" || userId == "" {
		return
	}

	if bucket != nil && channel != nil {
		bucket.Del(channel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	leaveRoomRequest := &controller.LeaveRoomRequest{
		UserId: userId,
		RoomId: roomId,
	}

	startTime := time.Now()
	resp, err := h.server.controllerClient.LeaveRoom(ctx, leaveRoomRequest,
		grpc.WaitForReady(true))
	elapsed := time.Since(startTime)

	if err != nil {
		wsLog("❌ [ProtoHandler] LeaveRoom 失败 user=%s room=%s elapsed=%v err=%v", userId, roomId, elapsed, err)
		return
	}
	if resp != nil {
		wsLog("✅ [ProtoHandler] LeaveRoom 成功 user=%s room=%s elapsed=%v", userId, roomId, elapsed)
	} else {
		wsLog("⚠️  [ProtoHandler] LeaveRoom 返回 nil user=%s room=%s", userId, roomId)
	}
}

func (h *ProtoMessageHandler) authWebsocket(p *proto.Proto, session getty.Session) error {
	// 使用 op=1（加入房间）作为鉴权操作
	// 或者 op=0（如果有专门的认证操作码）
	if p.Roomid != "" && p.Userid != "" && (p.Op == proto.OpAuth || p.Op == 1) {
		// redis check login session (这里可以添加实际的认证逻辑)

		h.roomId = p.Roomid
		h.clientId = p.Userid

		h.bucket = h.server.Bucket(p.Userid)

		// ⚠️ 重要：设置 channel.Key，使用 userId 作为唯一标识
		// 如果不设置 Key，所有 channel 的 key 都是空字符串，会导致 bucket.Put 时关闭错误的 channel
		h.channel.Key = p.Userid
		if remoteAddr := session.RemoteAddr(); remoteAddr != "" {
			h.channel.IP = remoteAddr
		}

		h.bucket.Put(p.Roomid, h.channel)
		h.auth = true
		wsLog("✅ [ProtoHandler] 鉴权成功 room=%s user=%s", p.Roomid, p.Userid)
		return nil
	}

	return fmt.Errorf("auth failed: op=%d, roomId=%s, userId=%s", p.Op, p.Roomid, p.Userid)
}

func (h *ProtoMessageHandler) OnMessage(session getty.Session, pkg any) {
	// pkg 现在是 *ProtoWithBuffer 类型
	pwb, ok := pkg.(*gettypkg.ProtoWithBuffer)
	if !ok {
		wsLog("❌ [ProtoHandler] 非法包类型: %#v", pkg)
		return
	}

	p := pwb.Proto

	// 鉴权检查
	if !h.auth {
		err := h.authWebsocket(p, session)
		if err != nil {
			wsLog("❌ [ProtoHandler] 鉴权失败: %v", err)
			// 鉴权失败，立即释放 Buffer
			pwb.Release()
			return
		}
	}

	if p.Op == 5 {
		resp := &proto.Proto{
			Ver:    p.Ver,
			Op:     6,
			Seq:    p.Seq,
			Roomid: p.Roomid,
			Userid: p.Userid,
			Body:   nil,
		}
		if err := h.writeProto(session, resp, "heartbeat"); err != nil {
			wsLog("⚠️  [ProtoHandler] 发送心跳响应失败: %v", err)
		} else {
			h.markWriteTime()
		}
		pwb.Release()
		return
	}

	// 将消息放入 CliProto Ring Buffer
	// 1. Set() 获取 wp 位置的 ProtoWithBuffer 指针位置
	clipwb, err := h.channel.ClientReqQueue.Set()
	if err != nil {
		// Ring Buffer 满了，丢弃消息或等待
		wsLog("⚠️  [ProtoHandler] ClientReqQueue 已满，丢弃消息: op=%d, seq=%d", p.Op, p.Seq)
		// Ring Buffer 满了，立即释放 Buffer
		pwb.Release()
		return
	}

	// 2. 直接存储 ProtoWithBuffer 指针（零拷贝）
	*clipwb = pwb

	// 3. SetAdv() 推进 wp 指针
	h.channel.ClientReqQueue.SetAdv()

	// 4. Signal() 通知 dispatchWebsocket 有新数据
	h.channel.Signal()

	// ⚠️ 注意：此时不能释放 Buffer！
	// Buffer 会传递给 dispatchWebsocket 协程，处理完后由它负责释放
}

func writeResp(session getty.Session, resp *proto.Proto) {
	if _, _, err := session.WritePkg(resp, 5*time.Second); err != nil {
		wsLog("send failed: %v", err)
	}
}

func (h *ProtoMessageHandler) writeProto(session getty.Session, p *proto.Proto, source string) error {
	if h.batcher == nil {
		h.writeTraceStart(source)
		defer h.writeTraceEnd()
		_, _, err := session.WritePkg(p, 0)
		return err
	}
	atomic.AddUint64(&h.batchEnqueued, 1)
	return h.batcher.Enqueue(p)
}

func (h *ProtoMessageHandler) markWriteTime() {
	atomic.StoreInt64(&h.lastWriteTimeNano, time.Now().UnixNano())
}

func (h *ProtoMessageHandler) writeTraceStart(source string) {
	inFlight := atomic.AddInt32(&h.writeInFlight, 1)
	atomic.AddUint64(&h.writeTotal, 1)

	if inFlight <= 1 {
		return
	}
	atomic.AddUint64(&h.writeConcurrent, 1)

	// Throttle contention logs to <= 1/s per session.
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&h.writeLastLogNano)
	if now-last < int64(time.Second) {
		return
	}
	if !atomic.CompareAndSwapInt64(&h.writeLastLogNano, last, now) {
		return
	}

	total := atomic.LoadUint64(&h.writeTotal)
	concurrent := atomic.LoadUint64(&h.writeConcurrent)
	remote := ""
	if h.channel != nil {
		remote = h.channel.IP
	}
	if enableWriteTraceLog {
		wsLog("⚠️  [ProtoHandler] write contention: inFlight=%d source=%s totalWrites=%d concurrentWrites=%d remote=%s",
			inFlight, source, total, concurrent, remote)
	}
}

func (h *ProtoMessageHandler) writeTraceEnd() {
	atomic.AddInt32(&h.writeInFlight, -1)
}

type writeBatcher struct {
	session       getty.Session
	handler       *gettypkg.ProtoPackageHandler
	owner         *ProtoMessageHandler
	batchSize     int
	flushInterval time.Duration
	in            chan *proto.Proto
	stop          chan struct{}
	stopped       int32
}

func newWriteBatcher(session getty.Session, owner *ProtoMessageHandler, handler *gettypkg.ProtoPackageHandler, batchSize int, flushInterval time.Duration) *writeBatcher {
	if batchSize <= 0 {
		batchSize = 1
	}
	return &writeBatcher{
		session:       session,
		owner:         owner,
		handler:       handler,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		in:            make(chan *proto.Proto, writeBatchQueueSize),
		stop:          make(chan struct{}),
	}
}

func (b *writeBatcher) Start() {
	go b.loop()
}

func (b *writeBatcher) Stop() {
	if atomic.CompareAndSwapInt32(&b.stopped, 0, 1) {
		close(b.stop)
	}
}

func (b *writeBatcher) Enqueue(p *proto.Proto) error {
	select {
	case b.in <- p:
		return nil
	case <-b.stop:
		return errors.New("write batcher stopped")
	}
}

func (b *writeBatcher) loop() {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	var batch [][]byte

	flush := func() {
		if len(batch) == 0 {
			return
		}
		pkgs := len(batch)
		bytes := 0
		for i := 0; i < pkgs; i++ {
			bytes += len(batch[i])
		}
		if b.owner != nil {
			b.owner.writeTraceStart("batch")
		}
		if _, err := b.session.WriteBytesArray(batch...); err != nil {
			wsLog("❌ [ProtoHandler] WriteBytesArray failed: %v", err)
		} else if b.owner != nil {
			b.owner.markWriteTime()
			atomic.AddUint64(&b.owner.batchFlushes, 1)
			atomic.AddUint64(&b.owner.batchFlushedPkgs, uint64(pkgs))
			atomic.AddUint64(&b.owner.batchFlushedBytes, uint64(bytes))

			// Track max pkgs per flush.
			for {
				old := atomic.LoadUint32(&b.owner.batchMaxPkgsPerFlush)
				if uint32(pkgs) <= old {
					break
				}
				if atomic.CompareAndSwapUint32(&b.owner.batchMaxPkgsPerFlush, old, uint32(pkgs)) {
					break
				}
			}

			// Throttle: log at most once per second per session, and only if we actually flushed >1 pkg.
			if pkgs > 1 {
				now := time.Now().UnixNano()
				last := atomic.LoadInt64(&b.owner.batchLastFlushLogNano)
				if now-last >= int64(time.Second) && atomic.CompareAndSwapInt64(&b.owner.batchLastFlushLogNano, last, now) {
					remote := ""
					if b.owner.channel != nil {
						remote = b.owner.channel.IP
					}
					if enableWriteTraceLog {
						wsLog("✅ [ProtoHandler] batch flush: pkgs=%d bytes=%d remote=%s", pkgs, bytes, remote)
					}
				}
			}
		}
		if b.owner != nil {
			b.owner.writeTraceEnd()
		}
		batch = batch[:0]
	}

	for {
		select {
		case p := <-b.in:
			if p == nil {
				continue
			}
			data, err := b.handler.Write(b.session, p)
			if err != nil {
				wsLog("❌ [ProtoHandler] batch encode failed: %v", err)
				continue
			}
			batch = append(batch, data)
			if len(batch) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-b.stop:
			flush()
			return
		}
	}
}

func (h *ProtoMessageHandler) OnCron(session getty.Session) {
	// Low-frequency write stats to help correlate CPU profiles with actual write contention.
	// This is intentionally throttled to avoid adding more lock contention via logging.
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&h.writeLastStatLogNano)
	if now-last >= int64(10*time.Second) && atomic.CompareAndSwapInt64(&h.writeLastStatLogNano, last, now) {
		concurrent := atomic.LoadUint64(&h.writeConcurrent)
		if concurrent != 0 && enableWriteTraceLog {
			// Avoid noisy logs per session; only report when we have observed concurrent writes.
			remote := ""
			if h.channel != nil {
				remote = h.channel.IP
			}
			wsLog("[stat] ws writes: total=%d concurrent=%d inFlight=%d remote=%s",
				atomic.LoadUint64(&h.writeTotal),
				concurrent,
				atomic.LoadInt32(&h.writeInFlight),
				remote,
			)
		}

		flushes := atomic.LoadUint64(&h.batchFlushes)
		if flushes != 0 && enableWriteTraceLog {
			pkgs := atomic.LoadUint64(&h.batchFlushedPkgs)
			bytes := atomic.LoadUint64(&h.batchFlushedBytes)
			enq := atomic.LoadUint64(&h.batchEnqueued)
			maxPkgs := atomic.LoadUint32(&h.batchMaxPkgsPerFlush)
			avgPkgs := float64(pkgs) / float64(flushes)
			remote := ""
			if h.channel != nil {
				remote = h.channel.IP
			}
			// Only print when batching is meaningfully doing work (avg > 1).
			if avgPkgs > 1.05 {
				wsLog("[stat] ws batch: enqueued=%d flushes=%d pkgs=%d avgPkgs=%.2f bytes=%d maxPkgs=%d remote=%s",
					enq, flushes, pkgs, avgPkgs, bytes, maxPkgs, remote)
			}
		}
	}

	// 使用配置的 session_timeout（默认 180 秒，给心跳留出更多缓冲）
	timeout := h.server.config.GettyConfig.SessionTimeout
	if timeout == 0 {
		timeout = 180 * time.Second // 默认值改为 180 秒（6倍心跳周期）
	}

	// 如果超时时间设置为 0 或负数，则禁用超时检查
	if timeout <= 0 {
		return
	}

	// Getty 的 GetActive() 只在读取时更新，写入不会更新
	// 所以我们需要同时检查读取活跃时间和写入活跃时间
	readActiveTime := session.GetActive()
	timeSinceRead := time.Since(readActiveTime)

	// 检查写入活跃时间
	lastWriteNano := atomic.LoadInt64(&h.lastWriteTimeNano)
	var lastWriteTime time.Time
	if lastWriteNano > 0 {
		lastWriteTime = time.Unix(0, lastWriteNano)
	}
	timeSinceWrite := time.Since(lastWriteTime)

	// 取两者中较新的时间作为活跃时间
	var timeSinceActive time.Duration
	var _ time.Time
	if !lastWriteTime.IsZero() && lastWriteTime.After(readActiveTime) {
		// 如果写入时间更新，使用写入时间
		timeSinceActive = timeSinceWrite
		_ = lastWriteTime
	} else {
		// 否则使用读取时间
		timeSinceActive = timeSinceRead
		_ = readActiveTime
	}

	// 记录每次检查的详细信息（用于调试）
	if timeSinceActive > timeout {
		wsLog("⏰ [ProtoHandler] Session 超时，关闭连接: %s (超时阈值: %v, 距离上次活跃: %v, 读取活跃: %v, 写入活跃: %v)",
			session.RemoteAddr(), timeout, timeSinceActive, readActiveTime, lastWriteTime)

		// 清理用户（如果已认证）
		//h.cleanupUser()

		session.Close()
	}
}
