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
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"google.golang.org/protobuf/proto"
)

// WebSocketClient WebSocket 客户端（连接到 Connect-Node）
type WebSocketClient struct {
	conn     *websocket.Conn
	userID   string
	userName string
	roomID   string
	done     chan struct{}
}

// NewWebSocketClient 创建 WebSocket 客户端
func NewWebSocketClient(url, userID, userName, roomID string) (*WebSocketClient, error) {
	log.Printf("🔌 连接到 Connect-Node: %s", url)
	log.Printf("   用户: %s (%s)", userName, userID)
	log.Printf("   房间: %s", roomID)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}

	client := &WebSocketClient{
		conn:     conn,
		userID:   userID,
		userName: userName,
		roomID:   roomID,
		done:     make(chan struct{}),
	}

	log.Printf("✅ WebSocket 连接成功")
	return client, nil
}

// JoinRoom 加入房间
func (c *WebSocketClient) JoinRoom() error {
	log.Printf("🚪 加入房间: %s", c.roomID)

	// 构造加入房间的 Proto 消息
	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     1, // 1 = 加入房间
		Seq:    1,
		Roomid: c.roomID,
		Userid: c.userID,
		Body:   []byte(c.userName),
	}

	// 序列化
	data, err := proto.Marshal(protoMsg)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	// 构造消息包: [包长度(4字节)] + [消息体]
	msgLen := uint32(len(data))
	packet := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(packet[0:4], msgLen)
	copy(packet[4:], data)

	// 发送二进制消息
	err = c.conn.WriteMessage(websocket.BinaryMessage, packet)
	if err != nil {
		return fmt.Errorf("发送失败: %w", err)
	}

	log.Printf("✅ 加入房间请求已发送")
	return nil
}

// Listen 监听服务器消息
func (c *WebSocketClient) Listen() {
	defer close(c.done)

	log.Printf("👂 开始监听服务器消息...")

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("❌ 读取消息失败: %v", err)
			return
		}

		if messageType != websocket.BinaryMessage {
			log.Printf("⚠️  收到非二进制消息，跳过")
			continue
		}

		// 解析消息包: [包长度(4字节)] + [消息体]
		if len(message) < 4 {
			log.Printf("⚠️  消息太短，跳过")
			continue
		}

		msgLen := binary.BigEndian.Uint32(message[0:4])
		if len(message) < int(4+msgLen) {
			log.Printf("⚠️  消息长度不匹配，跳过")
			continue
		}

		// 反序列化 Proto 消息
		protoMsg := &protocol.Proto{}
		err = proto.Unmarshal(message[4:4+msgLen], protoMsg)
		if err != nil {
			log.Printf("❌ 反序列化失败: %v", err)
			continue
		}

		c.handleMessage(protoMsg)
	}
}

// handleMessage 处理收到的消息
func (c *WebSocketClient) handleMessage(msg *protocol.Proto) {
	switch msg.Op {
	case 2: // 加入房间响应
		log.Printf("✅ 加入房间成功")
		log.Printf("   房间: %s", msg.Roomid)
		log.Printf("   用户: %s", msg.Userid)

	case 3: // 服务器推送消息
		log.Printf("📨 收到推送消息:")
		log.Printf("   房间: %s", msg.Roomid)
		log.Printf("   发送者: %s", msg.Userid)
		log.Printf("   内容: %s", string(msg.Body))

	case 4: // 广播消息
		log.Printf("📢 收到广播消息:")
		log.Printf("   房间: %s", msg.Roomid)
		log.Printf("   内容: %s", string(msg.Body))

	case 5: // 心跳响应
		log.Printf("💓 心跳响应")

	default:
		log.Printf("⚠️  未知消息类型: op=%d", msg.Op)
	}
}

// SendHeartbeat 发送心跳
func (c *WebSocketClient) SendHeartbeat() error {
	protoMsg := &protocol.Proto{
		Ver:    1,
		Op:     5, // 5 = 心跳
		Seq:    int32(time.Now().Unix()),
		Roomid: c.roomID,
		Userid: c.userID,
	}

	data, err := proto.Marshal(protoMsg)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	msgLen := uint32(len(data))
	packet := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(packet[0:4], msgLen)
	copy(packet[4:], data)

	return c.conn.WriteMessage(websocket.BinaryMessage, packet)
}

// StartHeartbeat 启动心跳
func (c *WebSocketClient) StartHeartbeat(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.SendHeartbeat(); err != nil {
				log.Printf("❌ 心跳发送失败: %v", err)
				return
			}
		case <-c.done:
			return
		}
	}
}

// Close 关闭连接
func (c *WebSocketClient) Close() error {
	log.Printf("👋 关闭 WebSocket 连接")
	close(c.done)
	return c.conn.Close()
}

// Wait 等待连接关闭
func (c *WebSocketClient) Wait() {
	<-c.done
}

