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
	queueFullLogWindow  = time.Second
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
		if h.isSharedWriterQueueFull(err) {
			return err
		}
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
	// Monotonic ID used to bind one websocket session to one shared writer shard.
	sharedWriteSessionSeq uint64
)

// InitWebsocketLogger 初始化 WebSocket 日志输出到文件
// 如果 logFile 为空，则只输出到控制台
func InitWebsocketLogger(logFile string) error {
	var logWriter io.Writer = os.Stdout

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open websocket log file %s: %w", logFile, err)
		}
		// 同时输出到控制台和文件
		logWriter = io.MultiWriter(os.Stdout, f)
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

		// 创建 ProtoPackageHandler（buffer 通过 server-level sync.Pool 管理）
		protoPkgHandler := gettypkg.NewProtoPackageHandler()

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

	writeSessionID uint64

	// 写入竞争统计（用于定位高并发写锁争用）
	writeInFlight        int32
	writeTotal           uint64
	writeConcurrent      uint64
	writeLastLogNano     int64
	writeLastStatLogNano int64

	// Batch 写入验证统计（用于确认 WriteBytesArray 合并发送是否生效）
	batchEnqueued         uint64
	batchEnqueueFailures  uint64
	batchEnqueueQueueFull uint64
	batchFlushes          uint64
	batchFlushByCount     uint64
	batchFlushByBytes     uint64
	batchFlushByTimeout   uint64
	batchFlushedPkgs      uint64
	batchFlushedBytes     uint64
	batchMaxPkgsPerFlush  uint32
	batchLastFlushLogNano int64
	batchLastFailLogNano  int64

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

	h.writeSessionID = atomic.AddUint64(&sharedWriteSessionSeq, 1)
	if h.server != nil && h.server.sharedWriter != nil {
		if err := h.server.sharedWriter.Register(h.writeSessionID, session, h.protoPackageHandler, h); err != nil {
			wsLog("❌ [ProtoHandler] shared writer register failed: %v", err)
			return err
		}
		h.channel.SetServerPushWriter(func(p *proto.Proto) error {
			// Keep server-side push non-blocking; drop when shard queue is full.
			return h.enqueueSharedWrite(p, "broadcast")
		})
		// 装载预编码字节快路径，房间广播一次编码、多接收者复用。
		h.channel.SetServerPushBytesWriter(func(data []byte) error {
			return h.enqueueSharedWriteBytes(data, "broadcast")
		})
		// 记录 writeSessionID 到 channel，room.PushMsg 批量分组时需要。
		h.channel.writeSessionID = h.writeSessionID
	}

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
		// Join must stay synchronous: controller result, local room/watch updates, and
		// the response enqueue all belong to the same confirmation chain.
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

		bucket := h.server.Bucket(p.Userid)
		if bucket == nil {
			return fmt.Errorf("bucket unavailable")
		}

		h.rwlock.RLock()
		wasAuth := h.auth
		h.rwlock.RUnlock()

		if wasAuth {
			if err := bucket.ChangeRoom(p.Roomid, h.channel); err != nil {
				wsLog("❌ [ProtoHandler] 切换 channel.Room 失败: %v", err)
				return err
			}
		} else if err := bucket.Put(p.Roomid, h.channel); err != nil {
			wsLog("❌ [ProtoHandler] 设置 channel.Room 失败: %v", err)
			return err
		}
		h.rwlock.Lock()
		h.bucket = bucket
		h.roomId = p.Roomid
		h.clientId = p.Userid
		h.auth = true
		h.rwlock.Unlock()

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
		if h.server != nil && h.server.sharedWriter != nil && h.writeSessionID != 0 {
			if err := h.server.sharedWriter.Unregister(h.writeSessionID); err != nil {
				wsLog("⚠️  [ProtoHandler] shared writer unregister failed: %v", err)
			}
		}
		if h.channel != nil {
			h.channel.ClearServerPushWriter()
			h.channel.Close()
		}
		if h.protoPackageHandler != nil {
			h.protoPackageHandler.Close()
		}
	})
}

// cleanupUser 先做本地解绑，再把 LeaveRoom 交给异步队列。
// Join 仍保持同步确认；只有 Leave 改为异步化。
func (h *ProtoMessageHandler) cleanupUser() {
	start := time.Now()
	result := "success"
	defer func() {
		// This measures local session close cleanup latency (detach + leave enqueue),
		// not end-to-end LeaveRoom completion across the async worker path.
		recordCriticalCloseLatency("cleanup_user", result, time.Since(start))
	}()
	h.rwlock.RLock()
	auth := h.auth
	roomId := h.roomId
	userId := h.clientId
	bucket := h.bucket
	channel := h.channel
	h.rwlock.RUnlock()

	if !auth || roomId == "" || userId == "" {
		result = "skipped"
		return
	}

	if bucket != nil && channel != nil {
		bucket.Del(channel)
		channel.Room.Store(nil)
	}
	if h.server != nil {
		if err := h.server.EnqueueLeaveWithShutdownFallback(userId, roomId); err != nil {
			result = "leave_enqueue_failed"
			wsLog("⚠️  [ProtoHandler] LeaveRoom 入队失败 user=%s room=%s err=%v", userId, roomId, err)
		}
	}
}

func (h *ProtoMessageHandler) authWebsocket(p *proto.Proto, session getty.Session) error {
	// 使用 op=1（加入房间）作为鉴权操作
	// 或者 op=0（如果有专门的认证操作码）
	if p.Roomid != "" && p.Userid != "" && (p.Op == proto.OpAuth || p.Op == 1) {
		// redis check login session (这里可以添加实际的认证逻辑)

		bucket := h.server.Bucket(p.Userid)
		if bucket == nil {
			return fmt.Errorf("bucket unavailable")
		}

		h.rwlock.Lock()
		h.roomId = p.Roomid
		h.clientId = p.Userid
		h.bucket = bucket

		// ⚠️ 重要：设置 channel.Key 和 IP，使用 roomId:userId 作为唯一标识。
		// SetKeyIP 通过 sync.Once 确保只设置一次，避免 race condition。
		// Key 使用复合键确保同一 userId 可以加入不同房间而不冲突。
		remoteAddr := session.RemoteAddr()
		compositeKey := p.Roomid + ":" + p.Userid
		h.channel.SetKeyIP(compositeKey, remoteAddr)

		h.rwlock.Unlock()

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
			if !h.isSharedWriterQueueFull(err) {
				wsLog("⚠️  [ProtoHandler] 发送心跳响应失败: %v", err)
			}
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

func (h *ProtoMessageHandler) writeProto(session getty.Session, p *proto.Proto, source string) error {
	return h.enqueueSharedWrite(p, source)
}

func (h *ProtoMessageHandler) enqueueSharedWrite(p *proto.Proto, source string) error {
	if h.server == nil || h.server.sharedWriter == nil || h.writeSessionID == 0 {
		err := fmt.Errorf("shared writer unavailable")
		h.recordSharedWriterEnqueue(source, err)
		return err
	}
	if err := h.server.sharedWriter.TryEnqueue(h.writeSessionID, p); err != nil {
		h.recordSharedWriterEnqueue(source, err)
		return err
	}
	atomic.AddUint64(&h.batchEnqueued, 1)
	if h.server != nil && h.server.metrics != nil {
		h.server.metrics.RecordSharedWriterEnqueue(context.Background(), source, "success", "none")
	}
	return nil
}

// enqueueSharedWriteBytes 投递已编码好的字节帧（房间广播快路径）。
// 与 enqueueSharedWrite 等价的非阻塞语义、统计指标，唯一差别是跳过 per-session 编码。
func (h *ProtoMessageHandler) enqueueSharedWriteBytes(data []byte, source string) error {
	if h.server == nil || h.server.sharedWriter == nil || h.writeSessionID == 0 {
		err := fmt.Errorf("shared writer unavailable")
		h.recordSharedWriterEnqueue(source, err)
		return err
	}
	if err := h.server.sharedWriter.TryEnqueuePreEncoded(h.writeSessionID, data); err != nil {
		h.recordSharedWriterEnqueue(source, err)
		return err
	}
	atomic.AddUint64(&h.batchEnqueued, 1)
	if h.server != nil && h.server.metrics != nil {
		h.server.metrics.RecordSharedWriterEnqueue(context.Background(), source, "success", "none")
	}
	return nil
}

func (h *ProtoMessageHandler) recordSharedWriterEnqueue(source string, err error) {
	atomic.AddUint64(&h.batchEnqueueFailures, 1)
	reason := "unknown"
	if h.isSharedWriterQueueFull(err) {
		reason = "queue_full"
		atomic.AddUint64(&h.batchEnqueueQueueFull, 1)
	} else if err != nil {
		reason = "unavailable"
	}
	if h.server != nil && h.server.metrics != nil {
		h.server.metrics.RecordSharedWriterEnqueue(context.Background(), source, "failure", reason)
	}
	recordCriticalEnqueueFailure(source, reason)
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&h.batchLastFailLogNano)
	if now-last < int64(queueFullLogWindow) {
		return
	}
	if !atomic.CompareAndSwapInt64(&h.batchLastFailLogNano, last, now) {
		return
	}
	remote := ""
	if h.channel != nil {
		remote = h.channel.IP
	}
	wsLog("⚠️  [ProtoHandler] shared writer enqueue failed: source=%s reason=%s err=%v failures=%d queueFull=%d remote=%s",
		source,
		reason,
		err,
		atomic.LoadUint64(&h.batchEnqueueFailures),
		atomic.LoadUint64(&h.batchEnqueueQueueFull),
		remote,
	)
}

func (h *ProtoMessageHandler) isSharedWriterQueueFull(err error) bool {
	return errors.Is(err, errSharedWriterQueueFull)
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

func (h *ProtoMessageHandler) OnCron(session getty.Session) {
	h.logStatsIfNeeded()
	h.checkSessionTimeout(session)
}

func (h *ProtoMessageHandler) logStatsIfNeeded() {
	if !enableWriteTraceLog {
		return
	}

	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&h.writeLastStatLogNano)
	if now-last < int64(10*time.Second) || !atomic.CompareAndSwapInt64(&h.writeLastStatLogNano, last, now) {
		return
	}

	remote := ""
	if h.channel != nil {
		remote = h.channel.IP
	}

	// 写入统计
	if concurrent := atomic.LoadUint64(&h.writeConcurrent); concurrent != 0 {
		wsLog("[stat] ws writes: total=%d concurrent=%d inFlight=%d remote=%s",
			atomic.LoadUint64(&h.writeTotal),
			concurrent,
			atomic.LoadInt32(&h.writeInFlight),
			remote,
		)
	}

	// 批处理统计
	if flushes := atomic.LoadUint64(&h.batchFlushes); flushes != 0 {
		pkgs := atomic.LoadUint64(&h.batchFlushedPkgs)
		avgPkgs := float64(pkgs) / float64(flushes)
		if avgPkgs > 1.05 {
			wsLog("[stat] ws batch: enqueued=%d flushes=%d pkgs=%d avgPkgs=%.2f bytes=%d maxPkgs=%d remote=%s",
				atomic.LoadUint64(&h.batchEnqueued),
				flushes,
				pkgs,
				avgPkgs,
				atomic.LoadUint64(&h.batchFlushedBytes),
				atomic.LoadUint32(&h.batchMaxPkgsPerFlush),
				remote,
			)
		}
		if failures := atomic.LoadUint64(&h.batchEnqueueFailures); failures != 0 {
			wsLog("[stat] ws enqueue failures: total=%d queueFull=%d remote=%s",
				failures,
				atomic.LoadUint64(&h.batchEnqueueQueueFull),
				remote,
			)
		}
	}
}

func (h *ProtoMessageHandler) checkSessionTimeout(session getty.Session) {
	timeout := h.server.config.GettyConfig.SessionTimeout
	if timeout == 0 {
		timeout = 180 * time.Second
	}
	if timeout <= 0 {
		return
	}

	// 取读取和写入活跃时间中较新的
	readActiveTime := session.GetActive()
	lastWriteNano := atomic.LoadInt64(&h.lastWriteTimeNano)
	lastWriteTime := time.Unix(0, lastWriteNano)

	var timeSinceActive time.Duration
	if lastWriteNano > 0 && lastWriteTime.After(readActiveTime) {
		timeSinceActive = time.Since(lastWriteTime)
	} else {
		timeSinceActive = time.Since(readActiveTime)
	}

	if timeSinceActive > timeout {
		wsLog("⏰ [ProtoHandler] Session 超时，关闭连接: %s (超时: %v, 距上次活跃: %v)",
			session.RemoteAddr(), timeout, timeSinceActive)
		session.Close()
	}
}
