// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"log"
	"sync"
	"time"

	getty "github.com/AlexStocks/getty/transport"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

// ProtoResponse Proto 消息的响应结构体
type ProtoResponse struct {
	Code    int    `json:"code"`              // 响应码：0=成功，非0=失败
	Message string `json:"message"`           // 响应消息（简短描述）
	Text    string `json:"text,omitempty"`    // 详细文本（可选）
}

// GettyWebSocketClient Getty WebSocket 客户端
type GettyWebSocketClient struct {
	session    getty.Session
	userID     string
	userName   string
	roomID     string
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.RWMutex
}

// NewGettyWebSocketClient 创建 Getty WebSocket 客户端
func NewGettyWebSocketClient(addr, userID, userName, roomID string) (*GettyWebSocketClient, error) {
	// Getty 要求地址格式为 ws://host:port/path
	if len(addr) > 0 && addr[:5] != "ws://" && addr[:6] != "wss://" {
		addr = "ws://" + addr + "/connect"
	}

	log.Printf("🔌 连接到 Connect-Node (Getty): %s", addr)
	log.Printf("   用户: %s (%s)", userName, userID)
	log.Printf("   房间: %s", roomID)

	client := &GettyWebSocketClient{
		userID:   userID,
		userName: userName,
		roomID:   roomID,
		done:     make(chan struct{}),
	}

	// 创建 Getty WebSocket 客户端
	wsClient := getty.NewWSClient(
		getty.WithServerAddress(addr),
		getty.WithConnectionNumber(1),
	)

	// 设置会话回调
	wsClient.RunEventLoop(func(session getty.Session) error {
		log.Printf("✅ Getty Session 创建: %s", session.Stat())

		// 配置 session
		session.SetName("pubsub-client")
		session.SetMaxMsgLen(1024 * 1024) // 1MB

		var readerPool pkg.Pool
		var writePool pkg.Pool

		readerPool.Init(10, 256)
		writePool.Init(10, 256)

		session.SetPkgHandler(gettypkg.NewProtoPackageHandler(&readerPool, &writePool))
		session.SetEventListener(client)
		session.SetReadTimeout(60 * time.Second)
		session.SetWriteTimeout(60 * time.Second)
		session.SetCronPeriod(30 * 1000) // 30s 心跳
		session.SetWaitTime(60 * time.Second)

		client.mu.Lock()
		client.session = session
		client.mu.Unlock()

		return nil
	})

	// 等待连接建立
	time.Sleep(2 * time.Second)

	if client.session == nil {
		return nil, fmt.Errorf("连接失败: session 未创建")
	}

	log.Printf("✅ Getty WebSocket 连接成功")
	return client, nil
}

// OnOpen Getty 会话打开回调
func (c *GettyWebSocketClient) OnOpen(session getty.Session) error {
	log.Printf("✅ [Getty] Session 打开: %s", session.Stat())
	return nil
}

// OnError Getty 错误回调
func (c *GettyWebSocketClient) OnError(session getty.Session, err error) {
	log.Printf("❌ [Getty] Session 错误: %v", err)
}

// OnClose Getty 会话关闭回调
func (c *GettyWebSocketClient) OnClose(session getty.Session) {
	log.Printf("👋 [Getty] Session 关闭: %s", session.Stat())
	// 使用 sync.Once 确保 channel 只关闭一次
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

// OnMessage Getty 消息回调
func (c *GettyWebSocketClient) OnMessage(session getty.Session, pkg interface{}) {
	// pkg 现在是 *ProtoWithBuffer 类型
	pwb, ok := pkg.(*gettypkg.ProtoWithBuffer)
	if !ok {
		log.Printf("⚠️  收到非 ProtoWithBuffer 消息，跳过: %T", pkg)
		return
	}

	// 获取 Proto
	protoMsg := pwb.Proto

	log.Printf("📥 [Client] 收到消息: op=%d, seq=%d, roomId=%s, userId=%s, bodyLen=%d",
		protoMsg.Op, protoMsg.Seq, protoMsg.Roomid, protoMsg.Userid, len(protoMsg.Body))

	// 处理消息
	c.handleMessage(protoMsg)

	// ⚠️ 重要：处理完消息后，立即释放 Buffer
	pwb.Release()
}

// OnCron Getty 定时回调（心跳）
func (c *GettyWebSocketClient) OnCron(session getty.Session) {
	if err := c.SendHeartbeat(); err != nil {
		log.Printf("❌ 心跳发送失败: %v", err)
	}
}

// JoinRoom 加入房间
func (c *GettyWebSocketClient) JoinRoom() error {
	log.Printf("🚪 加入房间: %s", c.roomID)

	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("session 未连接")
	}

	// 构造加入房间的 Proto 消息
	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     1, // 1 = 加入房间
		Seq:    1,
		Roomid: c.roomID,
		Userid: c.userID,
		Body:   []byte(c.userName),
	}

	// 发送消息
	_, _, err := session.WritePkg(protoMsg, 5*time.Second)
	if err != nil {
		return fmt.Errorf("发送失败: %w", err)
	}

	log.Printf("✅ 加入房间请求已发送")
	return nil
}

// SendHeartbeat 发送心跳
func (c *GettyWebSocketClient) SendHeartbeat() error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("session 未连接")
	}

	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     5, // 5 = 心跳
		Seq:    int32(time.Now().Unix()),
		Roomid: c.roomID,
		Userid: c.userID,
	}

	_, _, err := session.WritePkg(protoMsg, 5*time.Second)
	return err
}

// handleMessage 处理收到的消息
func (c *GettyWebSocketClient) handleMessage(msg *protocol.Proto) {
	switch msg.Op {
	case 2: // 加入房间响应 或 广播消息
		if len(msg.Body) > 0 {
			// 尝试解析为 JSON 响应（加入房间响应）
			var resp ProtoResponse
			if err := json.Unmarshal(msg.Body, &resp); err == nil {
				// 成功解析为 JSON，检查是否有 code 字段（响应格式）
				// 注意：即使 code 为 0，只要 Message 字段存在，就认为是响应
				if resp.Message != "" {
					// 是 JSON 响应格式
					if resp.Code == 0 {
						log.Printf("✅ 加入房间成功")
						log.Printf("   房间: %s", msg.Roomid)
						log.Printf("   用户: %s", msg.Userid)
						log.Printf("   消息: %s", resp.Message)
						if resp.Text != "" {
							log.Printf("   详情: %s", resp.Text)
						}
					} else {
						log.Printf("❌ 加入房间失败 [code=%d]", resp.Code)
						log.Printf("   房间: %s", msg.Roomid)
						log.Printf("   用户: %s", msg.Userid)
						log.Printf("   错误: %s", resp.Message)
						if resp.Text != "" {
							log.Printf("   详情: %s", resp.Text)
						}
					}
				} else {
					// JSON 格式但不是响应格式，当作广播消息
					log.Printf("📢 收到广播消息:")
					log.Printf("   房间: %s", msg.Roomid)
					log.Printf("   内容: %s", string(msg.Body))
				}
			} else {
				// 不是 JSON 格式，是广播消息
				log.Printf("📢 收到广播消息:")
				log.Printf("   房间: %s", msg.Roomid)
				log.Printf("   内容: %s", string(msg.Body))
			}
		} else {
			// Body 为空，可能是旧格式的成功响应
			log.Printf("✅ 加入房间成功（旧格式）")
			log.Printf("   房间: %s", msg.Roomid)
			log.Printf("   用户: %s", msg.Userid)
		}

	case 3: // 服务器推送消息
		log.Printf("📨 收到推送消息:")
		log.Printf("   房间: %s", msg.Roomid)
		log.Printf("   发送者: %s", msg.Userid)
		log.Printf("   内容: %s", string(msg.Body))

	case 4: // 广播消息
		log.Printf("📢 收到广播消息:")
		log.Printf("   房间: %s", msg.Roomid)
		log.Printf("   内容: %s", string(msg.Body))

	case 5: // 心跳请求（不应该收到）
		log.Printf("⚠️  收到心跳请求（服务器不应该发送）: op=%d", msg.Op)

	case 6: // 心跳响应
		log.Printf("💓 收到心跳响应: seq=%d", msg.Seq)

	default:
		log.Printf("⚠️  未知消息类型: op=%d, seq=%d, body=%s", msg.Op, msg.Seq, string(msg.Body))
	}
}

// Close 关闭连接
func (c *GettyWebSocketClient) Close() error {
	log.Printf("👋 关闭 Getty WebSocket 连接")

	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session != nil {
		session.Close()
	}

	return nil
}

// Wait 等待连接关闭
func (c *GettyWebSocketClient) Wait() {
	<-c.done
}

// SendMessage 发送自定义消息
func (c *GettyWebSocketClient) SendMessage(op int32, body []byte) error {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("session 未连接")
	}

	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     op,
		Seq:    int32(time.Now().Unix()),
		Roomid: c.roomID,
		Userid: c.userID,
		Body:   body,
	}

	_, _, err := session.WritePkg(protoMsg, 5*time.Second)
	return err
}
