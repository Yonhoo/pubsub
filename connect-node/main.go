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
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 加载配置
	cfg := loadConnectNodeConfig()

	log.Printf("🚀 启动 Connect-Node: %s (%s)\n", cfg.nodeID, cfg.nodeAddress)
	log.Printf("📍 配置信息:\n")
	log.Printf("   - gRPC 端口: %d\n", cfg.grpcPort)
	log.Printf("   - HTTP 端口: %d\n", cfg.httpPort)
	log.Printf("   - Controller: %s\n", cfg.controllerAddress)
	log.Printf("   - ETCD: %v\n", cfg.config.ETCD.Endpoints)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化 Tracing
	tracingShutdown, err := tracing.InitTracer(cfg.nodeID, "connect-node")
	if err != nil {
		log.Printf("⚠️  Tracing 初始化失败: %v\n", err)
	} else {
		defer tracingShutdown(ctx)
		log.Printf("✅ Tracing 初始化成功\n")
	}

	// 初始化 Metrics
	metricsCollector, err := metrics.NewMetricsCollector(cfg.nodeID, "connect-node")
	log.Printf("✅ Metrics 初始化成功\n")

	// 先注册到 ETCD，让其他服务能发现本服务
	log.Printf("📝 注册服务到 ETCD...\n")
	go etcd.RegisterEndPointToEtcd(ctx, cfg.nodeAddress, "/services/connect-node", cfg.config.ETCD.Endpoints)

	// 等待一小段时间确保注册完成
	time.Sleep(1 * time.Second)

	// 创建 Controller 客户端（阻塞模式，确保连接成功后再启动）
	controllerClient := newLogicClient(cfg.config.RpcConfig, cfg.config.ETCD.Endpoints)
	log.Printf("✅ Controller 客户端创建成功（通过 ETCD 服务发现）\n")

	// 创建 ConnectNode 服务器
	connectNodeServer := NewConnectNodeServer(
		cfg.nodeID,
		cfg.nodeAddress,
		cfg.config,
		controllerClient,
		metricsCollector,
	)

	// 启动 gRPC 服务器（用于接收 Push-Manager 的推送）
	grpcServer := grpc.NewServer()

	push.RegisterCometServer(grpcServer, connectNodeServer)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.grpcPort))
	if err != nil {
		log.Fatalf("❌ gRPC 监听失败: %v\n", err)
	}

	go func() {
		log.Printf("🚀 gRPC 服务器启动: :%d\n", cfg.grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ gRPC 服务器启动失败: %v\n", err)
		}
	}()

	// 注意：不再启动标准 HTTP 服务器，Getty WebSocket 服务器会处理 WebSocket 连接
	// HTTP 健康检查等功能可以通过 Metrics 服务器提供

	// 启动 Metrics HTTP 服务器
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.metricsPort),
		Handler: metricsCollector.Handler(),
	}

	go func() {
		log.Printf("📊 Metrics 服务器启动: :%d\n", cfg.metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("⚠️  Metrics 服务器错误: %v\n", err)
		}
	}()

	// InitWebsocket 需要端口列表，不是完整地址
	// 使用 GettyConfig.Ports 作为 WebSocket 端口
	ports := cfg.config.GettyConfig.Ports
	if len(ports) == 0 {
		ports = []string{fmt.Sprintf("%d", cfg.httpPort)}
	}
	log.Printf("🔌 初始化 Getty WebSocket 服务器，端口: %v\n", ports)

	err = InitWebsocket(connectNodeServer, ports, 0)
	if err != nil {
		log.Printf("⚠️  InitWebsocket 服务器错误: %v\n", err)
		return
	}

	log.Printf("✅ Connect-Node 启动完成\n")
	log.Printf("📝 WebSocket 端点: ws://localhost:%d/connect?user_id=xxx&user_name=xxx&room_id=xxx\n", cfg.httpPort)
	log.Printf("📝 健康检查: http://localhost:%d/health\n", cfg.httpPort)
	log.Printf("📝 统计信息: http://localhost:%d/stats\n", cfg.httpPort)
	log.Printf("📝 Metrics: http://localhost:%d/metrics\n", cfg.metricsPort)

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("\n🛑 收到退出信号，开始优雅关闭...\n")

	// 注销节点
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// 关闭 Getty WebSocket 服务器
	for _, server := range serverList {
		server.Close()
	}

	// 关闭 Metrics 服务器
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("⚠️  Metrics 服务器关闭错误: %v\n", err)
	}

	// 停止 gRPC 服务器
	grpcServer.GracefulStop()

	// 取消上下文
	cancel()

	log.Printf("✅ Connect-Node 已关闭\n")
}

func newLogicClient(c *config.RpcConfig, etcdEndpoints []string) (controllerClient controller.ControllerServiceClient) {

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.TimeOut))
	defer cancel()

	// 方案：直接从 ETCD 获取 endpoint 地址，然后直接连接
	// 这是 Push-Manager 成功使用的方式
	log.Printf("🔍 通过 ETCD 查询 Controller-Manager 地址...")
	
	// 创建 ServiceDiscovery
	discovery, err := etcd.NewServiceDiscovery(etcdEndpoints, "controller-manager")
	if err != nil {
		log.Printf("❌ 创建 ServiceDiscovery 失败: %v", err)
		panic(err)
	}
	defer discovery.Close()
	
	// 等待一下让 ETCD 查询完成
	time.Sleep(500 * time.Millisecond)
	
	// 获取 endpoints
	endpoints, err := discovery.GetEndpoints()
	if err != nil {
		log.Printf("❌ 获取 Controller-Manager endpoints 失败: %v", err)
		panic(err)
	}
	
	if len(endpoints) == 0 {
		log.Printf("❌ 未找到任何 Controller-Manager 实例")
		panic(fmt.Errorf("no controller-manager instances found"))
	}
	
	// 使用第一个 endpoint（单实例场景）
	target := endpoints[0]
	log.Printf("🔗 直接连接到 Controller-Manager: %s", target)

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), // 阻塞直到连接建立或超时
	)

	if err != nil {
		log.Printf("❌ 连接 Controller-Manager 失败: %v", err)
		panic(err)
	}

	log.Printf("✅ 已建立 gRPC 连接到 Controller-Manager")

	return controller.NewControllerServiceClient(conn)

}

// newLogicClientNonBlocking 非阻塞创建 Controller 客户端
func newLogicClientNonBlocking(c *config.RpcConfig, etcdEndpoints []string) (controllerClient controller.ControllerServiceClient) {

	log.Printf("🔍 连接 ETCD: %v", etcdEndpoints)
	resolverBuilder, err := etcd.GetETCDResolverBuilder(etcdEndpoints)
	if err != nil {
		log.Printf("❌ 获取 ETCD Resolver 失败: %v", err)
		panic(err)
	}

	log.Printf("🔗 通过 ETCD 连接 Controller-Manager（非阻塞模式）...")

	// etcd v3 naming resolver: 使用 resolver 的 scheme
	target := fmt.Sprintf("%s:///services/controller-manager", resolverBuilder.Scheme())
	log.Printf("   目标: %s", target)

	// 不使用 WithBlock，允许异步连接
	conn, err := grpc.Dial(target,
		grpc.WithResolvers(resolverBuilder),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		log.Printf("❌ 创建 gRPC 连接失败: %v", err)
		panic(err)
	}

	log.Printf("✅ Controller 客户端已创建（将在后台建立连接）")

	return controller.NewControllerServiceClient(conn)

}

// ConnectNodeConfig 配置
type ConnectNodeConfig struct {
	nodeID            string
	nodeAddress       string
	grpcPort          int
	httpPort          int
	metricsPort       int
	controllerAddress string
	config            *config.Config
}

// loadConnectNodeConfig 加载配置
func loadConnectNodeConfig() *ConnectNodeConfig {
	cfg := config.LoadConfig()

	nodeID := getEnv("NODE_ID", "connect-node-1")
	grpcPort := getEnvAsInt("GRPC_PORT", 50052)
	httpPort := getEnvAsInt("HTTP_PORT", 8083)
	metricsPort := getEnvAsInt("METRICS_PORT", 9091)
	controllerAddress := getEnv("CONTROLLER_ADDRESS", "localhost:50051")

	// 构建节点地址（gRPC 地址，供其他服务调用）
	// 本地开发使用 localhost，生产环境可以从环境变量获取
	nodeAddr := os.Getenv("NODE_ADDR")
	if nodeAddr == "" {
		nodeAddr = "localhost"
	}
	nodeAddress := fmt.Sprintf("%s:%d", nodeAddr, grpcPort)

	return &ConnectNodeConfig{
		nodeID:            nodeID,
		nodeAddress:       nodeAddress,
		grpcPort:          grpcPort,
		httpPort:          httpPort,
		metricsPort:       metricsPort,
		controllerAddress: controllerAddress,
		config:            cfg,
	}
}

// getEnv 获取环境变量（带默认值）
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量作为整数
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intVal int
		fmt.Sscanf(value, "%d", &intVal)
		if intVal > 0 {
			return intVal
		}
	}
	return defaultValue
}
