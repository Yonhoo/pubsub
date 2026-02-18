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

package types

import (
	"sync"
	"time"
)

// Room 房间信息
type Room struct {
	Mu sync.RWMutex

	ID          string
	Name        string
	Description string
	Users       map[string]*User // user_id -> User
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewRoom 创建新房间
func NewRoom(id, name string) *Room {
	now := time.Now()
	return &Room{
		ID:        id,
		Name:      name,
		Users:     make(map[string]*User),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddUser 添加用户到房间
func (r *Room) AddUser(user *User) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	r.Users[user.ID] = user
	r.UpdatedAt = time.Now()
}

// RemoveUser 从房间移除用户
func (r *Room) RemoveUser(userID string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	delete(r.Users, userID)
	r.UpdatedAt = time.Now()
}

// GetUser 获取用户
func (r *Room) GetUser(userID string) (*User, bool) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	user, ok := r.Users[userID]
	return user, ok
}

// UserCount 获取用户数量
func (r *Room) UserCount() int {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	return len(r.Users)
}

// GetAllUsers 获取所有用户列表
func (r *Room) GetAllUsers() []*User {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	users := make([]*User, 0, len(r.Users))
	for _, u := range r.Users {
		users = append(users, u)
	}
	return users
}

// User 用户信息
type User struct {
	ID       string
	Name     string
	RoomID   string
	NodeID   string // 用户所在的 Connect-Node ID
	Metadata map[string]string
	JoinedAt time.Time
}

// NewUser 创建新用户
func NewUser(id, name, roomID, nodeID string) *User {
	return &User{
		ID:       id,
		Name:     name,
		RoomID:   roomID,
		NodeID:   nodeID,
		Metadata: make(map[string]string),
		JoinedAt: time.Now(),
	}
}

// ConnectNode 长连接节点信息
type ConnectNode struct {
	mu sync.RWMutex

	ID                 string
	Address            string
	MaxConnections     int32
	CurrentConnections int32
	CPUUsage           int32
	MemoryUsage        int32
	RegisteredAt       time.Time
	LastHeartbeat      time.Time
	Active             bool
}

// NewConnectNode 创建新的连接节点
func NewConnectNode(id, address string, maxConn int32) *ConnectNode {
	now := time.Now()
	return &ConnectNode{
		ID:             id,
		Address:        address,
		MaxConnections: maxConn,
		RegisteredAt:   now,
		LastHeartbeat:  now,
		Active:         true,
	}
}

// UpdateHeartbeat 更新心跳信息
func (n *ConnectNode) UpdateHeartbeat(currentConn, cpu, memory int32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.CurrentConnections = currentConn
	n.CPUUsage = cpu
	n.MemoryUsage = memory
	n.LastHeartbeat = time.Now()
}

// IsHealthy 检查节点是否健康
func (n *ConnectNode) IsHealthy() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// 如果超过 30 秒没有心跳，认为不健康
	return n.Active && time.Since(n.LastHeartbeat) < 30*time.Second
}

// GetAddress 获取地址
func (n *ConnectNode) GetAddress() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Address
}
