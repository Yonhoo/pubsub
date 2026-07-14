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

	_ "net/http/pprof"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
	"google.golang.org/grpc"
)

func main() {
	// 加载配置
	cfg := loadPushManagerConfig()
	role := normalizePushManagerRole(cfg.role)
	needsIngress := role == pushManagerRoleIngress || role == pushManagerRoleAll
	needsFanout := role == pushManagerRoleJob || role == pushManagerRoleAll

	log.Printf("启动 Push-Manager: %s role=%s (端口: %d)", cfg.managerID, role, cfg.grpcPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1️⃣ 初始化 OpenTelemetry
	tracingShutdown, err := tracing.InitTracer(cfg.managerID, "push-manager")
	if err != nil {
		log.Printf("OpenTelemetry 初始化失败: %v", err)
	} else {
		defer tracingShutdown(ctx)
	}

	// 2️⃣ 初始化 Metrics
	metricsCollector, err := metrics.NewMetricsCollector(cfg.managerID, "push-manager")
	if err != nil {
		log.Fatalf("Metrics 初始化失败: %v", err)
	}

	// 3️⃣ 注册入口服务到 ETCD。job 角色不提供 Broadcast gRPC，不能注册到 push-manager 服务名。
	if needsIngress {
		go etcd.RegisterEndPointToEtcd(ctx, fmt.Sprintf("localhost:%d", cfg.grpcPort), "push-manager", cfg.config.ETCD.Endpoints)
		// 等待一小段时间确保注册完成
		time.Sleep(1 * time.Second)
	}

	// 4️⃣ 初始化 ETCD 服务发现（job/all 角色需要发现 connect-node）
	var etcdDiscovery *etcd.ServiceDiscovery
	if needsFanout {
		etcdDiscovery, err = etcd.NewServiceDiscovery(cfg.config.ETCD.Endpoints, "connect-node")
		if err != nil {
			log.Fatalf("ETCD 初始化失败: %v", err)
		}
		defer etcdDiscovery.Close()
	}

	// 5️⃣ 创建 Push-Manager 服务器
	pushManager := NewPushManagerServer(
		cfg.managerID,
		cfg.config,
		etcdDiscovery,
		cfg.config.ETCD.Endpoints,
		metricsCollector,
	)
	var roomBatchAggregator *roomBatchAggregatorManager
	if needsFanout {
		roomBatchCfg := loadRoomBatchAggregatorConfig()
		roomBatchAggregator = newRoomBatchAggregatorManager(pushManager, roomBatchCfg)
		if roomBatchAggregator != nil {
			pushManager.SetRoomBatchAggregator(roomBatchAggregator)
			defer roomBatchAggregator.Stop()
		}
		log.Printf("[RoomBatchAggregator] %s", describeRoomBatchAggregatorConfig(roomBatchCfg))
	} else {
		log.Printf("[RoomBatchAggregator] disabled role=%s", role)
	}

	// 6️⃣ 启动 Connect-Node 发现与监听
	if needsFanout {
		pushManager.WatchConnectNodes(ctx)
		// 等待发现节点
		time.Sleep(1 * time.Second)
	}

	// 6.5: Optional Kafka bridge. ingress writes to Kafka; job consumes and fans out to connect-node.
	kafkaCfg := loadKafkaBridgeConfig()
	kafkaCfg.ProduceEnabled = needsIngress
	kafkaCfg.ConsumeEnabled = needsFanout
	kafkaBridge, err := NewKafkaBridge(kafkaCfg, pushManager)
	if err != nil {
		log.Fatalf("Kafka init failed: %v", err)
	}
	if kafkaBridge != nil {
		pushManager.SetKafkaBridge(kafkaBridge)
		if err := kafkaBridge.Start(ctx); err != nil {
			log.Fatalf("Kafka consumer start failed: %v", err)
		}
		defer kafkaBridge.Close()
	}

	// 7️⃣ 创建 gRPC 服务器
	grpcServer := grpc.NewServer()

	if needsIngress {
		broadcast.RegisterPushServerServer(grpcServer, pushManager)
	} else {
		log.Printf("Broadcast gRPC disabled role=%s", role)
	}

	// 8️⃣ 启动 gRPC Server
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.grpcPort))
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	// 9️⃣ 启动 Metrics HTTP 服务器
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.metricsPort),
		Handler: metricsCollector.Handler(),
	}

	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics 服务器错误: %v", err)
		}
	}()

	pprofPort := 6061

	go func() {
		addr := fmt.Sprintf(":%d", pprofPort)
		if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
			log.Printf("pprof 服务错误: %v", err)
		}
	}()

	log.Printf("Push-Manager 运行中 ID=%s role=%s gRPC=:%d metrics=:%d", cfg.managerID, role, cfg.grpcPort, cfg.metricsPort)

	// 定期打印队列满丢弃数（便于分析丢包来源）
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if n := pushManager.GetQueueFullDropCount(); n > 0 {
				log.Printf("Push-Manager 队列满丢弃: %d", n)
			}
		}
	}()

	// 启动 gRPC Server
	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			log.Fatalf("gRPC 服务启动失败: %v", err)
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("收到退出信号，开始优雅关闭...")

	// 关闭 gRPC 服务器
	grpcServer.GracefulStop()

	// 关闭 Metrics 服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Metrics 服务器关闭错误: %v", err)
	}

	// 取消上下文
	cancel()
	if roomBatchAggregator != nil {
		roomBatchAggregator.Stop()
	}

	// 等待 watch goroutine 退出（确保 cleanupAllClients 完成）
	if needsFanout {
		pushManager.watchWG.Wait()
	}

	log.Printf("Push-Manager 已关闭")
}

// PushManagerConfig 配置
type PushManagerConfig struct {
	managerID   string
	grpcPort    int
	metricsPort int
	role        string
	config      *config.Config
}

const (
	pushManagerRoleAll     = "all"
	pushManagerRoleIngress = "ingress"
	pushManagerRoleJob     = "job"
)

// loadPushManagerConfig 加载配置
func loadPushManagerConfig() *PushManagerConfig {
	configFile := getEnv("CONFIG_FILE", "config.yaml")
	cfg := config.LoadConfigFromFile(configFile)

	managerID := getEnv("MANAGER_ID", "push-manager-1")
	grpcPort := getEnvAsInt("GRPC_PORT", 50053)
	metricsPort := getEnvAsInt("METRICS_PORT", 9093)
	role := getEnv("PUSH_MANAGER_ROLE", pushManagerRoleAll)

	return &PushManagerConfig{
		managerID:   managerID,
		grpcPort:    grpcPort,
		metricsPort: metricsPort,
		role:        role,
		config:      cfg,
	}
}

func normalizePushManagerRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case pushManagerRoleIngress:
		return pushManagerRoleIngress
	case pushManagerRoleJob:
		return pushManagerRoleJob
	default:
		return pushManagerRoleAll
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

func loadKafkaBridgeConfig() KafkaBridgeConfig {
	enabled := getEnv("PUSH_MANAGER_KAFKA_ENABLED", "") == "1" || getEnv("PUSH_MANAGER_KAFKA_ENABLED", "") == "true"
	brokers := getEnv("PUSH_MANAGER_KAFKA_BROKERS", "kafka:9092")
	parts := getEnvAsInt("PUSH_MANAGER_KAFKA_PARTITIONS", 1)
	return KafkaBridgeConfig{
		Enabled:    enabled,
		Brokers:    splitCSV(brokers),
		Topic:      getEnv("PUSH_MANAGER_KAFKA_TOPIC", "pubsub-broadcast-topic"),
		Partitions: int32(parts),
	}
}

func loadRoomBatchAggregatorConfig() roomBatchAggregatorConfig {
	enabledValue := getEnv("PUSH_MANAGER_ROOM_BATCH_ENABLED", "1")
	return roomBatchAggregatorConfig{
		Enabled:       enabledValue == "1" || strings.EqualFold(enabledValue, "true"),
		BatchSize:     getEnvAsInt("PUSH_MANAGER_ROOM_BATCH_SIZE", 20),
		FlushInterval: getEnvAsDuration("PUSH_MANAGER_ROOM_BATCH_FLUSH_INTERVAL", 500*time.Millisecond),
		QueueSize:     getEnvAsInt("PUSH_MANAGER_ROOM_BATCH_QUEUE_SIZE", 4096),
	}
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			return d
		}
	}
	return defaultValue
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
