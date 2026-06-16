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
	"google.golang.org/grpc/reflection"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/database"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
)

func initLogger() {
	// 从环境变量获取日志文件路径，默认为 /app/logs/controller.log
	logFile := "/app/logs/controller.log"

	// 确保日志目录存在
	if err := os.MkdirAll("/app/logs", 0755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
		return
	}

	// 打开日志文件（追加模式）
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("打开日志文件失败: %v", err)
		return
	}

	// 同时输出到控制台和文件
	log.SetOutput(io.MultiWriter(os.Stdout, f))
}

func main() {
	// 初始化日志（输出到文件和控制台）
	initLogger()

	// 加载配置
	configFile := getEnv("CONFIG_FILE", "config.yaml")
	cfg := config.LoadConfigFromFile(configFile)

	// 命令行参数可以覆盖配置
	if len(os.Args) > 1 {
		cfg.Server.ID = os.Args[1]
	}
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &cfg.Server.Port)
	}

	log.Printf("启动 Controller Manager: %s (端口: %d)", cfg.Server.ID, cfg.Server.Port)

	// 1️⃣ 初始化 OpenTelemetry
	shutdown, err := tracing.InitTracer(cfg.Server.ID, tracing.ServiceNameController)
	if err != nil {
		log.Printf("OpenTelemetry 初始化失败: %v", err)
	} else {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				log.Printf("关闭 Tracer 失败: %v", err)
			}
		}()
	}

	// 2️⃣ 连接 MySQL 数据库
	dbConfig := &database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		DBName:   cfg.Database.DBName,
		Charset:  cfg.Database.Charset,
	}

	db, err := database.NewDatabase(dbConfig)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	// 3️⃣ 连接 Redis（用于缓存）
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Redis 连接失败（将跳过缓存）: %v", err)
		_ = redisClient.Close()
		redisClient = nil
	} else {
		defer func() { _ = redisClient.Close() }()
	}

	// 4️⃣ 创建 Metrics Collector
	metricsCollector, err := metrics.NewMetricsCollector(cfg.Server.ID, tracing.ServiceNameController)
	if err != nil {
		log.Printf("Metrics Collector 创建失败: %v", err)
	}

	// 5️⃣ 注册服务到 ETCD
	nodeAddr := os.Getenv("NODE_ADDR")
	if nodeAddr == "" {
		nodeAddr = "localhost"
	}
	serviceAddr := fmt.Sprintf("%s:%d", nodeAddr, cfg.Server.Port)

	go etcd.RegisterEndPointToEtcd(ctx, serviceAddr, "controller-manager", cfg.ETCD.Endpoints)

	// 等待一小段时间确保注册完成
	time.Sleep(1 * time.Second)

	// 6️⃣ 初始化 ETCD 服务发现（用于 GetUserNode 时把 nodeID 解析为 nodeAddress）
	connectNodeDiscovery, err := etcd.NewServiceDiscovery(cfg.ETCD.Endpoints, "connect-node")
	if err != nil {
		log.Printf("ETCD 服务发现初始化失败（GetUserNode 将退化返回 nodeID）: %v", err)
	} else {
		defer connectNodeDiscovery.Close()
	}

	// 7️⃣ 创建 Repository 和 Controller Server
	repo := database.NewRepository(db)
	controllerServer := NewControllerServer(cfg, repo, redisClient, connectNodeDiscovery, metricsCollector)

	// 启动空房间后台清理（DB + Redis 缓存）
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go controllerServer.RunRoomCleanup(cleanupCtx)

	// 8️⃣ 创建 gRPC Server（带 OpenTelemetry）
	grpcOpts := tracing.GetGRPCServerOptions()
	grpcServer := grpc.NewServer(grpcOpts...)

	controller.RegisterControllerServiceServer(grpcServer, controllerServer)

	// 启用 gRPC 反射（用于 grpcurl 等工具）
	reflection.Register(grpcServer)

	// 9️⃣ 启动 gRPC Server
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.Port))
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	pprofPort := 6062

	go func() {
		addr := fmt.Sprintf(":%d", pprofPort)
		if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
			log.Printf("pprof 服务错误: %v", err)
		}
	}()

	// 启动 Metrics HTTP 服务（/metrics 供 Prometheus 抓取）
	metricsPort := 9090
	if metricsCollector != nil {
		go func() {
			addr := fmt.Sprintf(":%d", metricsPort)
			mux := http.NewServeMux()
			mux.Handle("/metrics", metricsCollector.Handler())
			if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
				log.Printf("Metrics 服务错误: %v", err)
			}
		}()
	}

	log.Printf("Controller Manager 运行中 ID=%s gRPC=:%d MySQL=%s:%d/%s Redis=%s",
		cfg.Server.ID, cfg.Server.Port, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName, cfg.Redis.Addr)

	// 启动 gRPC Server
	go func() {
		if err := grpcServer.Serve(listen); err != nil {
			log.Fatalf("gRPC 服务启动失败: %v", err)
		}
	}()

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 优雅关闭
	log.Printf("正在关闭服务...")

	// 先停掉清理 goroutine，避免 shutdown 期间还在跑 DB 事务
	cleanupCancel()

	// 兜底超时：避免 GracefulStop 因 in-flight RPC 卡死无法退出
	graceDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(graceDone)
	}()
	select {
	case <-graceDone:
	case <-time.After(10 * time.Second):
		log.Printf("GracefulStop 超时，强制停止")
		grpcServer.Stop()
	}
	log.Printf("服务已关闭")
}

// getEnv 获取环境变量（带默认值）
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// printStats 定期打印统计信息
