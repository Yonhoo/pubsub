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
	"context"
	"encoding/json"
	"fmt"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"

	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/database"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
)

const (
	// Redis 缓存 TTL
	RoomCacheTTL = 10 * time.Minute
	UserCacheTTL = 1 * time.Hour
	NodeCacheTTL = 5 * time.Minute
)

// ControllerServer 实现 ControllerService
type ControllerServer struct {
	controller.UnimplementedControllerServiceServer

	id string

	// 配置
	config *config.Config

	// 数据库（主要数据源）
	repo *database.Repository

	// Redis 缓存
	redis *redis.Client

	// push-manager
	pushClient *push.CometClient

	// Metrics
	metrics *metrics.MetricsCollector
}

// NewControllerServer 创建 Controller 服务
func NewControllerServer(cfg *config.Config, repo *database.Repository, redisClient *redis.Client,
	pushClient *push.CometClient, metricsCollector *metrics.MetricsCollector) *ControllerServer {
	return &ControllerServer{
		id:         cfg.Server.ID,
		config:     cfg,
		repo:       repo,
		redis:      redisClient,
		pushClient: pushClient,
		metrics:    metricsCollector,
	}
}

// ========== Room Management ==========

// JoinRoom 用户加入房间（使用 MySQL 事务保证一致性）
func (s *ControllerServer) JoinRoom(ctx context.Context, req *controller.JoinRoomRequest) (*controller.JoinRoomResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "Controller.JoinRoom")
	defer span.End()

	tracing.AddSpanAttributes(ctx,
		tracing.AttrUserID.String(req.UserId),
		tracing.AttrUserName.String(req.UserName),
		tracing.AttrRoomID.String(req.RoomId),
		tracing.AttrNodeID.String(req.NodeId),
	)

	log.Printf("👤 [Controller] 用户加入房间: %s -> %s (最大用户数: %d)\n",
		req.UserName, req.RoomId, s.config.Room.DefaultMaxUsers)

	// 🔥 关键：使用 MySQL 事务保证一致性（支持多 Controller 节点）
	tracing.AddSpanEvent(ctx, "db_transaction_join_room")
	err := s.repo.UserJoinRoom(ctx, req.UserId, req.UserName, req.RoomId, req.NodeId, int32(s.config.Room.DefaultMaxUsers))
	if err != nil {
		log.Printf("❌ [Controller] 加入房间失败: %v\n", err)
		tracing.RecordError(ctx, err)

		// 检查是否是房间已满
		if err.Error() == "gorm.ErrInvalidData" {
			return &controller.JoinRoomResponse{
				Success: false,
				Message: "房间已满",
			}, nil
		}

		return &controller.JoinRoomResponse{Success: false, Message: err.Error()}, err
	}

	// 缓存用户到房间的 Hash 中
	roomUsersKey := fmt.Sprintf("room_users:%s", req.RoomId)
	userOnlineData := map[string]interface{}{
		"user_name": req.UserName,
		"node_id":   req.NodeId,
		"room_id":   req.RoomId,
		"timestamp": time.Now().Unix(),
	}
	if data, err := json.Marshal(userOnlineData); err == nil {
		s.redis.HSet(ctx, roomUsersKey, req.UserId, data)
		s.redis.Expire(ctx, roomUsersKey, s.config.Room.CacheTTL)
	}

	// 获取房间当前用户数（用于 metrics）
	userCount, _ := s.redis.HLen(ctx, roomUsersKey).Result()

	tracing.AddSpanAttributes(ctx, tracing.AttrUserCount.Int(int(userCount)))
	log.Printf("✅ [Controller] 用户加入成功: %s, 房间人数: %d\n", req.UserName, userCount)

	// 更新 metrics
	s.metrics.SetRoomUserCount(req.RoomId, userCount)
	s.metrics.RecordAPIRequest(ctx, "JoinRoom", true)

	tracing.SetSpanSuccess(ctx)
	return &controller.JoinRoomResponse{
		Success: true,
		Message: "加入房间成功",
		RoomInfo: &controller.RoomInfo{
			RoomId: req.RoomId,
			Metadata: &controller.RoomMetadata{
				Name:        req.RoomId, // 默认使用 RoomId 作为名称
				Description: "",
			},
			CreatedAt: time.Now().Unix(),
			UpdatedAt: time.Now().Unix(),
		},
	}, nil
}

// LeaveRoom 用户离开房间
func (s *ControllerServer) LeaveRoom(ctx context.Context, req *controller.LeaveRoomRequest) (*controller.LeaveRoomResponse, error) {
	log.Printf("👋 [Controller] 用户离开房间: %s <- %s\n", req.RoomId, req.UserId)

	// 从数据库更新（标记 left_at）
	err := s.repo.UserLeaveRoom(ctx, req.UserId, req.RoomId)
	if err != nil {
		log.Printf("❌ [Controller] 离开房间失败: %v\n", err)
		return &controller.LeaveRoomResponse{Success: false, Message: err.Error()}, err
	}

	// 从房间用户 Hash 中移除该用户
	roomUsersKey := fmt.Sprintf("room_users:%s", req.RoomId)
	s.redis.HDel(ctx, roomUsersKey, req.UserId)

	// 获取房间当前用户数（从 Redis）
	userCount, _ := s.redis.HLen(ctx, roomUsersKey).Result()

	// 更新 metrics
	if userCount == 0 {
		s.metrics.DecrementRooms(ctx, 1)
		s.metrics.RemoveRoom(req.RoomId)
		log.Printf("🗑️  [Controller] 房间已空: %s\n", req.RoomId)
	} else {
		s.metrics.SetRoomUserCount(req.RoomId, userCount)
	}
	s.metrics.RecordAPIRequest(ctx, "LeaveRoom", true)

	log.Printf("✅ [Controller] 用户离开成功: %s\n", req.UserId)
	return &controller.LeaveRoomResponse{Success: true, Message: "离开房间成功"}, nil
}

// GetRoomInfo 获取房间信息（供 Push-Manager 查询）
func (s *ControllerServer) GetRoomInfo(ctx context.Context, req *controller.GetRoomInfoRequest) (*controller.GetRoomInfoResponse, error) {
	// 从 Redis Hash 获取房间用户列表
	roomUsersKey := fmt.Sprintf("room_users:%s", req.RoomId)
	usersData, err := s.redis.HGetAll(ctx, roomUsersKey).Result()

	// 如果 Redis 中有数据，直接从缓存返回
	if err == nil && len(usersData) > 0 {
		log.Printf("🎯 [Controller] 从缓存获取房间: %s, 用户数: %d\n", req.RoomId, len(usersData))

		// 构建用户列表
		userInfos := make([]*controller.UserInfo, 0, len(usersData))
		for userId, userData := range usersData {
			var userInfo map[string]interface{}
			if json.Unmarshal([]byte(userData), &userInfo) == nil {
				userName, _ := userInfo["user_name"].(string)
				nodeId, _ := userInfo["node_id"].(string)
				timestamp, _ := userInfo["timestamp"].(float64)

				userInfos = append(userInfos, &controller.UserInfo{
					UserId:   userId,
					UserName: userName,
					NodeId:   nodeId,
					JoinedAt: int64(timestamp),
				})
			}
		}

		return &controller.GetRoomInfoResponse{
			RoomInfo: &controller.RoomInfo{
				RoomId: req.RoomId,
				Users:  userInfos,
				Metadata: &controller.RoomMetadata{
					Name:        req.RoomId,
					Description: "",
					MaxUsers:    int32(s.config.Room.DefaultMaxUsers),
				},
				CreatedAt: time.Now().Unix(),
				UpdatedAt: time.Now().Unix(),
			},
		}, nil
	}

	// Redis 缓存未命中，从数据库获取
	room, _, err := s.repo.GetRoomWithStats(ctx, req.RoomId)
	if err != nil || room == nil {
		log.Printf("⚠️  [Controller] 房间不存在: %s\n", req.RoomId)
		return &controller.GetRoomInfoResponse{}, nil
	}

	// 获取房间用户列表
	users, err := s.repo.GetRoomUsers(ctx, req.RoomId)
	if err != nil {
		return &controller.GetRoomInfoResponse{}, nil
	}

	// 构建用户列表并同时回填缓存
	userInfos := make([]*controller.UserInfo, 0, len(users))
	for _, u := range users {
		userInfos = append(userInfos, &controller.UserInfo{
			UserId:   u.UserID,
			UserName: u.UserName,
			NodeId:   u.NodeID,
			JoinedAt: u.JoinedAt.Unix(),
		})

		// 回填到 Redis
		userOnlineData := map[string]interface{}{
			"user_name": u.UserName,
			"node_id":   u.NodeID,
			"timestamp": u.JoinedAt.Unix(),
		}
		if data, err := json.Marshal(userOnlineData); err == nil {
			s.redis.HSet(ctx, roomUsersKey, u.UserID, data)
		}
	}

	if len(users) > 0 {
		s.redis.Expire(ctx, roomUsersKey, s.config.Room.CacheTTL)
	}

	log.Printf("📊 [Controller] 房间 %s: %d 人在线（从数据库）\n", req.RoomId, len(users))

	return &controller.GetRoomInfoResponse{
		RoomInfo: &controller.RoomInfo{
			RoomId: room.ID, // room.ID 现在是 string 类型
			Users:  userInfos,
			Metadata: &controller.RoomMetadata{
				Name:        room.Name,
				Description: room.Description,
				MaxUsers:    int32(s.config.Room.DefaultMaxUsers),
			},
			CreatedAt: room.CreatedAt.Unix(),
			UpdatedAt: room.UpdatedAt.Unix(),
		},
	}, nil
}

// GetUserNode 获取用户所在的节点（供 Push-Manager 查询）
func (s *ControllerServer) GetUserNode(ctx context.Context, req *controller.GetUserNodeRequest) (*controller.GetUserNodeResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "Controller.GetUserNode")
	defer span.End()

	tracing.AddSpanAttributes(ctx, tracing.AttrUserID.String(req.UserId))

	log.Printf("🔍 [Controller] 查询用户节点: %s\n", req.UserId)

	// 1. 先从 Redis 查询（快速路径）
	// 查找用户在哪个房间
	var foundRoomID string
	var foundNodeID string
	var foundUserName string

	// 遍历所有房间的缓存（这里简化处理，实际可以维护 user_id -> room_id 的映射）
	// 为了性能，我们直接查询数据库

	// 2. 从数据库查询用户信息
	user, err := s.repo.GetUserByID(ctx, req.UserId)
	if err != nil || user == nil {
		log.Printf("⚠️  [Controller] 用户不存在或不在线: %s\n", req.UserId)
		s.metrics.RecordAPIRequest(ctx, "GetUserNode", false)
		return &controller.GetUserNodeResponse{
			NodeId:      "",
			NodeAddress: "",
			Found:       false,
			RoomId:      "",
		}, nil
	}

	foundNodeID = user.NodeID
	foundRoomID = user.RoomID
	foundUserName = user.UserName

	// 3. 从 ETCD 或缓存获取节点地址（这里简化，直接返回节点ID）
	nodeAddress := foundNodeID // 实际应该从 ETCD 获取节点的实际地址

	log.Printf("✅ [Controller] 找到用户: %s (%s) -> node=%s, room=%s\n",
		foundUserName, req.UserId, foundNodeID, foundRoomID)

	s.metrics.RecordAPIRequest(ctx, "GetUserNode", true)
	tracing.SetSpanSuccess(ctx)

	return &controller.GetUserNodeResponse{
		NodeId:      foundNodeID,
		NodeAddress: nodeAddress,
		Found:       true,
		RoomId:      foundRoomID,
	}, nil
}

// GetRoomStats 获取房间统计
func (s *ControllerServer) GetRoomStats(ctx context.Context, req *controller.GetRoomStatsRequest) (*controller.GetRoomStatsResponse, error) {
	// 从数据库获取统计
	totalRooms, totalUsers, err := s.repo.GetRoomStats(ctx)
	if err != nil {
		log.Printf("❌ [Controller] 获取统计失败: %v\n", err)
		return &controller.GetRoomStatsResponse{}, err
	}

	// 获取所有房间列表
	rooms, err := s.repo.ListRooms(ctx, 100, 0)
	if err != nil {
		return &controller.GetRoomStatsResponse{
			TotalRooms: int32(totalRooms),
			TotalUsers: int32(totalUsers),
		}, nil
	}

	// 构建房间统计
	roomStats := make([]*controller.RoomStats, 0, len(rooms))
	for _, room := range rooms {
		count, _ := s.repo.GetRoomUserCount(ctx, room.ID) // room.ID 现在是 string 类型
		roomStats = append(roomStats, &controller.RoomStats{
			RoomId:    room.ID, // room.ID 现在是 string 类型
			UserCount: int32(count),
			CreatedAt: room.CreatedAt.Unix(),
		})
	}

	return &controller.GetRoomStatsResponse{
		TotalRooms: int32(totalRooms),
		TotalUsers: int32(totalUsers),
		Rooms:      roomStats,
	}, nil
}
