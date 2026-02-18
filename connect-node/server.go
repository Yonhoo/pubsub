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
	getty "github.com/AlexStocks/getty/transport"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"github.com/zhenjl/cityhash"
	"log"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
)

// ConnectNodeServer 连接节点服务器
type ConnectNodeServer struct {
	push.UnimplementedCometServer

	// 基础配置
	nodeID      string
	nodeAddress string
	config      *config.Config

	// gRPC 客户端（用于调用 Controller）
	controllerClient controller.ControllerServiceClient

	// Metrics
	metrics *metrics.MetricsCollector

	// accept round store
	round *Round

	sessionMap map[*getty.Session]*clientProtoSession

	buckets []*Bucket

	bucketIdx uint32

	// 房间同步停止信号
	stopRoomSync chan struct{}
}

// NewConnectNodeServer 创建连接节点服务器
func NewConnectNodeServer(
	nodeID, nodeAddress string,
	cfg *config.Config,
	controllerClient controller.ControllerServiceClient,
	metricsCollector *metrics.MetricsCollector,
) *ConnectNodeServer {
	// 优先使用传入的 nodeID（来自环境变量 NODE_ID），如果为空则使用配置中的 Server.ID
	// 注意：传入的 nodeID 应该是正确的（来自环境变量），不应该被覆盖
	finalNodeID := nodeID
	if finalNodeID == "" {
		if cfg.Server.ID != "" {
			finalNodeID = cfg.Server.ID
			log.Printf("⚠️  [ConnectNodeServer] 传入的 nodeID 为空，使用配置的 Server.ID: %s", finalNodeID)
		} else {
			log.Printf("❌ [ConnectNodeServer] nodeID 和 Server.ID 都为空！这会导致 JoinRoom 失败！")
		}
	} else {
		log.Printf("✅ [ConnectNodeServer] 使用传入的 nodeID: %s", finalNodeID)
	}

	if finalNodeID == "" {
		log.Printf("❌ [ConnectNodeServer] 最终 nodeID 为空！这会导致 JoinRoom 失败！")
		finalNodeID = "connect-node-1" // 最后的兜底，避免崩溃
		log.Printf("⚠️  [ConnectNodeServer] 使用默认 nodeID: %s", finalNodeID)
	}

	server := &ConnectNodeServer{
		nodeID:           finalNodeID, // 使用确定后的 nodeID
		nodeAddress:      nodeAddress,
		config:           cfg,
		controllerClient: controllerClient,
		metrics:          metricsCollector,
		buckets:          make([]*Bucket, cfg.Bucket.Size),
		bucketIdx:        uint32(cfg.Bucket.Size),
		round:            NewRound(cfg),
		stopRoomSync:     make(chan struct{}),
	}

	for i := 0; i < cfg.Bucket.Size; i++ {
		server.buckets[i] = NewBucket(cfg.Bucket)
	}

	go server.onlineproc()

	return server
}

func (s *ConnectNodeServer) Buckets() []*Bucket {
	return s.buckets
}

func (s *ConnectNodeServer) Bucket(clientID string) *Bucket {

	idx := cityhash.CityHash32([]byte(clientID), uint32(len(clientID))) % s.bucketIdx

	return s.buckets[idx]
}

func (s *ConnectNodeServer) onlineproc() {
	for {
		var (
			allRoomsCount map[string]int32
			//err           error
		)

		roomCount := make(map[string]int32)

		for _, bucket := range s.buckets {
			for roomID, count := range bucket.RoomsCount() {
				roomCount[roomID] += count
			}
		}

		for _, bucket := range s.buckets {
			bucket.UpRoomsCount(allRoomsCount)
		}

		time.Sleep(time.Second * 10)
	}

}

// ========== RPC 方法实现 ==========

// 假设 userId 是全局 服务端颁发
func (s *ConnectNodeServer) PushMsg(ctx context.Context, req *push.PushMsgReq) (reply *push.PushMsgReply, err error) {
	if len(req.Keys) == 0 || req.Proto == nil {
		return nil, pkg.ErrPushMsgArg
	}

	for _, key := range req.Keys {
		bucket := s.Bucket(key)

		if bucket == nil {
			continue
		}

		if channel := bucket.Channel(key); channel != nil {
			if !channel.NeedPush(req.ProtoOp) {
				continue
			}

			if err = channel.Push(req.Proto); err != nil {
				return
			}
		}

	}

	return &push.PushMsgReply{}, nil

}

func (s *ConnectNodeServer) Broadcast(ctx context.Context, req *push.BroadcastReq) (*push.BroadcastReply, error) {
	log.Printf("📡 [ConnectNodeServer] 收到 Broadcast gRPC 请求: op=%d, roomId=%s", req.ProtoOp, req.GetProto().Roomid)
	if req.Proto == nil {
		return nil, pkg.ErrBroadCastArg
	}

	//go func() {
	//log.Printf("🚀 [ConnectNodeServer] 开始广播到 %d 个 buckets", len(s.Buckets()))
	for _, bucket := range s.Buckets() {
		//channelCount := bucket.ChannelCount()
		//log.Printf("📤 [ConnectNodeServer] 广播到 bucket[%d], channels=%d", i, channelCount)
		bucket.Broadcast(req.GetProto(), req.ProtoOp)
		//if req.Speed > 0 {
		//	t := bucket.ChannelCount() / int(req.Speed)
		//	time.Sleep(time.Duration(t) * time.Second)
		//}
	}
	log.Printf("✅ [ConnectNodeServer] Broadcast 完成")
	//}()
	return &push.BroadcastReply{}, nil
}

func (s *ConnectNodeServer) BroadcastRoom(ctx context.Context, req *push.BroadcastRoomReq) (*push.BroadcastRoomReply, error) {
	log.Printf("🎯 [ConnectNodeServer] 收到 BroadcastRoom gRPC 请求: roomID=%s", req.RoomID)
	if req.Proto == nil || req.RoomID == "" {
		log.Printf("❌ [ConnectNodeServer] 参数无效: roomID=%s, proto=%v", req.RoomID, req.Proto)
		return nil, pkg.ErrBroadCastRoomArg
	}
	log.Printf("🔄 [ConnectNodeServer] 分发到 %d 个 buckets", len(s.Buckets()))
	for i, bucket := range s.Buckets() {
		log.Printf("🔄 [ConnectNodeServer] 调用 bucket[%d].BroadcastRoom", i)
		bucket.BroadcastRoom(req)
	}
	log.Printf("✅ [ConnectNodeServer] BroadcastRoom 处理完成")
	return &push.BroadcastRoomReply{}, nil
}

func (s *ConnectNodeServer) Rooms(ctx context.Context, req *push.RoomsReq) (*push.RoomsReply, error) {
	var (
		roomIds = make(map[string]bool)
	)
	for _, bucket := range s.Buckets() {
		for roomID := range bucket.Rooms() {
			roomIds[roomID] = true
		}
	}
	return &push.RoomsReply{Rooms: roomIds}, nil
}
