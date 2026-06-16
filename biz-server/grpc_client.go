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
	"fmt"
	"log"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PushManagerClient Push-Manager gRPC 客户端
type PushManagerClient struct {
	conn   *grpc.ClientConn
	client broadcast.PushServerClient
}

// NewPushManagerClient 创建 Push-Manager 客户端
func NewPushManagerClient(addr string) (*PushManagerClient, error) {
	log.Printf("🔌 连接到 Push-Manager: %s", addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}

	log.Printf("✅ gRPC 连接成功")

	return &PushManagerClient{
		conn:   conn,
		client: broadcast.NewPushServerClient(conn),
	}, nil
}

// BroadcastToRoom 广播消息到房间
func (c *PushManagerClient) BroadcastToRoom(roomID, message string) error {
	log.Printf("📢 广播消息到房间: %s", roomID)
	log.Printf("   内容: %s", message)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 构造消息 Proto
	protoMsg := &proto.Proto{
		Ver:    1,
		Op:     2, // OP_SEND_MSG
		Seq:    1,
		Roomid: roomID,
		Body:   []byte(message),
	}

	req := &broadcast.BroadCastRoomReq{
		RoomId: roomID,
		Proto:  protoMsg,
	}

	resp, err := c.client.BroadcastToRoom(ctx, req)
	if err != nil {
		return fmt.Errorf("广播失败: %w", err)
	}

	log.Printf("✅ 广播成功")
	log.Printf("   响应: code=%s, msg=%s", resp.Code, resp.Msg)

	return nil
}

// PushToUser 推送消息给指定用户（暂未实现）
func (c *PushManagerClient) PushToUser(userID, message string) error {
	log.Printf("📤 推送消息给用户: %s", userID)
	log.Printf("   内容: %s", message)
	log.Printf("⚠️  PushToUser 功能暂未实现")
	return nil
}

// GetRoomStats 获取房间统计信息（暂未实现）
func (c *PushManagerClient) GetRoomStats() error {
	log.Printf("📊 获取系统统计")
	log.Printf("⚠️  GetRoomStats 功能暂未实现")
	return nil
}

// Close 关闭连接
func (c *PushManagerClient) Close() error {
	log.Printf("👋 关闭 gRPC 连接")
	return c.conn.Close()
}
