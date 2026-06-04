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
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

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

	// ETCD Endpoints (用于创建 Resolver)
	etcdEndpoints []string

	// Connect-Node 客户端池（nodeID -> *BroadcastClient）
	broadCastClientMap map[string]*BroadcastClient

	// 队列满丢弃计数（用于分析丢包）
	queueFullDropCount int64

	// Metrics
	metrics *metrics.MetricsCollector

	// 上下文控制
	ctx    context.Context
	cancel context.CancelFunc

	// WaitGroup for WatchConnectNodes goroutine
	watchWG sync.WaitGroup
}

// NewPushManagerServer 创建 Push-Manager 服务器
func NewPushManagerServer(
	managerID string,
	cfg *config.Config,
	discovery *etcd.ServiceDiscovery,
	etcdEndpoints []string,
	metricsCollector *metrics.MetricsCollector,
) *PushManagerServer {
	ctx, cancel := context.WithCancel(context.Background())

	pms := &PushManagerServer{
		managerID:          managerID,
		config:             cfg,
		discovery:          discovery,
		etcdEndpoints:      etcdEndpoints,
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

	s.watchWG.Add(1)
	go func() {
		defer s.watchWG.Done()
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

		// 🔑 改为使用 ETCD Resolver 连接（而不是直连）
		log.Printf("🔍 [Push-Manager] 通过 ETCD 连接 Connect-Node: %s", nodeID)

		resolverBuilder, err := etcd.GetETCDResolverBuilder(s.etcdEndpoints)
		if err != nil {
			log.Printf("❌ [Push-Manager] 获取 ETCD Resolver 失败: %v\n", err)
			cancel()
			continue
		}

		// 🔑 关键：target 格式必须是 "etcd:///服务名"（服务名不带开头斜杠）
		target := "etcd:///connect-node"
		log.Printf("   目标: %s", target)

		// 建立 gRPC 连接（使用 ETCD 服务发现）
		// 🔑 添加负载均衡配置和 keepalive 参数
		conn, err := grpc.DialContext(
			ctx,
			target,
			grpc.WithResolvers(resolverBuilder),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100*1024*1024)),
			// Keepalive 参数：防止 NAT 超时导致连接断开
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second, // 每 30s 发一次 ping
				Timeout:             10 * time.Second, // ping 超时时间
				PermitWithoutStream: true,             // 没有 active stream 时也发 ping
			}),
			// 负载均衡配置
			grpc.WithDefaultServiceConfig(`{
				"loadBalancingPolicy":"round_robin",
				"healthCheckConfig": {
					"serviceName": "connect-node"
				}
			}`),
		)
		if err != nil {
			log.Printf("❌ [Push-Manager] 连接到 %s 失败: %v\n", target, err)
			cancel()
			continue
		}

		log.Printf("✅ [Push-Manager] gRPC 连接已创建: %s", nodeID)

		client := push.NewCometClient(conn)

		broadcastClient := &BroadcastClient{
			serverID:      nodeID,
			client:        client,
			broadcastChan: make(chan *push.BroadcastReq, 1000), // 缓冲队列
			conn:          conn,
			ctx:           ctx,
			cancel:        cancel,
		}

		// 启动单个工作协程处理消息（gRPC 内部已有 HTTP/2 多路复用）
		go broadcastClient.runWorker(0)

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
			// 🔑 使用 WaitForReady 避免连接未就绪时调用失败
			ctx, cancel := context.WithTimeout(bc.ctx, 10*time.Second)

			startTime := time.Now()
			_, err := bc.client.Broadcast(ctx, req, grpc.WaitForReady(true))
			elapsed := time.Since(startTime)
			cancel() // 释放资源

			if err != nil {
				log.Printf("❌ [Worker-%s-%d] 推送消息失败 (耗时: %v): %v\n", bc.serverID, workerID, elapsed, err)
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

	for _, client := range s.broadCastClientMap {
		select {
		case client.broadcastChan <- &args:
		default:
			atomic.AddInt64(&s.queueFullDropCount, 1)
			// 不在此处打日志，避免刷屏；累计值由 main 中 10s 周期 stat 打印
		}
	}
}

// GetQueueFullDropCount 返回因队列满而丢弃的消息数（用于排查丢包）
func (s *PushManagerServer) GetQueueFullDropCount() int64 {
	return atomic.LoadInt64(&s.queueFullDropCount)
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
	s.EnqueueBroadcastMsg(req)

	return &broadcast.BroadCastReply{
		Code: "0",
		Msg:  "OK",
		Desc: "消息已加入推送队列",
	}, nil
}
