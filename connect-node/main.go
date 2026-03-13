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
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	grpcWriteBufferSize = 256 * 1024
	grpcReadBufferSize  = 64 * 1024
)

func main() {

	profilingSettings := loadProfilingSettingsFromEnv()
	applyProfilingSettings(profilingSettings)

	// 初始化全局日志输出到文件
	logFile := getEnv("CONNECT_NODE_LOG_FILE", "connect-node.log")
	if err := initGlobalLogger(logFile); err != nil {
		log.Printf("⚠️  初始化日志文件失败: %v，将只输出到控制台\n", err)
	} else {
		log.Printf("📝 日志已输出到文件: %s\n", logFile)
	}

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
	if err != nil {
		log.Fatalf("❌ Metrics 初始化失败: %v\n", err)
	}
	setCriticalMetricsCollector(metricsCollector)
	log.Printf("✅ Metrics 初始化成功\n")

	// 先注册到 ETCD，让其他服务能发现本服务
	log.Printf("📝 注册服务到 ETCD...\n")
	go etcd.RegisterEndPointToEtcd(ctx, cfg.nodeAddress, "connect-node", cfg.config.ETCD.Endpoints)

	// 等待一小段时间确保注册完成
	time.Sleep(1 * time.Second)

	// 创建 Controller 客户端（非阻塞模式，立即返回，gRPC 在后台自动发现和连接）
	controllerClient := newLogicClientNonBlocking(cfg.config.RpcConfig, cfg.config.ETCD.Endpoints)
	log.Printf("✅ Controller 客户端创建成功（将在后台通过 ETCD 自动发现并连接）\n")

	// 创建 ConnectNode 服务器
	connectNodeServer := NewConnectNodeServer(
		cfg.nodeID,
		cfg.nodeAddress,
		cfg.config,
		controllerClient,
		metricsCollector,
	)

	// 启动 gRPC 服务器（用于接收 Push-Manager 的推送）
	grpcServer := grpc.NewServer(
		grpc.SharedWriteBuffer(true),
		grpc.WriteBufferSize(grpcWriteBufferSize),
		grpc.ReadBufferSize(grpcReadBufferSize),
	)

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

	// 启动 Channel 丢弃统计协程（每 5 秒打印一次）
	// 初始化丢弃日志文件
	dropLogFile := getEnv("DROP_LOG_FILE", "/app/logs/drop.log")
	if err := InitDropLogger(dropLogFile); err != nil {
		log.Printf("⚠️  初始化丢弃日志失败: %v\n", err)
	} else {
		log.Printf("📝 丢弃日志已输出到: %s\n", dropLogFile)
	}

	go func() {
		var lastDropCount int64
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			<-ticker.C
			currentDrop := GetGlobalDropCount()
			if currentDrop > 0 {
				diffDrop := currentDrop - lastDropCount
				dropLog("⚠️  [Stats] Signal channel 丢弃统计: 总丢弃=%d, 最近5s丢弃=%d", currentDrop, diffDrop)
				lastDropCount = currentDrop
			}
		}
	}()

	// InitWebsocket 需要端口列表，不是完整地址
	// 使用 GettyConfig.Ports 作为 WebSocket 端口
	ports := cfg.config.GettyConfig.Ports
	if len(ports) == 0 {
		ports = []string{fmt.Sprintf("%d", cfg.httpPort)}
	}
	log.Printf("🔌 初始化 Getty WebSocket 服务器\n")
	log.Printf("   - 监听地址: %s\n", cfg.config.GettyConfig.Host)
	log.Printf("   - 监听端口: %v\n", ports)
	log.Printf("   - WebSocket 路径: /connect\n")
	log.Printf("   - 连接 URL: ws://localhost:%s/connect\n", ports[0])

	// 初始化 WebSocket 日志文件（从环境变量获取，默认为 connect-node/websocket.log）
	wsLogFile := getEnv("WEBSOCKET_LOG_FILE", "websocket.log")
	if err := InitWebsocketLogger(wsLogFile); err != nil {
		log.Printf("⚠️  初始化 WebSocket 日志文件失败: %v，将只输出到控制台\n", err)
	}

	err = InitWebsocket(connectNodeServer, ports, 0)
	if err != nil {
		log.Printf("⚠️  InitWebsocket 服务器错误: %v\n", err)
		return
	}

	if profilingSettings.pprofEnabled {
		go func() {
			addr := fmt.Sprintf(":%d", profilingSettings.pprofPort)
			log.Printf("🔬 pprof 已启动: http://localhost:%d/debug/pprof/\n", profilingSettings.pprofPort)
			if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
				log.Printf("⚠️  pprof 服务错误: %v\n", err)
			}
		}()
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
	connectNodeServer.Stop()

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

// newLogicClientNonBlocking 非阻塞创建 Controller 客户端
func newLogicClientNonBlocking(c *config.RpcConfig, etcdEndpoints []string) (controllerClient controller.ControllerServiceClient) {

	log.Printf("🔍 连接 ETCD: %v", etcdEndpoints)
	resolverBuilder, err := etcd.GetETCDResolverBuilder(etcdEndpoints)
	if err != nil {
		log.Printf("❌ 获取 ETCD Resolver 失败: %v", err)
		panic(err)
	}

	log.Printf("🔗 通过 ETCD 连接 Controller-Manager（非阻塞模式）...")

	// 🔑 关键：target 格式必须是 "etcd:///服务名"（服务名不带开头斜杠）
	target := "etcd:///controller-manager"
	log.Printf("   目标: %s", target)

	// 添加连接选项：
	// 1. 非阻塞模式（异步连接）
	// 2. 设置初始窗口大小和 Keepalive
	// 3. 启用负载均衡（round_robin）- 这对 ETCD Resolver 很重要
	// 4. WaitForReady - 等待首次连接建立
	conn, err := grpc.Dial(target,
		grpc.WithResolvers(resolverBuilder),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithSharedWriteBuffer(true),
		grpc.WithWriteBufferSize(grpcWriteBufferSize),
		grpc.WithReadBufferSize(grpcReadBufferSize),
		grpc.WithDefaultServiceConfig(`{
			"loadBalancingPolicy":"round_robin",
			"healthCheckConfig": {
				"serviceName": "controller"
			}
		}`),
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
	// 优先从环境变量获取配置文件路径，默认使用 connect-node/config.yaml
	configFile := getEnv("CONFIG_FILE", "config.yaml")
	cfg := config.LoadConfigFromFile(configFile)

	nodeID := getEnv("NODE_ID", "connect-node-1")
	grpcPort := getEnvAsInt("GRPC_PORT", 50052)
	httpPort := getEnvAsInt("HTTP_PORT", 8083)
	metricsPort := getEnvAsInt("METRICS_PORT", 9091)
	controllerAddress := getEnv("CONTROLLER_ADDRESS", "localhost:50051")

	// 打印加载的配置，用于调试
	log.Printf("📋 加载配置文件: %s", configFile)
	log.Printf("   - Protocol.SvrProto: %d (signal channel 缓冲区大小)", cfg.Protocol.SvrProto)
	log.Printf("   - Protocol.CliProto: %d", cfg.Protocol.CliProto)
	log.Printf("   - Bucket.Channel: %d", cfg.Bucket.Channel)
	if cfg.SharedWriter != nil {
		log.Printf("   - SharedWriter.BatchSize: %d", cfg.SharedWriter.BatchSize)
		log.Printf("   - SharedWriter.MaxBatchBytes: %d", cfg.SharedWriter.MaxBatchBytes)
		log.Printf("   - SharedWriter.FlushInterval: %v", cfg.SharedWriter.FlushInterval)
		log.Printf("   - SharedWriter.QueueSize: %d", cfg.SharedWriter.QueueSize)
	}
	if cfg.LeaveQueue != nil {
		log.Printf("   - LeaveQueue.RetryDelay: %v", cfg.LeaveQueue.RetryDelay)
		log.Printf("   - LeaveQueue.MaxAttempts: %d", cfg.LeaveQueue.MaxAttempts)
	}

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

// initGlobalLogger 初始化全局日志输出到文件
// 如果 logFile 为空字符串，则只输出到控制台
func initGlobalLogger(logFile string) error {
	if logFile == "" {
		return nil
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	// 同时输出到控制台和文件
	multiWriter := io.MultiWriter(os.Stdout, f)
	log.SetOutput(multiWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return nil
}
