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
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
)

// BroadcastClient 广播客户端包装
type BroadcastClient struct {
	serverID      string
	client        push.CometClient
	broadcastChan chan *push.BroadcastReq
	routineSize   uint64
	conn          *grpc.ClientConn

	ctx    context.Context
	cancel context.CancelFunc
}

// PushManagerServer Push-Manager 服务器
type PushManagerServer struct {
	broadcast.UnimplementedPushServerServer

	// 基础配置
	managerID string
	config    *config.Config

	// ETCD 服务发现
	discovery *etcd.ServiceDiscovery

	// Connect-Node 客户端池（nodeID -> *BroadcastClient）
	broadCastClientMap map[string]*BroadcastClient

	// Metrics
	metrics *metrics.MetricsCollector

	// 上下文控制
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPushManagerServer 创建 Push-Manager 服务器
func NewPushManagerServer(
	managerID string,
	cfg *config.Config,
	discovery *etcd.ServiceDiscovery,
	metricsCollector *metrics.MetricsCollector,
) *PushManagerServer {
	ctx, cancel := context.WithCancel(context.Background())

	pms := &PushManagerServer{
		managerID:          managerID,
		config:             cfg,
		discovery:          discovery,
		broadCastClientMap: make(map[string]*BroadcastClient),
		metrics:            metricsCollector,
		ctx:                ctx,
		cancel:             cancel,
	}

	return pms
}

// WatchConnectNodes 🔥 监听 Connect-Node 服务发现事件（事件驱动）
func (s *PushManagerServer) WatchConnectNodes(ctx context.Context) {
	log.Printf("🔍 [Push-Manager] 开始监听 Connect-Node 事件...\n")

	// 首先获取已有的节点
	instances, _ := s.discovery.GetEndpoints()

	s.createBroadcastClient(instances)

	// 🔥 获取事件通道，监听 ETCD 事件
	eventChan := s.discovery.GetEventChan()

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("⚠️  [Push-Manager] 停止监听 Connect-Node 事件\n")
				s.cleanupAllClients()
				return

			case event, ok := <-eventChan:
				if !ok {
					// 事件通道已关闭
					return
				}
				log.Printf("etcd discovery clients %s", event)

				endpoints, err := s.discovery.GetEndpoints()

				if err != nil {
					log.Printf("get endpoints error ")
					continue
				}

				s.createBroadcastClient(endpoints)

			}
		}
	}()

}

// createBroadcastClient 为指定的 Connect-Node 创建广播客户端
func (s *PushManagerServer) createBroadcastClient(instances []string) {
	// 保留已存在的客户端，只创建新的
	comets := make(map[string]*BroadcastClient)
	
	// 先复制已存在的客户端
	for k, v := range s.broadCastClientMap {
		comets[k] = v
	}
	
	// 处理所有实例
	for _, instance := range instances {
		nodeID := fmt.Sprintf("connect-node-%s", instance)
		
		// 如果已存在，跳过
		if _, exists := comets[nodeID]; exists {
			log.Printf("✅ [Push-Manager] Connect-Node 客户端已存在: %s (%s)\n", nodeID, instance)
			continue
		}

		log.Printf("🔗 [Push-Manager] 创建 Connect-Node 客户端: %s (%s)\n", nodeID, instance)

		ctx, cancel := context.WithCancel(s.ctx)

		// 建立 gRPC 连接
		conn, err := grpc.DialContext(
			ctx,
			instance,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100*1024*1024)),
		)
		if err != nil {
			log.Printf("❌ [Push-Manager] 连接到 %s 失败: %v\n", instance, err)
			cancel()
			continue // 继续处理下一个节点，而不是直接返回
		}

		client := push.NewCometClient(conn)
		routineSize := uint64(10) // 工作协程数量

		broadcastClient := &BroadcastClient{
			serverID:      nodeID,
			client:        client,
			broadcastChan: make(chan *push.BroadcastReq, 1000), // 缓冲队列
			routineSize:   routineSize,
			conn:          conn,
			ctx:           ctx,
			cancel:        cancel,
		}

		// 启动工作协程处理消息
		for i := uint64(0); i < routineSize; i++ {
			go broadcastClient.runWorker(i)
		}

		comets[nodeID] = broadcastClient
	}

	// 更新客户端映射
	s.broadCastClientMap = comets

	log.Printf("✅ [Push-Manager] Connect-Node 客户端创建成功，共 %d 个节点\n", len(comets))
}

// runWorker 工作协程：从队列中取出消息并发送
func (bc *BroadcastClient) runWorker(workerID uint64) {
	log.Printf("👷 [Worker-%s-%d] 已启动\n", bc.serverID, workerID)

	defer func() {
		log.Printf("👷 [Worker-%s-%d] 已停止\n", bc.serverID, workerID)
	}()

	for {
		select {
		case <-bc.ctx.Done():
			return
		case req, ok := <-bc.broadcastChan:
			if !ok {
				// 通道已关闭
				return
			}
			// 发送消息到 Connect-Node
			ctx, cancel := context.WithTimeout(bc.ctx, 5*time.Second)
			_, err := bc.client.Broadcast(ctx, req)
			cancel()

			if err != nil {
				log.Printf("❌ [Worker-%s-%d] 推送消息失败: %v\n", bc.serverID, workerID, err)
			} else {
				log.Printf("✅ [Worker-%s-%d] 消息推送成功\n", bc.serverID, workerID)
			}
		}
	}
}

// EnqueueBroadcastMsg 将消息加入到所有 Connect-Node 的队列中
func (s *PushManagerServer) EnqueueBroadcastMsg(req *broadcast.BroadCastReq) {

	var args = push.BroadcastReq{
		Proto:   req.Proto,
		ProtoOp: req.Proto.Op, // 设置 ProtoOp，用于客户端的 NeedPush 检查
	}

	for nodeID, client := range s.broadCastClientMap {
		select {
		case client.broadcastChan <- &args:
			log.Printf("📤 [Push-Manager] 消息已加入队列: %s, op=%d\n", nodeID, args.ProtoOp)
		default:
			log.Printf("⚠️  [Push-Manager] 节点 %s 的队列已满，丢弃消息\n", nodeID)
		}
	}
}

// Close 关闭客户端
func (bc *BroadcastClient) Close() {
	log.Printf("🔌 [Push-Manager] 关闭客户端: %s\n", bc.serverID)
	bc.cancel()
	close(bc.broadcastChan)

	if bc.conn != nil {
		bc.conn.Close()
	}
	log.Printf("✅ [Push-Manager] 客户端已关闭: %s\n", bc.serverID)
}

// cleanupAllClients 清理所有客户端
func (s *PushManagerServer) cleanupAllClients() {
	for nodeID, client := range s.broadCastClientMap {
		log.Printf("🧹 [Push-Manager] 清理客户端: %s\n", nodeID)
		client.Close()
	}
	s.broadCastClientMap = make(map[string]*BroadcastClient)
}

// ========== RPC 方法实现 ==========

// Broadcast 实现 PushServer 的 Broadcast 方法
func (s *PushManagerServer) Broadcast(ctx context.Context, req *broadcast.BroadCastReq) (*broadcast.BroadCastReply, error) {
	log.Printf("📡 [Push-Manager] 收到广播请求\n")

	// 将消息加入所有 Connect-Node 的队列
	s.EnqueueBroadcastMsg(req)

	return &broadcast.BroadCastReply{
		Code: "0",
		Msg:  "OK",
		Desc: "消息已加入推送队列",
	}, nil
}
