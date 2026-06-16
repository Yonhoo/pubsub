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
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrRoomFull 房间已满。Controller 层用 errors.Is(err, ErrRoomFull) 判断。
var ErrRoomFull = errors.New("room is full")

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

// CleanupEmptyRooms 清理空闲超过 idleFor 的房间。
// 只删满足以下条件的 room：
//  1. updated_at < now - idleFor
//  2. 没有任何在线用户（room_users.left_at IS NULL）
//
// 删除时整个房间走单事务并行锁，避免与 Join/Leave 竞争。
// 返回被清理的 roomID 列表，供调用方做 Redis cache 失效与 metrics 上报。
func (r *Repository) CleanupEmptyRooms(ctx context.Context, idleFor time.Duration, batchSize int) ([]string, error) {
	if idleFor <= 0 {
		return nil, nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	idleBefore := time.Now().Add(-idleFor)

	// 1. 先选出候选 roomID（不持锁，仅做候选筛选）
	var candidates []string
	if err := r.db.WithContext(ctx).
		Model(&Room{}).
		Where("updated_at < ?", idleBefore).
		Where("NOT EXISTS (SELECT 1 FROM room_users WHERE room_users.room_id = rooms.id AND room_users.left_at IS NULL)").
		Limit(batchSize).
		Pluck("id", &candidates).Error; err != nil {
		return nil, fmt.Errorf("查询空闲房间失败: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// 2. 逐房间在事务内做"加锁 + 二次校验 + 删除"，不批量删，避免在大事务中锁太多行
	deleted := make([]string, 0, len(candidates))
	for _, roomID := range candidates {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var room Room
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&room, "id = ?", roomID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil // 已被并发清理
				}
				return err
			}

			// 二次校验：拿到行锁后房间必须仍然空闲
			if room.UpdatedAt.After(idleBefore) {
				return nil // 已重新活跃
			}
			var onlineCount int64
			if err := tx.Model(&RoomUser{}).
				Where("room_id = ? AND left_at IS NULL", roomID).
				Count(&onlineCount).Error; err != nil {
				return err
			}
			if onlineCount > 0 {
				return nil // 期间又有人 Join
			}

			// 先清历史 room_users 记录（含 left_at NOT NULL 的离线记录），再删 room
			if err := tx.Where("room_id = ?", roomID).Delete(&RoomUser{}).Error; err != nil {
				return fmt.Errorf("删除 room_users 失败: %w", err)
			}
			if err := tx.Delete(&Room{}, "id = ?", roomID).Error; err != nil {
				return fmt.Errorf("删除 room 失败: %w", err)
			}
			return nil
		})
		if err != nil {
			log.Printf("CleanupEmptyRooms 房间 %s 失败: %v", roomID, err)
			continue
		}
		// 事务成功后再确认是否真的删除了（room 仍存在表示二次校验失败）
		var still int64
		if err := r.db.WithContext(ctx).Model(&Room{}).
			Where("id = ?", roomID).Count(&still).Error; err == nil && still == 0 {
			deleted = append(deleted, roomID)
		}
	}
	return deleted, nil
}

// ========== RoomUser 操作 ==========

// UserJoinRoom 用户加入房间（事务）
func (r *Repository) UserJoinRoom(ctx context.Context, userID, userName, roomID, nodeID string, maxUsers int32) error {
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
		userName = userID
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 幂等创建房间，然后锁住 room 行作为同房间 Join/Leave 的串行点。
		room := Room{
			ID:       roomID,
			Name:     roomID,
			MaxUsers: int(maxUsers),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&room).Error; err != nil {
			return fmt.Errorf("创建房间失败: %w", err)
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&room, "id = ?", roomID).Error; err != nil {
			return fmt.Errorf("查询房间失败: %w", err)
		}

		// 2. 检查用户是否已在房间中。已在线用户重连不增加容量占用。
		var existingUser RoomUser
		err := tx.Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
			First(&existingUser).Error

		if err == nil {
			// 用户已在房间中，更新信息（重新连接，设置为在线）
			updateSQL := `UPDATE room_users
			              SET user_name = ?, node_id = ?, is_online = ?, left_at = NULL
			              WHERE id = ?`

			result := tx.Exec(updateSQL, userName, nodeID, true, existingUser.ID)
			if result.Error != nil {
				return fmt.Errorf("更新用户信息失败 (userID=%s, roomID=%s): %w", userID, roomID, result.Error)
			}
			return nil
		}

		if err != nil && err != gorm.ErrRecordNotFound {
			return fmt.Errorf("查询用户失败: %w", err)
		}

		// 3. 检查房间是否已满（优先使用数据库中房间的 max_users，如果为 0 则使用传入的配置）
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

		if effectiveMaxUsers > 0 && currentCount >= int64(effectiveMaxUsers) {
			// 返回 typed sentinel error，Controller 层通过 errors.Is(err, ErrRoomFull) 判断。
			return fmt.Errorf("%w (current=%d, max=%d)", ErrRoomFull, currentCount, effectiveMaxUsers)
		}

		// 4. 创建新的用户-房间关系
		// 使用 Omit("joined_at") 让 GORM 不插入 joined_at 字段，让数据库使用默认值 CURRENT_TIMESTAMP
		roomUser := RoomUser{
			UserID:   userID,
			UserName: userName,
			RoomID:   roomID,
			NodeID:   nodeID,
			IsOnline: true,
			LeftAt:   nil,
			JoinedAt: nil,
		}

		if err := tx.Omit("joined_at").Create(&roomUser).Error; err != nil {
			return fmt.Errorf("创建用户房间关系失败 (userID=%s, roomID=%s, nodeID=%s): %w", userID, roomID, nodeID, err)
		}

		// 戳 updated_at,后台清理用它判断房间活跃度
		if err := tx.Model(&Room{}).Where("id = ?", roomID).
			Update("updated_at", time.Now()).Error; err != nil {
			log.Printf("更新 room.updated_at 失败: %v", err)
		}
		return nil
	})
}

// UserLeaveRoom 用户离开房间（直接删除记录）
func (r *Repository) UserLeaveRoom(ctx context.Context, userID, roomID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room Room
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&room, "id = ?", roomID).Error
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		if err != nil {
			return fmt.Errorf("查询房间失败: %w", err)
		}

		result := tx.
			Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
			Delete(&RoomUser{})

		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			// 记录可能已经被删除，不视为错误
			return nil
		}

		// 戳 updated_at,后台清理用它判断空闲起点
		if err := tx.Model(&Room{}).Where("id = ?", roomID).
			Update("updated_at", time.Now()).Error; err != nil {
			log.Printf("更新 room.updated_at 失败: %v", err)
		}
		return nil
	})
}

// UpdateUserOnlineStatus 更新用户在线状态
func (r *Repository) UpdateUserOnlineStatus(ctx context.Context, userID, roomID string, isOnline bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room Room
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&room, "id = ?", roomID).Error
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		if err != nil {
			return fmt.Errorf("查询房间失败: %w", err)
		}

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

		return tx.
			Model(&RoomUser{}).
			Where("user_id = ? AND room_id = ? AND left_at IS NULL", userID, roomID).
			Updates(updates).Error
	})
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

// GetRoomUserCounts 一次性获取所有房间的用户数（避免 N+1 查询）。
// 返回 map[roomID]count，只包含至少有一个在线用户的房间。
func (r *Repository) GetRoomUserCounts(ctx context.Context) (map[string]int64, error) {
	type rowResult struct {
		RoomID string `gorm:"column:room_id"`
		Count  int64  `gorm:"column:count"`
	}
	var rows []rowResult
	err := r.db.WithContext(ctx).
		Model(&RoomUser{}).
		Select("room_id, COUNT(*) AS count").
		Where("left_at IS NULL").
		Group("room_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.RoomID] = row.Count
	}
	return result, nil
}
