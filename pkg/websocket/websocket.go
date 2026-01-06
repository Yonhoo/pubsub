// // Copyright 2023 LiveKit, Inc.
// //
// // Licensed under the Apache License, Version 2.0 (the "License");
// // you may not use this file except in compliance with the License.
// // You may obtain a copy of the License at
// //
// //     http://www.apache.org/licenses/LICENSE-2.0
// //
// // Unless required by applicable law or agreed to in writing, software
// // distributed under the License is distributed on an "AS IS" BASIS,
// // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// // See the License for the specific language governing permissions and
// // limitations under the License.

package websocket

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"sync"
// 	"time"

// 	"github.com/gorilla/websocket"
// 	pb "github.com/livekit/psrpc/examples/pubsub/proto"
// )

// // UserEvent 用户事件（用于回调函数，便于扩展）
// type UserEvent struct {
// 	UserID   string            // 用户 ID
// 	UserName string            // 用户名称
// 	RoomID   string            // 房间 ID
// 	Metadata map[string]string // 元数据（可扩展新字段）
// }

// // Client WebSocket 客户端连接
// type Client struct {
// 	UserID   string
// 	UserName string
// 	RoomID   string
// 	Metadata map[string]string
// 	Conn     *websocket.Conn
// 	Send     chan []byte             // 发送队列
// 	Receive  chan *pb.MessageContent // 接收消息队列（用于推送）
// 	Manager  *Manager
// 	mu       sync.Mutex
// }

// // Message 前端发送的消息格式
// type Message struct {
// 	Type     string                 `json:"type"`
// 	RoomID   string                 `json:"room_id,omitempty"`
// 	UserName string                 `json:"user_name,omitempty"`
// 	Data     map[string]interface{} `json:"data,omitempty"`
// }

// // PushMessage 推送给客户端的消息格式
// type PushMessage struct {
// 	Type      string                 `json:"type"`
// 	RoomID    string                 `json:"room_id,omitempty"`
// 	UserID    string                 `json:"user_id,omitempty"`
// 	Timestamp int64                  `json:"timestamp"`
// 	Data      map[string]interface{} `json:"data"`
// 	Metadata  map[string]string      `json:"metadata,omitempty"`
// }

// var (
// 	upgrader = websocket.Upgrader{
// 		ReadBufferSize:  1024,
// 		WriteBufferSize: 1024,
// 		CheckOrigin: func(r *http.Request) bool {
// 			return true // 允许所有来源（生产环境需要严格校验）
// 		},
// 	}
// )

// // NewClient 创建新的 WebSocket 客户端
// func NewClient(userID, userName, roomID string, metadata map[string]string, conn *websocket.Conn, manager *Manager) *Client {
// 	return &Client{
// 		UserID:   userID,
// 		UserName: userName,
// 		RoomID:   roomID,
// 		Metadata: metadata,
// 		Conn:     conn,
// 		Send:     make(chan []byte, 256),
// 		Receive:  make(chan *pb.MessageContent, 256), // 接收消息队列
// 		Manager:  manager,
// 	}
// }

// // ReadPump 读取客户端消息
// func (c *Client) ReadPump() {
// 	defer func() {
// 		c.Manager.Unregister <- c
// 		c.Conn.Close()
// 	}()

// 	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
// 	c.Conn.SetPongHandler(func(string) error {
// 		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
// 		return nil
// 	})

// 	for {
// 		_, message, err := c.Conn.ReadMessage()
// 		if err != nil {
// 			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
// 				log.Printf("❌ [WebSocket] 读取消息错误: %v (user: %s)\n", err, c.UserID)
// 			}
// 			break
// 		}

// 		// 解析客户端消息
// 		var msg Message
// 		if err := json.Unmarshal(message, &msg); err != nil {
// 			log.Printf("⚠️  [WebSocket] 解析消息失败: %v (user: %s)\n", err, c.UserID)
// 			continue
// 		}

// 		// 处理不同类型的消息
// 		c.handleMessage(&msg)
// 	}
// }

// // WritePump 发送消息给客户端
// func (c *Client) WritePump() {
// 	ticker := time.NewTicker(54 * time.Second)
// 	defer func() {
// 		ticker.Stop()
// 		c.Conn.Close()
// 	}()

// 	for {
// 		select {
// 		case message, ok := <-c.Send:
// 			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
// 			if !ok {
// 				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
// 				return
// 			}

// 			w, err := c.Conn.NextWriter(websocket.TextMessage)
// 			if err != nil {
// 				return
// 			}
// 			w.Write(message)

// 			// 批量发送队列中的消息
// 			n := len(c.Send)
// 			for i := 0; i < n; i++ {
// 				w.Write([]byte{'\n'})
// 				w.Write(<-c.Send)
// 			}

// 			if err := w.Close(); err != nil {
// 				return
// 			}

// 		case msgContent := <-c.Receive:
// 			// 从 Receive chan 接收推送消息
// 			msg := &PushMessage{
// 				Type:      convertMessageType(msgContent.Type),
// 				RoomID:    c.RoomID,
// 				UserID:    c.UserID,
// 				Timestamp: msgContent.Timestamp,
// 				Data: map[string]interface{}{
// 					"content": string(msgContent.Data),
// 				},
// 				Metadata: msgContent.Metadata,
// 			}

// 			data, err := json.Marshal(msg)
// 			if err != nil {
// 				log.Printf("❌ [WebSocket] 序列化推送消息失败: %v\n", err)
// 				continue
// 			}

// 			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
// 			if err := c.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
// 				log.Printf("❌ [WebSocket] 发送推送消息失败: %v\n", err)
// 				return
// 			}

// 		case <-ticker.C:
// 			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
// 			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
// 				return
// 			}
// 		}
// 	}
// }

// // SendMessage 发送消息给客户端（线程安全）
// func (c *Client) SendMessage(msg *PushMessage) error {
// 	c.mu.Lock()
// 	defer c.mu.Unlock()

// 	data, err := json.Marshal(msg)
// 	if err != nil {
// 		return fmt.Errorf("序列化消息失败: %w", err)
// 	}

// 	select {
// 	case c.Send <- data:
// 		return nil
// 	default:
// 		// 发送队列已满
// 		log.Printf("⚠️  [WebSocket] 发送队列已满，丢弃消息 (user: %s)\n", c.UserID)
// 		return fmt.Errorf("send queue full")
// 	}
// }

// // handleMessage 处理客户端发送的消息
// func (c *Client) handleMessage(msg *Message) {
// 	switch msg.Type {
// 	case "ping":
// 		// 心跳响应
// 		c.SendMessage(&PushMessage{
// 			Type:      "pong",
// 			Timestamp: time.Now().Unix(),
// 			Data:      map[string]interface{}{"status": "ok"},
// 		})

// 	default:
// 		log.Printf("⚠️  [WebSocket] 未知消息类型: %s (user: %s)\n", msg.Type, c.UserID)
// 	}

// 	// 注意：不再处理 join_room 和 leave_room
// 	// 连接建立 = 加入房间，连接断开 = 离开房间
// }

// // Manager WebSocket 连接管理器
// type Manager struct {
// 	// 房间到用户连接的映射（roomID -> userID -> *Client）
// 	// 这是唯一的存储，用于快速查找房间内的所有用户连接
// 	// 注意：同一个 userID 可以在不同房间中，通过 roomID+userID 唯一定位
// 	RoomClients map[string]map[string]*Client
// 	roomMu      sync.RWMutex

// 	// 注册/注销通道
// 	Register   chan *Client
// 	Unregister chan *Client

// 	// 回调函数（建立连接即加入房间）
// 	OnUserJoinRoom  func(event *UserEvent) // 用户加入房间（连接建立）
// 	OnUserLeaveRoom func(event *UserEvent) // 用户离开房间（连接断开）
// }

// // NewManager 创建 WebSocket 管理器
// func NewManager() *Manager {
// 	return &Manager{
// 		RoomClients: make(map[string]map[string]*Client),
// 		Register:    make(chan *Client),
// 		Unregister:  make(chan *Client),
// 	}
// }

// // Run 运行管理器
// func (m *Manager) Run(ctx context.Context) {
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Printf("🛑 [WebSocket] Manager 停止运行\n")
// 			return

// 		case client := <-m.Register:
// 			m.registerClient(client)

// 		case client := <-m.Unregister:
// 			m.unregisterClient(client)
// 		}
// 	}
// }

// // registerClient 注册客户端（连接建立 = 加入房间）
// func (m *Manager) registerClient(client *Client) {
// 	// 只添加到房间映射（roomID + userID 组合唯一定位）
// 	m.roomMu.Lock()
// 	if _, ok := m.RoomClients[client.RoomID]; !ok {
// 		m.RoomClients[client.RoomID] = make(map[string]*Client)
// 	}
// 	m.RoomClients[client.RoomID][client.UserID] = client
// 	m.roomMu.Unlock()

// 	log.Printf("✅ [WebSocket] 用户加入房间: %s (%s) -> %s, 房间人数: %d\n",
// 		client.UserName, client.UserID, client.RoomID, m.GetRoomUserCount(client.RoomID))

// 	// 触发加入房间回调（连接建立即加入房间）
// 	if m.OnUserJoinRoom != nil {
// 		m.OnUserJoinRoom(&UserEvent{
// 			UserID:   client.UserID,
// 			UserName: client.UserName,
// 			RoomID:   client.RoomID,
// 			Metadata: client.Metadata,
// 		})
// 	}
// }

// // unregisterClient 注销客户端（连接断开 = 离开房间）
// func (m *Manager) unregisterClient(client *Client) {
// 	// 从房间映射移除（通过 roomID + userID 定位）
// 	m.roomMu.Lock()
// 	if roomClients, ok := m.RoomClients[client.RoomID]; ok {
// 		if _, exists := roomClients[client.UserID]; exists {
// 			delete(roomClients, client.UserID)
// 			close(client.Send)
// 			close(client.Receive)

// 			// 如果房间为空，删除房间
// 			if len(roomClients) == 0 {
// 				delete(m.RoomClients, client.RoomID)
// 				log.Printf("🗑️  [WebSocket] 房间已空，移除: %s\n", client.RoomID)
// 			}
// 		}
// 	}
// 	m.roomMu.Unlock()

// 	log.Printf("🔴 [WebSocket] 用户离开房间: %s (%s) <- %s, 房间人数: %d\n",
// 		client.UserName, client.UserID, client.RoomID, m.GetRoomUserCount(client.RoomID))

// 	// 触发离开房间回调（连接断开即离开房间）
// 	if m.OnUserLeaveRoom != nil {
// 		m.OnUserLeaveRoom(&UserEvent{
// 			UserID:   client.UserID,
// 			UserName: client.UserName,
// 			RoomID:   client.RoomID,
// 			Metadata: client.Metadata,
// 		})
// 	}
// }

// // GetRoomClients 获取房间内的所有客户端连接（用于推送消息）
// func (m *Manager) GetRoomClients(roomID string) []*Client {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	if clients, ok := m.RoomClients[roomID]; ok {
// 		result := make([]*Client, 0, len(clients))
// 		for _, client := range clients {
// 			result = append(result, client)
// 		}
// 		return result
// 	}
// 	return []*Client{}
// }

// // GetRoomUserCount 获取房间用户数
// func (m *Manager) GetRoomUserCount(roomID string) int {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	if clients, ok := m.RoomClients[roomID]; ok {
// 		return len(clients)
// 	}
// 	return 0
// }

// // GetClient 获取客户端连接（需要 roomID + userID）
// func (m *Manager) GetClient(roomID, userID string) (*Client, bool) {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	if roomClients, ok := m.RoomClients[roomID]; ok {
// 		if client, exists := roomClients[userID]; exists {
// 			return client, true
// 		}
// 	}
// 	return nil, false
// }

// // GetRoomUsers 获取房间内的所有用户 ID
// func (m *Manager) GetRoomUsers(roomID string) []string {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	if clients, ok := m.RoomClients[roomID]; ok {
// 		userIDs := make([]string, 0, len(clients))
// 		for userID := range clients {
// 			userIDs = append(userIDs, userID)
// 		}
// 		return userIDs
// 	}
// 	return []string{}
// }

// // GetConnectionCount 获取当前连接数（遍历所有房间）
// func (m *Manager) GetConnectionCount() int {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	count := 0
// 	for _, roomClients := range m.RoomClients {
// 		count += len(roomClients)
// 	}
// 	return count
// }

// // GetRoomCount 获取房间数
// func (m *Manager) GetRoomCount() int {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()
// 	return len(m.RoomClients)
// }

// // GetAllRooms 获取所有房间 ID 列表（用于同步到 ETCD）
// func (m *Manager) GetAllRooms() []string {
// 	m.roomMu.RLock()
// 	defer m.roomMu.RUnlock()

// 	rooms := make([]string, 0, len(m.RoomClients))
// 	for roomID := range m.RoomClients {
// 		rooms = append(rooms, roomID)
// 	}
// 	return rooms
// }

// // PushToUser 推送消息给指定用户（需要 roomID + userID）
// func (m *Manager) PushToUser(roomID, userID string, msgContent *pb.MessageContent) error {
// 	client, ok := m.GetClient(roomID, userID)
// 	if !ok {
// 		return fmt.Errorf("用户不在房间: roomID=%s, userID=%s", roomID, userID)
// 	}

// 	// 往用户的 Receive chan 发送消息
// 	select {
// 	case client.Receive <- msgContent:
// 		return nil
// 	default:
// 		return fmt.Errorf("用户接收队列已满: roomID=%s, userID=%s", roomID, userID)
// 	}
// }

// // PushToRoom 推送消息给房间内所有用户（通过房间映射快速查找）
// func (m *Manager) PushToRoom(roomID string, msgContent *pb.MessageContent, excludeUserIDs []string) (int, int) {
// 	// 通过 RoomClients 直接获取房间内的所有连接
// 	clients := m.GetRoomClients(roomID)

// 	excludeMap := make(map[string]bool)
// 	for _, uid := range excludeUserIDs {
// 		excludeMap[uid] = true
// 	}

// 	delivered := 0
// 	failed := 0

// 	//TODO fix user not ack msg
// 	// 直接循环房间内的客户端连接，往各自的 Receive chan 发送消息
// 	for _, client := range clients {
// 		// 跳过排除的用户
// 		if excludeMap[client.UserID] {
// 			continue
// 		}

// 		// 往客户端的 Receive chan 发送消息
// 		select {
// 		case client.Receive <- msgContent:
// 			delivered++
// 		default:
// 			failed++
// 			log.Printf("⚠️  [WebSocket] 推送失败（队列满）: %s -> %s\n", roomID, client.UserID)
// 		}
// 	}

// 	log.Printf("📤 [WebSocket] 房间推送完成: %s, 成功: %d, 失败: %d\n", roomID, delivered, failed)
// 	return delivered, failed
// }

// // HandleWebSocket HTTP WebSocket 处理器
// func (m *Manager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
// 	// 从 URL 参数获取用户信息
// 	query := r.URL.Query()
// 	userID := query.Get("user_id")
// 	userName := query.Get("user_name")
// 	roomID := query.Get("room_id")

// 	if userID == "" {
// 		http.Error(w, "缺少 user_id 参数", http.StatusBadRequest)
// 		return
// 	}

// 	if roomID == "" {
// 		http.Error(w, "缺少 room_id 参数", http.StatusBadRequest)
// 		return
// 	}

// 	// 解析 metadata（从 URL 参数，所有非标准参数都作为 metadata）
// 	metadata := make(map[string]string)
// 	for key, values := range query {
// 		// 跳过标准参数
// 		if key != "user_id" && key != "user_name" && key != "room_id" && len(values) > 0 {
// 			metadata[key] = values[0]
// 		}
// 	}

// 	// 升级 HTTP 连接为 WebSocket
// 	conn, err := upgrader.Upgrade(w, r, nil)
// 	if err != nil {
// 		log.Printf("❌ [WebSocket] 升级连接失败: %v\n", err)
// 		return
// 	}

// 	// 创建客户端（连接建立即加入房间）
// 	client := NewClient(userID, userName, roomID, metadata, conn, m)

// 	// 注册客户端（会触发 OnUserJoinRoom 回调）
// 	m.Register <- client

// 	// 启动读写协程
// 	go client.WritePump()
// 	go client.ReadPump()
// }

// // convertMessageType 转换消息类型
// func convertMessageType(msgType pb.MessageType) string {
// 	switch msgType {
// 	case pb.MessageType_TEXT:
// 		return "text"
// 	case pb.MessageType_AUDIO:
// 		return "audio"
// 	case pb.MessageType_VIDEO:
// 		return "video"
// 	case pb.MessageType_TRANSLATION:
// 		return "translation"
// 	case pb.MessageType_SYSTEM:
// 		return "system"
// 	default:
// 		return "unknown"
// 	}
// }
