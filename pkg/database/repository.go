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
	"fmt"
	"log"
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
	// 在事务开始前就记录参数，确保能看到所有传入的值
	log.Printf("🔍 [Repository] UserJoinRoom 调用: userID=%q (len=%d), userName=%q (len=%d), roomID=%q (len=%d), nodeID=%q (len=%d), maxUsers=%d",
		userID, len(userID), userName, len(userName), roomID, len(roomID), nodeID, len(nodeID), maxUsers)

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 检查房间是否存在
		var room Room
		if err := tx.First(&room, "id = ?", roomID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// 房间不存在，创建（使用传入的 maxUsers 作为默认值）
				room = Room{
					ID:       roomID,
					Name:     roomID,
					MaxUsers: int(maxUsers),
				}
				if err := tx.Create(&room).Error; err != nil {
					return fmt.Errorf("创建房间失败: %w", err)
				}
			} else {
				return fmt.Errorf("查询房间失败: %w", err)
			}
		}

		// 2. 检查房间是否已满（优先使用数据库中房间的 max_users，如果为 0 则使用传入的配置）
		var currentCount int64
		if err := tx.Model(&RoomUser{}).
			Where("room_id = ? AND left_at IS NULL", roomID).
			Count(&currentCount).Error; err != nil {
			return fmt.Errorf("查询房间用户数失败: %w", err)
		}

		// 使用数据库中房间的 max_users（如果已设置），否则使用传入的配置
		effectiveMaxUsers := room.MaxUsers
		if effectiveMaxUsers == 0 {
			effectiveMaxUsers = int(maxUsers)
		}

		log.Printf("🔍 [Repository] 房间容量检查: roomID=%q, currentCount=%d, effectiveMaxUsers=%d (db=%d, config=%d)",
			roomID, currentCount, effectiveMaxUsers, room.MaxUsers, maxUsers)

		if effectiveMaxUsers > 0 && currentCount >= int64(effectiveMaxUsers) {
			// 返回房间已满错误，使用自定义错误消息，方便 controller 识别
			// 不直接返回 gorm.ErrInvalidData，而是返回包含明确消息的错误
			return fmt.Errorf("房间已满 (当前用户数: %d, 最大用户数: %d)", currentCount, effectiveMaxUsers)
		}

		// 3. 检查用户是否已在房间中
		var existingUser RoomUser
		log.Printf("🔍 [Repository] 准备查询现有用户: userID=%q, roomID=%q", userID, roomID)

		// 先尝试查询，如果出错则记录详细错误
		err := tx.Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
			First(&existingUser).Error

		if err == nil {
			// 用户已在房间中，更新信息（重新连接，设置为在线）
			log.Printf("✅ [Repository] 找到现有用户: ID=%d, UserID=%q, UserName=%q, NodeID=%q",
				existingUser.ID, existingUser.UserID, existingUser.UserName, existingUser.NodeID)

			// 使用原生 SQL 更新，避免 GORM 的类型转换问题
			updateSQL := `UPDATE room_users 
			              SET user_name = ?, node_id = ?, is_online = ?, left_at = NULL 
			              WHERE id = ?`

			log.Printf("🔍 [Repository] 准备更新用户: userID=%s, userName=%s, roomID=%s, nodeID=%s, existingID=%d",
				userID, userName, roomID, nodeID, existingUser.ID)

			result := tx.Exec(updateSQL, userName, nodeID, true, existingUser.ID)
			if result.Error != nil {
				log.Printf("❌ [Repository] UPDATE 失败: %v, SQL: %s, 参数: [%s, %s, true, %d]",
					result.Error, updateSQL, userName, nodeID, existingUser.ID)
				return fmt.Errorf("更新用户信息失败 (userID=%s, roomID=%s): %w", userID, roomID, result.Error)
			}

			log.Printf("✅ [Repository] UPDATE 成功: rowsAffected=%d", result.RowsAffected)
			return nil
		}

		if err != nil && err != gorm.ErrRecordNotFound {
			log.Printf("❌ [Repository] 查询用户失败: %v, 错误类型: %T", err, err)
			log.Printf("❌ [Repository] 错误详情: %+v", err)
			return fmt.Errorf("查询用户失败: %w", err)
		}

		if err == gorm.ErrRecordNotFound {
			log.Printf("✅ [Repository] 用户不存在，将创建新记录")
		}

		// 4. 创建新的用户-房间关系
		// 验证参数
		if userID == "" {
			return fmt.Errorf("userID 不能为空")
		}
		if roomID == "" {
			return fmt.Errorf("roomID 不能为空")
		}
		if nodeID == "" {
			return fmt.Errorf("nodeID 不能为空")
		}
		if userName == "" {
			userName = userID // 如果 userName 为空，使用 userID
		}

		// 记录插入参数用于调试
		log.Printf("🔍 [Repository] 准备插入用户: userID=%q (len=%d), userName=%q (len=%d), roomID=%q (len=%d), nodeID=%q (len=%d)",
			userID, len(userID), userName, len(userName), roomID, len(roomID), nodeID, len(nodeID))

		// 使用 GORM 的 Create 方法，但只设置需要的字段，让数据库处理默认值
		roomUser := RoomUser{
			UserID:   userID,
			UserName: userName,
			RoomID:   roomID,
			NodeID:   nodeID,
			IsOnline: true,
			LeftAt:   nil, // NULL 表示在线
			JoinedAt: nil, // NULL，让数据库使用默认值 CURRENT_TIMESTAMP
		}

		// 使用 Omit("joined_at") 让 GORM 不插入 joined_at 字段，让数据库使用默认值 CURRENT_TIMESTAMP
		// 注意：如果显式设置 JoinedAt 为 nil，GORM 会插入 NULL，而不是使用数据库默认值
		// 记录创建前的完整对象状态
		log.Printf("🔍 [Repository] 准备创建 RoomUser: UserID=%q, UserName=%q, RoomID=%q, NodeID=%q, IsOnline=%v, LeftAt=%v, JoinedAt=%v",
			roomUser.UserID, roomUser.UserName, roomUser.RoomID, roomUser.NodeID, roomUser.IsOnline, roomUser.LeftAt, roomUser.JoinedAt)

		if err := tx.Omit("joined_at").Create(&roomUser).Error; err != nil {
			// 详细记录错误信息
			log.Printf("❌ [Repository] INSERT 失败: %v", err)
			log.Printf("❌ [Repository] 错误类型: %T", err)
			log.Printf("❌ [Repository] 错误详情: %+v", err)
			log.Printf("❌ [Repository] 参数: userID=%q, userName=%q, roomID=%q, nodeID=%q",
				userID, userName, roomID, nodeID)
			log.Printf("❌ [Repository] RoomUser 对象: %+v", roomUser)
			return fmt.Errorf("创建用户房间关系失败 (userID=%s, roomID=%s, nodeID=%s): %w", userID, roomID, nodeID, err)
		}

		log.Printf("✅ [Repository] INSERT 成功: ID=%d, UserID=%q, UserName=%q, RoomID=%q, NodeID=%q",
			roomUser.ID, roomUser.UserID, roomUser.UserName, roomUser.RoomID, roomUser.NodeID)
		return nil
	})
}

// UserLeaveRoom 用户离开房间（直接删除记录）
func (r *Repository) UserLeaveRoom(ctx context.Context, userID, roomID string) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND room_id = ? ", userID, roomID).
		Delete(&RoomUser{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		log.Printf("⚠️  [Repository] 未找到要删除的用户记录: userID=%q, roomID=%q", userID, roomID)
		// 不返回错误，因为可能记录已经被删除或不存在
		return nil
	}

	log.Printf("✅ [Repository] 已删除用户记录: userID=%q, roomID=%q, rowsAffected=%d", userID, roomID, result.RowsAffected)
	return nil
}

// UpdateUserOnlineStatus 更新用户在线状态
func (r *Repository) UpdateUserOnlineStatus(ctx context.Context, userID, roomID string, isOnline bool) error {
	updates := map[string]interface{}{
		"is_online": isOnline,
	}

	// 同时更新 left_at：在线时设为 NULL，离线时设为当前时间
	if isOnline {
		updates["left_at"] = nil
	} else {
		now := time.Now()
		updates["left_at"] = now
	}

	return r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
		Updates(updates).Error
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
