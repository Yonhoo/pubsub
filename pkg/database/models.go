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

package database

import (
	"time"

	"gorm.io/gorm"
)

// Room 房间模型
type Room struct {
	ID          string         `gorm:"column:id;primaryKey;size:64" json:"id"`              // 主键，对应数据库的 id 字段
	Name        string         `gorm:"column:name;size:128;not null" json:"name"`
	Description string         `gorm:"column:description;type:text" json:"description"`
	MaxUsers    int            `gorm:"column:max_users;default:100" json:"max_users"`
	CreatedAt   time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// RoomUser 用户-房间关系表（多对多）
type RoomUser struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UserID    string         `gorm:"size:64;not null;index:idx_user_room" json:"user_id"`
	UserName  string         `gorm:"size:128;not null" json:"user_name"`
	RoomID    string         `gorm:"size:64;not null;index:idx_user_room" json:"room_id"`
	NodeID    string         `gorm:"size:64;not null" json:"node_id"`
	JoinedAt  time.Time      `gorm:"not null" json:"joined_at"`
	LeftAt    *time.Time     `json:"left_at,omitempty"`      // NULL 表示在线，非 NULL 表示已离线
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// ConnectNode 连接节点模型
type ConnectNode struct {
	ID                 string    `gorm:"column:id;primaryKey;size:64" json:"id"`
	Address            string    `gorm:"column:address;size:256;not null" json:"address"`
	Region             string    `gorm:"column:region;size:64" json:"region"`                      // 数据库有此字段
	MaxConnections     int       `gorm:"column:max_connections;default:10000" json:"max_connections"` // 修正默认值
	CurrentConnections int       `gorm:"column:current_connections;default:0" json:"current_connections"`
	CPUUsage           float32   `gorm:"column:cpu_usage;default:0" json:"cpu_usage"`
	MemoryUsage        float32   `gorm:"column:memory_usage;default:0" json:"memory_usage"`
	Status             string    `gorm:"column:status;size:32;default:'online'" json:"status"` // online, offline, unhealthy
	LastHeartbeat      time.Time `gorm:"column:last_heartbeat" json:"last_heartbeat"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
	// 注意：数据库表没有 deleted_at 字段，不使用软删除
}

// TableName 指定表名
func (Room) TableName() string {
	return "rooms"
}

func (RoomUser) TableName() string {
	return "room_users"
}

func (ConnectNode) TableName() string {
	return "connect_nodes"
}

// BeforeCreate GORM 钩子
func (r *Room) BeforeCreate(tx *gorm.DB) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now()
	}
	return nil
}

func (ru *RoomUser) BeforeCreate(tx *gorm.DB) error {
	if ru.JoinedAt.IsZero() {
		ru.JoinedAt = time.Now()
	}
	// LeftAt 为 NULL 表示在线状态
	return nil
}

func (cn *ConnectNode) BeforeCreate(tx *gorm.DB) error {
	if cn.CreatedAt.IsZero() {
		cn.CreatedAt = time.Now()
	}
	if cn.UpdatedAt.IsZero() {
		cn.UpdatedAt = time.Now()
	}
	if cn.LastHeartbeat.IsZero() {
		cn.LastHeartbeat = time.Now()
	}
	if cn.Status == "" {
		cn.Status = "online"
	}
	return nil
}
