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
	"context"
	"time"

	"gorm.io/gorm"
)

// Repository 数据仓库
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ========== Room 操作 ==========

// CreateRoom 创建房间
func (r *Repository) CreateRoom(ctx context.Context, room *Room) error {
	return r.db.WithContext(ctx).Create(room).Error
}

// GetRoom 获取房间（包含用户列表）
func (r *Repository) GetRoom(ctx context.Context, roomID string) (*Room, error) {
	var room Room
	err := r.db.WithContext(ctx).
		Preload("RoomUsers", "left_at IS NULL"). // 只加载在线用户
		First(&room, "id = ?", roomID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &room, err
}

// GetRoomWithStats 获取房间及统计信息
func (r *Repository) GetRoomWithStats(ctx context.Context, roomID string) (*Room, int64, error) {
	room, err := r.GetRoom(ctx, roomID)
	if err != nil || room == nil {
		return room, 0, err
	}

	var count int64
	err = r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Count(&count).Error

	return room, count, err
}

// ListRooms 列出所有房间
func (r *Repository) ListRooms(ctx context.Context, limit, offset int) ([]*Room, error) {
	var rooms []*Room
	err := r.db.WithContext(ctx).
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&rooms).Error
	return rooms, err
}

// DeleteRoom 删除房间（软删除）
func (r *Repository) DeleteRoom(ctx context.Context, roomID string) error {
	return r.db.WithContext(ctx).Delete(&Room{}, "id = ?", roomID).Error
}

// ========== RoomUser 操作 ==========

// UserJoinRoom 用户加入房间（事务）
func (r *Repository) UserJoinRoom(ctx context.Context, userID, userName, roomID, nodeID string, maxUsers int32) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 检查房间是否存在
		var room Room
		if err := tx.First(&room, "id = ?", roomID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// 房间不存在，创建
				room = Room{
					ID:   roomID,
					Name: roomID,
				}
				if err := tx.Create(&room).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// 2. 检查房间是否已满（使用配置的最大用户数）
		var currentCount int64
		if err := tx.Model(&RoomUser{}).
			Where("room_id = ? AND left_at IS NULL", roomID).
			Count(&currentCount).Error; err != nil {
			return err
		}

		if maxUsers > 0 && currentCount >= int64(maxUsers) {
			return gorm.ErrInvalidData // 房间已满
		}

		// 3. 检查用户是否已在房间中
		var existingUser RoomUser
		err := tx.Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
			First(&existingUser).Error

		if err == nil {
			// 用户已在房间中，更新信息
			return tx.Model(&existingUser).Updates(map[string]interface{}{
				"user_name": userName,
				"node_id":   nodeID,
			}).Error
		}

		// 4. 创建新的用户-房间关系
		roomUser := RoomUser{
			UserID:   userID,
			UserName: userName,
			RoomID:   roomID,
			NodeID:   nodeID,
			JoinedAt: time.Now(),
		}

		return tx.Create(&roomUser).Error
	})
}

// UserLeaveRoom 用户离开房间
func (r *Repository) UserLeaveRoom(ctx context.Context, userID, roomID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
		Updates(map[string]interface{}{
			"left_at":   now,
			"is_online": false,
		}).Error
}

// UpdateUserOnlineStatus 更新用户在线状态
func (r *Repository) UpdateUserOnlineStatus(ctx context.Context, userID, roomID string, isOnline bool) error {
	return r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
		Update("is_online", isOnline).Error
}

// GetRoomUsers 获取房间中的用户列表
func (r *Repository) GetRoomUsers(ctx context.Context, roomID string) ([]*RoomUser, error) {
	var users []*RoomUser
	err := r.db.WithContext(ctx).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Order("joined_at ASC").
		Find(&users).Error
	return users, err
}

// GetUserByID 根据用户ID获取用户信息（查找当前在线的用户）
func (r *Repository) GetUserByID(ctx context.Context, userID string) (*RoomUser, error) {
	var user RoomUser
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND left_at IS NULL", userID).
		Order("joined_at DESC").
		First(&user).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &user, err
}

// GetUserRooms 获取用户加入的房间列表
func (r *Repository) GetUserRooms(ctx context.Context, userID string) ([]*Room, error) {
	var rooms []*Room
	err := r.db.WithContext(ctx).
		Joins("JOIN room_users ON room_users.room_id = rooms.id").
		Where("room_users.user_id = ? AND room_users.left_at IS NULL", userID).
		Find(&rooms).Error
	return rooms, err
}

// GetNode 获取节点
func (r *Repository) GetNode(ctx context.Context, nodeID string) (*ConnectNode, error) {
	var node ConnectNode
	err := r.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &node, err
}

// ListNodes 列出所有在线节点
func (r *Repository) ListNodes(ctx context.Context) ([]*ConnectNode, error) {
	var nodes []*ConnectNode
	err := r.db.WithContext(ctx).
		Where("status = ?", "online").
		Order("current_connections ASC"). // 按连接数排序，用于负载均衡
		Find(&nodes).Error
	return nodes, err
}

// MarkUnhealthyNodes 标记不健康的节点
func (r *Repository) MarkUnhealthyNodes(ctx context.Context, timeout time.Duration) error {
	threshold := time.Now().Add(-timeout)
	return r.db.WithContext(ctx).
		Model(&ConnectNode{}).
		Where("last_heartbeat < ? AND status = ?", threshold, "online").
		Update("status", "unhealthy").Error
}

// ========== 统计查询 ==========

// GetRoomStats 获取房间统计
func (r *Repository) GetRoomStats(ctx context.Context) (totalRooms, totalUsers int64, err error) {
	// 房间总数
	if err = r.db.WithContext(ctx).Model(&Room{}).Count(&totalRooms).Error; err != nil {
		return
	}

	// 在线用户总数
	err = r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("left_at IS NULL").
		Count(&totalUsers).Error

	return
}

// GetRoomUserCount 获取房间用户数
func (r *Repository) GetRoomUserCount(ctx context.Context, roomID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Count(&count).Error
	return count, err
}
