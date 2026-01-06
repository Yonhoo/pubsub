package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	getty "github.com/AlexStocks/getty/transport"
	gxnet "github.com/AlexStocks/goext/net"
	gxsync "github.com/dubbogo/gost/sync"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"log"
	"net"
	"sync"
	"time"
)

const (
	maxInt = 1<<31 - 1
	r      = 0
)

var serverList []getty.Server

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
				log.Printf("⚠️  SetNoDelay 失败: %v", err)
			}
			if err := tcpConn.SetKeepAlive(server.config.GettyConfig.GettySessionParam.TcpKeepAlive); err != nil {
				log.Printf("⚠️  SetKeepAlive 失败: %v", err)
			}
			if err := tcpConn.SetReadBuffer(server.config.GettyConfig.GettySessionParam.TcpRBufSize); err != nil {
				log.Printf("⚠️  SetReadBuffer 失败: %v", err)
			}
			if err := tcpConn.SetWriteBuffer(server.config.GettyConfig.GettySessionParam.TcpWBufSize); err != nil {
				log.Printf("⚠️  SetWriteBuffer 失败: %v", err)
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
		log.Printf("🔌 启动 Getty WebSocket 服务器: %s (路径: /connect)\n", addr)
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
	log.Printf("✅ [ProtoHandler] Session 打开: %s", session.Stat())

	// 启动 dispatchWebsocket 协程处理客户端消息
	go h.dispatchWebsocket(session)

	return nil
}

func (h *ProtoMessageHandler) dispatchWebsocket(session getty.Session) {
	var (
		err    error
		p      *proto.Proto
		finish bool
	)

	log.Printf("🚀 [ProtoHandler] dispatchWebsocket 启动")

	for {
		// 1. 等待信号（阻塞直到有新消息或关闭）
		p = h.channel.Ready()

		switch p {
		case proto.ProtoFinish:
			log.Printf("👋 [ProtoHandler] dispatchWebsocket 收到结束信号")
			finish = true
			goto close

		case proto.ProtoReady:
			for {
				// Get() 获取 rp 位置的 Proto 指针
				p, err = h.channel.ClientReqQueue.Get()
				if err != nil {
					// Ring Buffer 空了，跳出内层循环，继续等待信号
					break
				}

				// 3. 处理消息（根据 op 路由到不同的 handler）
				if err = h.processClientRequest(session, p); err != nil {
					log.Printf("❌ [ProtoHandler] 处理消息失败: op=%d, seq=%d, err=%v", p.Op, p.Seq, err)
				}

				// 4. GetAdv() 推进 rp 指针（⚠️ 重要：此时 ReadBuffer 可以复用了）
				h.channel.ClientReqQueue.GetAdv()

				log.Printf("✅ [ProtoHandler] 消息处理完成: op=%d, seq=%d, rp++", p.Op, p.Seq)
			}
		
		default:
			// 服务端推送的消息（通过 Broadcast/BroadcastRoom 推送）
			log.Printf("📤 [ProtoHandler] 收到服务端推送消息: op=%d, seq=%d, roomId=%s, bodyLen=%d", 
				p.Op, p.Seq, p.Roomid, len(p.Body))
			
			// 直接发送给客户端
			_, _, err := session.WritePkg(p, 0)
			if err != nil {
				log.Printf("❌ [ProtoHandler] 发送服务端推送消息失败: %v", err)
			} else {
				log.Printf("✅ [ProtoHandler] 服务端推送消息已发送给客户端")
			}
		}

	}

close:
	if finish {
		log.Printf("🛑 [ProtoHandler] dispatchWebsocket 正常退出")
		session.Close()
		h.protoPackageHandler.Close()
	}
}

// processClientRequest 处理客户端请求
func (h *ProtoMessageHandler) processClientRequest(session getty.Session, p *proto.Proto) error {
	log.Printf("📨 [ProtoHandler] 处理客户端消息: op=%d, seq=%d, roomId=%s, userId=%s, bodyLen=%d",
		p.Op, p.Seq, p.Roomid, p.Userid, len(p.Body))

	// TODO: 根据 op 路由到不同的业务 handler
	switch p.Op {
	case 1: // 加入房间
		log.Printf("🏠 [ProtoHandler] 加入房间: roomId=%s, userId=%s", p.Roomid, p.Userid)
		// 这里可以调用具体的业务逻辑

		joinRoomRequest := controller.JoinRoomRequest{
			RoomId: p.Roomid,
			UserId: p.Userid,
		}

		log.Printf("🔄 [ProtoHandler] 调用 Controller.JoinRoom...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		_, err := h.server.controllerClient.JoinRoom(ctx, &joinRoomRequest)
		if err != nil {
			log.Printf("❌ [ProtoHandler] join room error: %s", err.Error())
			return err
		}
		log.Printf("✅ [ProtoHandler] JoinRoom 调用成功")

		// 加入房间成功后，订阅消息推送操作码
		// Op=2: OP_SEND_MSG (服务端推送的消息)
		h.channel.Watch(2)
		log.Printf("✅ [ProtoHandler] 已订阅消息推送: op=2")

		// 处理完后，如果需要回复客户端
		// 准备响应消息
		resp := &proto.Proto{
			Ver:    p.Ver,
			Op:     p.Op + 1, // 回复 op
			Seq:    p.Seq,
			Roomid: p.Roomid,
			Userid: p.Userid,
			Body:   []byte("join room success"),
		}

		// 发送响应（通过 getty 的 WritePkg）
		log.Printf("📤 [ProtoHandler] 发送加入房间响应...")
		_, _, err = session.WritePkg(resp, 0)
		if err != nil {
			log.Printf("❌ [ProtoHandler] 发送响应失败: %v", err)
			return err
		}
		log.Printf("✅ [ProtoHandler] 加入房间响应已发送")

	case 5: // 心跳包
		log.Printf("💓 [ProtoHandler] 收到心跳: roomId=%s, userId=%s", p.Roomid, p.Userid)
		// 心跳包不需要特殊处理，Getty 会自动更新 session 活跃时间
		// 可以选择性地回复心跳确认
		resp := &proto.Proto{
			Ver:    p.Ver,
			Op:     6, // 心跳响应
			Seq:    p.Seq,
			Roomid: p.Roomid,
			Userid: p.Userid,
			Body:   nil,
		}
		_, _, err := session.WritePkg(resp, 0)
		if err != nil {
			log.Printf("⚠️  [ProtoHandler] 发送心跳响应失败: %v", err)
		}
		return nil

	default:
		log.Printf("⚠️  [ProtoHandler] 未知 op: %d", p.Op)
		return nil
	}

	return nil
}

func (h *ProtoMessageHandler) OnError(session getty.Session, err error) {
	log.Printf("❌ [ProtoHandler] Session 错误: %s, err=%v", session.Stat(), err)

	// 通知 dispatchWebsocket 退出
	h.channel.Close()

	// 归还 ReadBuffer
	h.protoPackageHandler.Close()
}

func (h *ProtoMessageHandler) OnClose(session getty.Session) {
	log.Printf("👋 [ProtoHandler] Session 关闭: %s", session.Stat())

	// 通知 dispatchWebsocket 退出
	h.channel.Close()

	// 归还 ReadBuffer
	h.protoPackageHandler.Close()
}

func (h *ProtoMessageHandler) authWebsocket(p *proto.Proto, session getty.Session) error {
	// 使用 op=1（加入房间）作为鉴权操作
	// 或者 op=0（如果有专门的认证操作码）
	if p.Roomid != "" && p.Userid != "" && (p.Op == proto.OpAuth || p.Op == 1) {
		// redis check login session (这里可以添加实际的认证逻辑)

		h.roomId = p.Roomid
		h.clientId = p.Userid

		h.bucket = h.server.Bucket(p.Userid)

		//connectNodeServer := session.GetAttribute("server").(*ConnectNodeServer)
		h.bucket.Put(p.Roomid, h.channel)

		h.auth = true
		log.Printf("✅ [ProtoHandler] 鉴权成功: roomId=%s, userId=%s", p.Roomid, p.Userid)
		return nil
	}

	return fmt.Errorf("auth failed: op=%d, roomId=%s, userId=%s", p.Op, p.Roomid, p.Userid)
}

func (h *ProtoMessageHandler) OnMessage(session getty.Session, pkg any) {
	p, ok := pkg.(*proto.Proto)
	if !ok {
		log.Printf("❌ [ProtoHandler] 非法包类型: %#v", pkg)
		return
	}

	// 鉴权检查
	if !h.auth {
		err := h.authWebsocket(p, session)
		if err != nil {
			log.Printf("❌ [ProtoHandler] 鉴权失败: %v", err)
			return
		}
	}

	// 将消息放入 CliProto Ring Buffer
	// 1. Set() 获取 wp 位置的 Proto 指针
	cliproto, err := h.channel.ClientReqQueue.Set()
	if err != nil {
		// Ring Buffer 满了，丢弃消息或等待
		log.Printf("⚠️  [ProtoHandler] ClientReqQueue 已满，丢弃消息: op=%d, seq=%d", p.Op, p.Seq)
		return
	}

	// 2. 拷贝数据到 Ring Buffer（浅拷贝，Body 仍然引用 ReadBuffer）
	*cliproto = *p

	// 3. SetAdv() 推进 wp 指针
	h.channel.ClientReqQueue.SetAdv()

	// 4. Signal() 通知 dispatchWebsocket 有新数据
	h.channel.Signal()

	log.Printf("✅ [ProtoHandler] 消息入队: op=%d, seq=%d, roomId=%s, userId=%s, bodyLen=%d",
		p.Op, p.Seq, p.Roomid, p.Userid, len(p.Body))
}

func writeResp(session getty.Session, resp *proto.Proto) {
	if _, _, err := session.WritePkg(resp, 5*time.Second); err != nil {
		log.Printf("send failed: %v", err)
	}
}

func (h *ProtoMessageHandler) OnCron(session getty.Session) {
	activeTime := session.GetActive()
	// 使用配置的 session_timeout（默认 60 秒）
	timeout := h.server.config.GettyConfig.SessionTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	if time.Since(activeTime) > timeout {
		log.Printf("⏰ [ProtoHandler] Session 超时，关闭连接: %s (超时: %v)", session.RemoteAddr(), timeout)
		session.Close()
	}
}
