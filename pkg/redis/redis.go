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

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/examples/pubsub/pkg/types"
)

const (
	// Redis key 前缀
	RoomPrefix = "room:"
	UserPrefix = "user:"
	NodePrefix = "node:"

	// 过期时间
	RoomTTL = 24 * time.Hour
	UserTTL = 1 * time.Hour
	NodeTTL = 1 * time.Hour
)

// RoomStore Redis Room 存储
type RoomStore struct {
	client *redis.Client
}

// NewRoomStore 创建 Room 存储
func NewRoomStore(client *redis.Client) *RoomStore {
	return &RoomStore{client: client}
}

// SaveRoom 保存房间
func (s *RoomStore) SaveRoom(ctx context.Context, room *types.Room) error {
	key := fmt.Sprintf("%s%s", RoomPrefix, room.ID)

	data, err := json.Marshal(room)
	if err != nil {
		return fmt.Errorf("failed to marshal room: %w", err)
	}

	return s.client.Set(ctx, key, data, RoomTTL).Err()
}

// GetRoom 获取房间
func (s *RoomStore) GetRoom(ctx context.Context, roomID string) (*types.Room, error) {
	key := fmt.Sprintf("%s%s", RoomPrefix, roomID)

	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get room: %w", err)
	}

	var room types.Room
	if err := json.Unmarshal([]byte(data), &room); err != nil {
		return nil, fmt.Errorf("failed to unmarshal room: %w", err)
	}

	return &room, nil
}

// DeleteRoom 删除房间
func (s *RoomStore) DeleteRoom(ctx context.Context, roomID string) error {
	key := fmt.Sprintf("%s%s", RoomPrefix, roomID)
	return s.client.Del(ctx, key).Err()
}

// GetAllRooms 获取所有房间
func (s *RoomStore) GetAllRooms(ctx context.Context) ([]*types.Room, error) {
	pattern := fmt.Sprintf("%s*", RoomPrefix)

	var cursor uint64
	var rooms []*types.Room

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			data, err := s.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var room types.Room
			if err := json.Unmarshal([]byte(data), &room); err != nil {
				continue
			}
			rooms = append(rooms, &room)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return rooms, nil
}

// UserStore Redis User 存储
type UserStore struct {
	client *redis.Client
}

// NewUserStore 创建 User 存储
func NewUserStore(client *redis.Client) *UserStore {
	return &UserStore{client: client}
}

// SaveUser 保存用户
func (s *UserStore) SaveUser(ctx context.Context, user *types.User) error {
	key := fmt.Sprintf("%s%s", UserPrefix, user.ID)

	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	return s.client.Set(ctx, key, data, UserTTL).Err()
}

// GetUser 获取用户
func (s *UserStore) GetUser(ctx context.Context, userID string) (*types.User, error) {
	key := fmt.Sprintf("%s%s", UserPrefix, userID)

	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	var user types.User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	return &user, nil
}

// DeleteUser 删除用户
func (s *UserStore) DeleteUser(ctx context.Context, userID string) error {
	key := fmt.Sprintf("%s%s", UserPrefix, userID)
	return s.client.Del(ctx, key).Err()
}

// NodeStore Redis Node 存储
type NodeStore struct {
	client *redis.Client
}

// NewNodeStore 创建 Node 存储
func NewNodeStore(client *redis.Client) *NodeStore {
	return &NodeStore{client: client}
}

// SaveNode 保存节点
func (s *NodeStore) SaveNode(ctx context.Context, node *types.ConnectNode) error {
	key := fmt.Sprintf("%s%s", NodePrefix, node.ID)

	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	return s.client.Set(ctx, key, data, NodeTTL).Err()
}

// GetNode 获取节点
func (s *NodeStore) GetNode(ctx context.Context, nodeID string) (*types.ConnectNode, error) {
	key := fmt.Sprintf("%s%s", NodePrefix, nodeID)

	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	var node types.ConnectNode
	if err := json.Unmarshal([]byte(data), &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node: %w", err)
	}

	return &node, nil
}

// GetAllNodes 获取所有节点
func (s *NodeStore) GetAllNodes(ctx context.Context) ([]*types.ConnectNode, error) {
	pattern := fmt.Sprintf("%s*", NodePrefix)

	var cursor uint64
	var nodes []*types.ConnectNode

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			data, err := s.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var node types.ConnectNode
			if err := json.Unmarshal([]byte(data), &node); err != nil {
				continue
			}
			nodes = append(nodes, &node)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nodes, nil
}

