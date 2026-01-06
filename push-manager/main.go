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
	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
	"google.golang.org/grpc"
)

func main() {
	// 加载配置
	cfg := loadPushManagerConfig()

	log.Println(strings.Repeat("=", 80))
	log.Printf("🚀 启动 Push-Manager: %s (端口: %d)\n", cfg.managerID, cfg.grpcPort)
	log.Println(strings.Repeat("=", 80))
	log.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1️⃣ 初始化 OpenTelemetry
	log.Println("🔭 初始化 OpenTelemetry...")
	tracingShutdown, err := tracing.InitTracer(cfg.managerID, "push-manager")
	if err != nil {
		log.Printf("⚠️  OpenTelemetry 初始化失败: %v\n", err)
	} else {
		defer tracingShutdown(ctx)
		log.Printf("✅ OpenTelemetry 初始化成功\n")
	}

	// 2️⃣ 初始化 Metrics
	log.Println("📊 初始化 Metrics...")
	metricsCollector, err := metrics.NewMetricsCollector(cfg.managerID, "push-manager")
	if err != nil {
		log.Fatalf("❌ ETCD 初始化失败: %v\n", err)
		return
	}
	log.Printf("✅ Metrics 初始化成功\n")

	// 3️⃣ 注册服务到 ETCD
	log.Println("📝 注册服务到 ETCD...")
	go etcd.RegisterEndPointToEtcd(ctx, fmt.Sprintf("localhost:%d", cfg.grpcPort), "/services/push-manager", cfg.config.ETCD.Endpoints)
	
	// 等待一小段时间确保注册完成
	time.Sleep(1 * time.Second)
	log.Println("✅ 服务已注册到 ETCD")
	log.Println()

	// 4️⃣ 初始化 ETCD 服务发现
	log.Println("🔍 初始化 ETCD 服务发现...")
	etcdDiscovery, err := etcd.NewServiceDiscovery(cfg.config.ETCD.Endpoints, "connect-node")
	if err != nil {
		log.Fatalf("❌ ETCD 初始化失败: %v\n", err)
	}
	defer etcdDiscovery.Close()
	log.Printf("✅ ETCD 连接成功\n")

	// 5️⃣ 创建 Push-Manager 服务器
	log.Println("🏗️  创建 Push-Manager 服务器...")
	pushManager := NewPushManagerServer(
		cfg.managerID,
		cfg.config,
		etcdDiscovery,
		metricsCollector,
	)
	log.Printf("✅ Push-Manager 服务器创建成功\n")

	// 6️⃣ 启动 Connect-Node 发现与监听
	log.Println("👀 启动 Connect-Node 发现...")
	pushManager.WatchConnectNodes(ctx)

	// 等待发现节点
	time.Sleep(1 * time.Second)

	// 7️⃣ 创建 gRPC 服务器
	grpcServer := grpc.NewServer()

	broadcast.RegisterPushServerServer(grpcServer, pushManager)

	// 8️⃣ 启动 gRPC Server
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.grpcPort))
	if err != nil {
		log.Fatalf("❌ 监听失败: %v\n", err)
	}

	// 9️⃣ 启动 Metrics HTTP 服务器
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

	log.Println(strings.Repeat("=", 80))
	log.Println("✅ Push-Manager 运行中")
	log.Println(strings.Repeat("=", 80))
	log.Println()
	log.Println("📋 服务信息:")
	log.Printf("  - Manager ID: %s\n", cfg.managerID)
	log.Printf("  - gRPC 端口: %d\n", cfg.grpcPort)
	log.Printf("  - Metrics 端口: %d\n", cfg.metricsPort)
	log.Printf("  - ETCD: %v\n", cfg.config.ETCD.Endpoints)
	log.Println()
	log.Println("📡 可用 API:")
	log.Println("  - PushToRoom: 推送消息到房间")
	log.Println("  - PushToUser: 推送消息给指定用户")
	log.Println("  - BroadcastMessage: 广播消息")
	log.Println()
	log.Println("💡 使用示例:")
	log.Println("  grpcurl -plaintext localhost:50053 list")
	log.Println("  grpcurl -plaintext localhost:50053 pubsub.PushManagerService/PushToRoom")
	log.Println()
	log.Println("🚪 按 Ctrl+C 退出")
	log.Println(strings.Repeat("=", 80))
	log.Println()

	// 启动 gRPC Server
	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			log.Fatalf("❌ gRPC 服务启动失败: %v\n", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("\n🛑 收到退出信号，开始优雅关闭...")

	// 关闭 gRPC 服务器
	grpcServer.GracefulStop()

	// 关闭 Metrics 服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("⚠️  Metrics 服务器关闭错误: %v\n", err)
	}

	// 取消上下文
	cancel()

	log.Println("✅ Push-Manager 已关闭")
}

// PushManagerConfig 配置
type PushManagerConfig struct {
	managerID   string
	grpcPort    int
	metricsPort int
	config      *config.Config
}

// loadPushManagerConfig 加载配置
func loadPushManagerConfig() *PushManagerConfig {
	cfg := config.LoadConfig()

	managerID := getEnv("MANAGER_ID", "push-manager-1")
	grpcPort := getEnvAsInt("GRPC_PORT", 50053)
	metricsPort := getEnvAsInt("METRICS_PORT", 9093)

	return &PushManagerConfig{
		managerID:   managerID,
		grpcPort:    grpcPort,
		metricsPort: metricsPort,
		config:      cfg,
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
