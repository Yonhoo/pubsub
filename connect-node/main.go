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
	"bytes"
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
	"google.golang.org/grpc/keepalive"
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
		log.Printf("初始化日志文件失败: %v", err)
	}

	// 加载配置
	cfg := loadConnectNodeConfig()

	log.Printf("启动 Connect-Node: %s (%s) gRPC=%d HTTP=%d", cfg.nodeID, cfg.nodeAddress, cfg.grpcPort, cfg.httpPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化 Tracing
	tracingShutdown, err := tracing.InitTracer(cfg.nodeID, "connect-node")
	if err != nil {
		log.Printf("Tracing 初始化失败: %v", err)
	} else {
		defer tracingShutdown(ctx)
	}

	// 初始化 Metrics
	metricsCollector, err := metrics.NewMetricsCollector(cfg.nodeID, "connect-node")
	if err != nil {
		log.Fatalf("Metrics 初始化失败: %v", err)
	}
	setCriticalMetricsCollector(metricsCollector)

	// 先注册到 ETCD，让其他服务能发现本服务
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

	if connectNodeServer == nil {
		log.Printf("⚠️  connectNodeServer 初始化失败: %v\n", err)
		return
	}

	// 启动 gRPC 服务器（用于接收 Push-Manager 的推送）
	grpcServer := grpc.NewServer(
		grpc.SharedWriteBuffer(true),
		grpc.WriteBufferSize(grpcWriteBufferSize),
		grpc.ReadBufferSize(grpcReadBufferSize),
		// 允许客户端较频繁地 keepalive ping（push-manager 配置 30s）
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		// 服务端主动 keepalive，避免 NAT 超时
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    60 * time.Second,
			Timeout: 20 * time.Second,
		}),
	)

	push.RegisterCometServer(grpcServer, connectNodeServer)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.grpcPort))
	if err != nil {
		log.Fatalf("gRPC 监听失败: %v", err)
	}

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC 服务器启动失败: %v", err)
		}
	}()

	// 启动 Metrics HTTP 服务器
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.metricsPort),
		Handler: metricsCollector.Handler(),
	}

	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics 服务器错误: %v", err)
		}
	}()

	// 启动 Channel 丢弃统计协程（每 5 秒打印一次）
	dropLogFile := getEnv("DROP_LOG_FILE", "/app/logs/drop.log")
	if err := InitDropLogger(dropLogFile); err != nil {
		log.Printf("初始化丢弃日志失败: %v", err)
	}

	go func() {
		var lastDropCount int64
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentDrop := GetGlobalDropCount()
				if currentDrop > 0 {
					diffDrop := currentDrop - lastDropCount
					dropLog("Signal channel 丢弃统计: 总丢弃=%d, 最近5s丢弃=%d", currentDrop, diffDrop)
					lastDropCount = currentDrop
				}
			}
		}
	}()

	// InitWebsocket 需要端口列表
	ports := cfg.config.GettyConfig.Ports
	if len(ports) == 0 {
		ports = []string{fmt.Sprintf("%d", cfg.httpPort)}
	}

	// 初始化 WebSocket 日志文件
	wsLogFile := getEnv("WEBSOCKET_LOG_FILE", "websocket.log")
	if err := InitWebsocketLogger(wsLogFile); err != nil {
		log.Printf("初始化 WebSocket 日志文件失败: %v", err)
	}

	err = InitWebsocket(connectNodeServer, ports, 0)
	if err != nil {
		log.Printf("InitWebsocket 服务器错误: %v", err)
		return
	}

	if profilingSettings.pprofEnabled {
		go func() {
			addr := fmt.Sprintf(":%d", profilingSettings.pprofPort)
			if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
				log.Printf("pprof 服务错误: %v", err)
			}
		}()
	}

	log.Printf("Connect-Node 启动完成 ws://localhost:%d/connect metrics=:%d", cfg.httpPort, cfg.metricsPort)

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("收到退出信号，开始优雅关闭...")

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
		log.Printf("Metrics 服务器关闭错误: %v", err)
	}

	// 停止 gRPC 服务器
	grpcServer.GracefulStop()

	// 取消上下文
	cancel()

	log.Printf("Connect-Node 已关闭")
}

// newLogicClientNonBlocking 非阻塞创建 Controller 客户端
func newLogicClientNonBlocking(c *config.RpcConfig, etcdEndpoints []string) (controllerClient controller.ControllerServiceClient) {

	resolverBuilder, err := etcd.GetETCDResolverBuilder(etcdEndpoints)
	if err != nil {
		log.Printf("获取 ETCD Resolver 失败: %v", err)
		panic(err)
	}

	target := "etcd:///controller-manager"

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
		log.Printf("创建 gRPC 连接失败: %v", err)
		panic(err)
	}

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
	var sinks []io.Writer
	sinks = append(sinks, os.Stdout)

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file %s: %w", logFile, err)
		}
		sinks = append(sinks, f)
	}

	multiWriter := io.MultiWriter(sinks...)

	// 默认过滤 gorilla/websocket 的 "discarding reader close error" 噪声
	// 设置 CONNECT_NODE_KEEP_WS_NOISE=1 可保留(用于排查)
	if os.Getenv("CONNECT_NODE_KEEP_WS_NOISE") != "1" {
		multiWriter = newFilterWriter(multiWriter,
			"discarding reader close error",
		)
	}

	log.SetOutput(multiWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return nil
}

// filterWriter 包装 io.Writer，丢弃匹配任意子串的整行写入。
// gorilla/websocket 用 stdlib log.Printf("websocket: discarding ...") 输出，
// 而 stdlib log 一行一个 Write 调用，所以可以直接对单次 Write 的内容判断子串。
type filterWriter struct {
	inner    io.Writer
	patterns []string
}

func newFilterWriter(inner io.Writer, patterns ...string) *filterWriter {
	return &filterWriter{inner: inner, patterns: patterns}
}

func (w *filterWriter) Write(p []byte) (int, error) {
	for _, pat := range w.patterns {
		if bytes.Contains(p, []byte(pat)) {
			// 假装写成功,避免上游误判
			return len(p), nil
		}
	}
	return w.inner.Write(p)
}
