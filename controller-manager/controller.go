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
	"log"
	"sync/atomic"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/database"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
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

	// ETCD 服务发现（用于把 nodeID 解析成 nodeAddress）
	discovery *etcd.ServiceDiscovery

	// Metrics
	metrics *metrics.MetricsCollector

	// 缓存写失败计数（HSet/HDel 失败次数，用于运维告警）
	cacheWriteFailures atomic.Int64
}

// NewControllerServer 创建 Controller 服务
func NewControllerServer(cfg *config.Config, repo *database.Repository, redisClient *redis.Client,
	discovery *etcd.ServiceDiscovery, metricsCollector *metrics.MetricsCollector) *ControllerServer {
	return &ControllerServer{
		id:        cfg.Server.ID,
		config:    cfg,
		repo:      repo,
		redis:     redisClient,
		discovery: discovery,
		metrics:   metricsCollector,
	}
}

// recordAPIRequest 安全调用 metrics（避免 metrics 为 nil 时 panic）。
func (s *ControllerServer) recordAPIRequest(ctx context.Context, method string, success bool) {
	if s.metrics == nil {
		return
	}
	s.metrics.RecordAPIRequest(ctx, method, success)
}

// recordCacheFailure 缓存写失败计数 + 限频日志。
func (s *ControllerServer) recordCacheFailure(op, key string, err error) {
	count := s.cacheWriteFailures.Add(1)
	if count%100 == 1 {
		log.Printf("[Controller] Redis %s 失败 key=%s err=%v totalFailures=%d", op, key, err, count)
	}
}

// roomUsersKey 计算 Redis hash key，避免 fmt.Sprintf 反射。
func roomUsersKey(roomID string) string {
	return "room_users:" + roomID
}

// userNodeKey 用户 -> {nodeID, roomID} 缓存的 key。
func userNodeKey(userID string) string {
	return "user_node:" + userID
}

// RunRoomCleanup 周期清理空闲房间。阻塞直到 ctx 取消。
// 调用方应放在 goroutine 里，并在进程退出前 cancel ctx。
func (s *ControllerServer) RunRoomCleanup(ctx context.Context) {
	interval := s.config.Room.CleanupInterval
	idleFor := s.config.Room.EmptyTTL
	if interval <= 0 || idleFor <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOnce(ctx, idleFor)
		}
	}
}

// cleanupOnce 跑一轮房间清理：DB 删空闲房间 + 失效 Redis 缓存 + metrics。
func (s *ControllerServer) cleanupOnce(ctx context.Context, idleFor time.Duration) {
	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	deleted, err := s.repo.CleanupEmptyRooms(scanCtx, idleFor, 200)
	if err != nil {
		log.Printf("CleanupEmptyRooms 失败: %v", err)
		return
	}
	if len(deleted) == 0 {
		return
	}

	// 失效 Redis 缓存。room_users:<roomID> 是房间用户 hash；user_node 反向映射会随
	// 各自 TTL 自然过期，无需在这里逐个清。
	if s.redis != nil {
		keys := make([]string, 0, len(deleted))
		for _, roomID := range deleted {
			keys = append(keys, roomUsersKey(roomID))
		}
		if err := s.redis.Del(ctx, keys...).Err(); err != nil {
			s.recordCacheFailure("cleanup_del", "room_users:*", err)
		}
	}

	// metrics：从仪表盘移除空房间
	if s.metrics != nil {
		for _, roomID := range deleted {
			s.metrics.RemoveRoom(roomID)
		}
		s.metrics.DecrementRooms(ctx, int64(len(deleted)))
	}
	log.Printf("已清理空闲房间 %d 个", len(deleted))
}

// userNodeCacheTTL GetUserNode 缓存的 TTL。
const userNodeCacheTTL = 5 * time.Minute

// ========== Room Management ==========

// JoinRoom ???????Redis-first ?????? 20k ??? Join ? MySQL ??????
func (s *ControllerServer) JoinRoom(ctx context.Context, req *controller.JoinRoomRequest) (*controller.JoinRoomResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "Controller.JoinRoom")
	defer span.End()

	tracing.AddSpanAttributes(ctx,
		tracing.AttrUserID.String(req.UserId),
		tracing.AttrUserName.String(req.UserName),
		tracing.AttrRoomID.String(req.RoomId),
		tracing.AttrNodeID.String(req.NodeId),
	)

	if req.RoomId == "" {
		return nil, status.Error(codes.InvalidArgument, "room_id is empty")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is empty")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is empty")
	}
	if req.UserName == "" {
		req.UserName = req.UserId
	}
	if s.redis == nil {
		return nil, status.Error(codes.Unavailable, "redis unavailable")
	}

	key := roomUsersKey(req.RoomId)
	userNode := userNodeKey(req.UserId)
	maxUsers := int64(s.config.Room.DefaultMaxUsers)
	if maxUsers < 0 {
		maxUsers = 0
	}
	userOnlineData := map[string]any{
		"user_name": req.UserName,
		"node_id":   req.NodeId,
		"room_id":   req.RoomId,
		"timestamp": time.Now().Unix(),
	}
	data, mErr := json.Marshal(userOnlineData)
	if mErr != nil {
		tracing.RecordError(ctx, mErr)
		s.recordAPIRequest(ctx, "JoinRoom", false)
		return nil, status.Error(codes.Internal, mErr.Error())
	}

	joinLua := redis.NewScript(`
local roomKey = KEYS[1]
local userNodeKey = KEYS[2]
local userID = ARGV[1]
local payload = ARGV[2]
local roomTTL = tonumber(ARGV[3])
local userNodeTTL = tonumber(ARGV[4])
local userNodeValue = ARGV[5]
local maxUsers = tonumber(ARGV[6])
local exists = redis.call('HEXISTS', roomKey, userID)
local count = redis.call('HLEN', roomKey)
if exists == 0 and maxUsers > 0 and count >= maxUsers then
  return {0, count}
end
redis.call('HSET', roomKey, userID, payload)
if roomTTL > 0 then
  redis.call('EXPIRE', roomKey, roomTTL)
end
if userNodeTTL > 0 then
  redis.call('SET', userNodeKey, userNodeValue, 'EX', userNodeTTL)
else
  redis.call('SET', userNodeKey, userNodeValue)
end
count = redis.call('HLEN', roomKey)
return {1, count}
`)

	res, err := joinLua.Run(ctx, s.redis, []string{key, userNode}, req.UserId, string(data), int(s.config.Room.CacheTTL/time.Second), int(userNodeCacheTTL/time.Second), req.NodeId+":"+req.RoomId, maxUsers).Result()
	if err != nil {
		tracing.RecordError(ctx, err)
		s.recordCacheFailure("join_eval", key, err)
		s.recordAPIRequest(ctx, "JoinRoom", false)
		return nil, status.Error(codes.Internal, err.Error())
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 2 {
		s.recordAPIRequest(ctx, "JoinRoom", false)
		return nil, status.Error(codes.Internal, "unexpected redis join result")
	}

	joined, _ := vals[0].(int64)
	userCount, _ := vals[1].(int64)
	if joined == 0 {
		s.recordAPIRequest(ctx, "JoinRoom", false)
		return &controller.JoinRoomResponse{Success: false, Message: "????"}, nil
	}

	tracing.AddSpanAttributes(ctx, tracing.AttrUserCount.Int(int(userCount)))
	if s.metrics != nil {
		s.metrics.SetRoomUserCount(req.RoomId, userCount)
	}
	s.recordAPIRequest(ctx, "JoinRoom", true)
	tracing.SetSpanSuccess(ctx)

	createdAt := time.Now().Unix()
	return &controller.JoinRoomResponse{
		Success: true,
		Message: "??????",
		RoomInfo: &controller.RoomInfo{
			RoomId: req.RoomId,
			Metadata: &controller.RoomMetadata{
				Name:        req.RoomId,
				Description: "",
				MaxUsers:    int32(maxUsers),
			},
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}, nil
}

// LeaveRoom 用户离开房间
func (s *ControllerServer) LeaveRoom(ctx context.Context, req *controller.LeaveRoomRequest) (*controller.LeaveRoomResponse, error) {
	// 从数据库直接删除记录
	err := s.repo.UserLeaveRoom(ctx, req.UserId, req.RoomId)
	if err != nil {
		log.Printf("[Controller] 离开房间失败: %v", err)
		s.recordAPIRequest(ctx, "LeaveRoom", false)
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Redis pipeline 合并 HDel + HLen + DEL(user_node)，减少 RTT
	key := roomUsersKey(req.RoomId)
	var userCount int64
	if s.redis != nil {
		pipe := s.redis.Pipeline()
		pipe.HDel(ctx, key, req.UserId)
		pipe.Del(ctx, userNodeKey(req.UserId))
		hlen := pipe.HLen(ctx, key)
		if _, pErr := pipe.Exec(ctx); pErr != nil {
			s.recordCacheFailure("pipeline_leave", key, pErr)
		} else {
			userCount = hlen.Val()
		}
	}
	if s.redis == nil {
		userCount, _ = s.repo.GetRoomUserCount(ctx, req.RoomId)
	}

	// 更新 metrics
	if s.metrics != nil {
		if userCount == 0 {
			s.metrics.DecrementRooms(ctx, 1)
			s.metrics.RemoveRoom(req.RoomId)
		} else {
			s.metrics.SetRoomUserCount(req.RoomId, userCount)
		}
	}
	s.recordAPIRequest(ctx, "LeaveRoom", true)
	return &controller.LeaveRoomResponse{Success: true, Message: "离开房间成功"}, nil
}

// GetRoomInfo 获取房间信息（供 Push-Manager 查询）
func (s *ControllerServer) GetRoomInfo(ctx context.Context, req *controller.GetRoomInfoRequest) (*controller.GetRoomInfoResponse, error) {
	key := roomUsersKey(req.RoomId)

	// Redis 命中：直接返回，并续 TTL（sliding window，避免缓存击穿）
	if s.redis != nil {
		usersData, err := s.redis.HGetAll(ctx, key).Result()
		if err == nil && len(usersData) > 0 {
			userInfos := make([]*controller.UserInfo, 0, len(usersData))
			for userId, userData := range usersData {
				var userInfo map[string]any
				if jerr := json.Unmarshal([]byte(userData), &userInfo); jerr != nil {
					// 数据损坏：log + 计数，但跳过该用户继续
					s.recordCacheFailure("unmarshal", key, jerr)
					continue
				}
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
			// R-2: 命中时续 TTL，避免热点 key 在 TTL 边界并发回源（缓存击穿）
			s.redis.Expire(ctx, key, s.config.Room.CacheTTL)

			s.recordAPIRequest(ctx, "GetRoomInfo", true)
			return &controller.GetRoomInfoResponse{
				RoomInfo: s.buildRoomInfoFromCache(req.RoomId, userInfos),
			}, nil
		}
	}

	// Redis 未命中或不可用：从数据库查询
	room, _, err := s.repo.GetRoomWithStats(ctx, req.RoomId)
	if err != nil {
		// E-1: DB 错误必须返回 grpc 错误，不能伪装成"房间不存在"
		log.Printf("[Controller] 查询房间失败: roomID=%s err=%v", req.RoomId, err)
		s.recordAPIRequest(ctx, "GetRoomInfo", false)
		return nil, status.Error(codes.Unavailable, "database error")
	}
	if room == nil {
		s.recordAPIRequest(ctx, "GetRoomInfo", true)
		return &controller.GetRoomInfoResponse{}, nil
	}

	// 获取房间用户列表
	users, err := s.repo.GetRoomUsers(ctx, req.RoomId)
	if err != nil {
		log.Printf("[Controller] 查询房间用户失败: roomID=%s err=%v", req.RoomId, err)
		s.recordAPIRequest(ctx, "GetRoomInfo", false)
		return nil, status.Error(codes.Unavailable, "database error")
	}

	// 构建用户列表
	userInfos := make([]*controller.UserInfo, 0, len(users))
	for _, u := range users {
		userInfos = append(userInfos, &controller.UserInfo{
			UserId:   u.UserID,
			UserName: u.UserName,
			NodeId:   u.NodeID,
			JoinedAt: u.JoinedAt.Unix(),
		})
	}

	// 回填缓存（pipeline 合并 N 次 HSet 到 1 次 RTT）
	if s.redis != nil && len(users) > 0 {
		fields := make([]any, 0, len(users)*2)
		for _, u := range users {
			cacheData := map[string]any{
				"user_name": u.UserName,
				"node_id":   u.NodeID,
				"timestamp": u.JoinedAt.Unix(),
			}
			data, mErr := json.Marshal(cacheData)
			if mErr != nil {
				s.recordCacheFailure("marshal", key, mErr)
				continue
			}
			fields = append(fields, u.UserID, data)
		}
		if len(fields) > 0 {
			pipe := s.redis.Pipeline()
			pipe.HSet(ctx, key, fields...)
			pipe.Expire(ctx, key, s.config.Room.CacheTTL)
			if _, pErr := pipe.Exec(ctx); pErr != nil {
				s.recordCacheFailure("pipeline_backfill", key, pErr)
			}
		}
	}

	s.recordAPIRequest(ctx, "GetRoomInfo", true)
	// B-2/B-4: 返回 DB 中真实的 room.Name / MaxUsers
	maxUsers := int32(room.MaxUsers)
	if maxUsers == 0 {
		maxUsers = int32(s.config.Room.DefaultMaxUsers)
	}
	return &controller.GetRoomInfoResponse{
		RoomInfo: &controller.RoomInfo{
			RoomId: room.ID,
			Users:  userInfos,
			Metadata: &controller.RoomMetadata{
				Name:        room.Name,
				Description: room.Description,
				MaxUsers:    maxUsers,
			},
			CreatedAt: room.CreatedAt.Unix(),
			UpdatedAt: room.UpdatedAt.Unix(),
		},
	}, nil
}

// buildRoomInfoFromCache 从 Redis 命中路径构建 RoomInfo。
// 注：Redis 中没有 room metadata（Name/MaxUsers/Desc），调用方拿到的是降级数据。
// 这是已知 trade-off：避免在缓存命中时还要再读一次 DB 拿 metadata。
func (s *ControllerServer) buildRoomInfoFromCache(roomID string, userInfos []*controller.UserInfo) *controller.RoomInfo {
	now := time.Now().Unix()
	return &controller.RoomInfo{
		RoomId: roomID,
		Users:  userInfos,
		Metadata: &controller.RoomMetadata{
			Name:     roomID,
			MaxUsers: int32(s.config.Room.DefaultMaxUsers),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// GetUserNode 获取用户所在的节点（供 Push-Manager 查询）
func (s *ControllerServer) GetUserNode(ctx context.Context, req *controller.GetUserNodeRequest) (*controller.GetUserNodeResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "Controller.GetUserNode")
	defer span.End()

	tracing.AddSpanAttributes(ctx, tracing.AttrUserID.String(req.UserId))

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is empty")
	}

	// 1. Redis 快速路径：user_node:{userID} -> "{nodeID}:{roomID}"
	if s.redis != nil {
		val, err := s.redis.Get(ctx, userNodeKey(req.UserId)).Result()
		if err == nil && val != "" {
			// 解析 "nodeID:roomID"
			for i := 0; i < len(val); i++ {
				if val[i] == ':' {
					nodeID := val[:i]
					roomID := val[i+1:]
					addr := s.resolveNodeAddress(nodeID)
					s.recordAPIRequest(ctx, "GetUserNode", true)
					tracing.SetSpanSuccess(ctx)
					return &controller.GetUserNodeResponse{
						NodeId:      nodeID,
						NodeAddress: addr,
						Found:       true,
						RoomId:      roomID,
					}, nil
				}
			}
		}
	}

	// 2. 慢路径：从 DB 查询
	user, err := s.repo.GetUserByID(ctx, req.UserId)
	if err != nil {
		log.Printf("[Controller] 查询用户失败: userID=%s err=%v", req.UserId, err)
		s.recordAPIRequest(ctx, "GetUserNode", false)
		return nil, status.Error(codes.Unavailable, "database error")
	}
	if user == nil {
		s.recordAPIRequest(ctx, "GetUserNode", false)
		return &controller.GetUserNodeResponse{Found: false}, nil
	}

	// 回填缓存
	if s.redis != nil {
		if err := s.redis.Set(ctx, userNodeKey(req.UserId), user.NodeID+":"+user.RoomID, userNodeCacheTTL).Err(); err != nil {
			s.recordCacheFailure("set_user_node", userNodeKey(req.UserId), err)
		}
	}

	addr := s.resolveNodeAddress(user.NodeID)
	s.recordAPIRequest(ctx, "GetUserNode", true)
	tracing.SetSpanSuccess(ctx)
	return &controller.GetUserNodeResponse{
		NodeId:      user.NodeID,
		NodeAddress: addr,
		Found:       true,
		RoomId:      user.RoomID,
	}, nil
}

// resolveNodeAddress 通过 ETCD 把 nodeID 解析成 ip:port。
// 如果 discovery 不可用或找不到，降级返回 nodeID 本身（保持向后兼容）。
func (s *ControllerServer) resolveNodeAddress(nodeID string) string {
	if s.discovery == nil || nodeID == "" {
		return nodeID
	}
	endpoints, err := s.discovery.GetEndpoints()
	if err != nil {
		return nodeID
	}
	for _, ep := range endpoints {
		if ep == nodeID {
			return ep
		}
	}
	return nodeID
}

// GetRoomStats 获取房间统计
func (s *ControllerServer) GetRoomStats(ctx context.Context, req *controller.GetRoomStatsRequest) (*controller.GetRoomStatsResponse, error) {
	// 从数据库获取统计
	totalRooms, totalUsers, err := s.repo.GetRoomStats(ctx)
	if err != nil {
		log.Printf("[Controller] 获取统计失败: %v", err)
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	// 获取所有房间列表
	rooms, err := s.repo.ListRooms(ctx, 100, 0)
	if err != nil {
		return &controller.GetRoomStatsResponse{
			TotalRooms: int32(totalRooms),
			TotalUsers: int32(totalUsers),
		}, nil
	}

	// P-2: 一次性查询所有房间的用户数，避免 N+1 查询
	roomCounts, err := s.repo.GetRoomUserCounts(ctx)
	if err != nil {
		log.Printf("[Controller] 获取房间用户数失败: %v", err)
		roomCounts = map[string]int64{}
	}

	roomStats := make([]*controller.RoomStats, 0, len(rooms))
	for _, room := range rooms {
		count := roomCounts[room.ID]
		roomStats = append(roomStats, &controller.RoomStats{
			RoomId:    room.ID,
			UserCount: int32(count),
			CreatedAt: room.CreatedAt.Unix(),
		})
	}

	s.recordAPIRequest(ctx, "GetRoomStats", true)
	return &controller.GetRoomStatsResponse{
		TotalRooms: int32(totalRooms),
		TotalUsers: int32(totalUsers),
		Rooms:      roomStats,
	}, nil
}

// GetCacheWriteFailures 暴露缓存写失败次数（运维 / 测试用）。
func (s *ControllerServer) GetCacheWriteFailures() int64 {
	return s.cacheWriteFailures.Load()
}
