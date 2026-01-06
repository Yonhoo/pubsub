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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/database"
	"github.com/livekit/psrpc/examples/pubsub/pkg/etcd"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
	"github.com/livekit/psrpc/examples/pubsub/pkg/tracing"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 命令行参数可以覆盖配置
	if len(os.Args) > 1 {
		cfg.Server.ID = os.Args[1]
	}
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &cfg.Server.Port)
	}

	log.Println(strings.Repeat("=", 80))
	log.Printf("🚀 启动 Controller Manager: %s (端口: %d)\n", cfg.Server.ID, cfg.Server.Port)
	log.Println(strings.Repeat("=", 80))
	log.Println()

	// 1️⃣ 初始化 OpenTelemetry
	log.Println("🔭 初始化 OpenTelemetry...")
	shutdown, err := tracing.InitTracer(cfg.Server.ID, tracing.ServiceNameController)
	if err != nil {
		log.Printf("⚠️  OpenTelemetry 初始化失败: %v\n", err)
	} else {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				log.Printf("⚠️  关闭 Tracer 失败: %v\n", err)
			}
		}()
		log.Println("✅ OpenTelemetry 初始化成功")
	}
	log.Println()

	// 2️⃣ 连接 MySQL 数据库
	log.Println("🗄️  连接到 MySQL...")
	dbConfig := &database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		DBName:   cfg.Database.DBName,
		Charset:  cfg.Database.Charset,
	}

	// 连接数据库
	db, err := database.NewDatabase(dbConfig)
	if err != nil {
		log.Fatalf("❌ 连接数据库失败: %v\n", err)
	}
	log.Println("✅ MySQL 连接成功")

	// 3️⃣ 连接 Redis（用于缓存）
	log.Println("📡 连接到 Redis...")
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  Redis 连接失败（将跳过缓存）: %v\n", err)
		redisClient = nil
	} else {
		log.Println("✅ Redis 连接成功")
	}
	log.Println()

	// 4️⃣ 创建 Metrics Collector
	log.Println("📊 创建 Metrics Collector...")
	metricsCollector, err := metrics.NewMetricsCollector(cfg.Server.ID, tracing.ServiceNameController)

	if err != nil {
		log.Printf("⚠️  Metrics Collector 创建失败: %v\n", err)
	}
	log.Println("✅ Metrics Collector 创建成功")
	log.Println()

	// 5️⃣ 注册服务到 ETCD
	log.Println("📝 注册服务到 ETCD...")
	
	// 构建服务注册地址
	nodeAddr := os.Getenv("NODE_ADDR")
	if nodeAddr == "" {
		nodeAddr = "localhost"
	}
	serviceAddr := fmt.Sprintf("%s:%d", nodeAddr, cfg.Server.Port)
	
	go etcd.RegisterEndPointToEtcd(ctx, serviceAddr, "/services/controller-manager", cfg.ETCD.Endpoints)
	
	// 等待一小段时间确保注册完成
	time.Sleep(1 * time.Second)
	log.Printf("✅ 服务已注册到 ETCD: %s", serviceAddr)
	log.Println()

	// 6️⃣ 创建 Push-Manager 客户端
	log.Println("🔗 创建 Push-Manager 客户端...")
	pushClient := newPushClient(cfg.RpcConfig, cfg.ETCD.Endpoints)
	log.Println("✅ Push-Manager 客户端创建成功")
	log.Println()

	// 7️⃣ 创建 Repository 和 Controller Server
	log.Println("🏗️  创建 Controller Server...")
	repo := database.NewRepository(db)
	controllerServer := NewControllerServer(cfg, repo, redisClient, &pushClient, metricsCollector)
	log.Println("✅ Controller Server 创建成功")
	log.Println()

	// 8️⃣ 创建 gRPC Server（带 OpenTelemetry）
	log.Println("🔧 创建 gRPC Server...")
	grpcOpts := tracing.GetGRPCServerOptions()
	grpcServer := grpc.NewServer(grpcOpts...)

	controller.RegisterControllerServiceServer(grpcServer, controllerServer)

	// 9️⃣ 启动 gRPC Server
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.Port))
	if err != nil {
		log.Fatalf("❌ 监听失败: %v\n", err)
	}

	log.Println(strings.Repeat("=", 80))
	log.Println("✅ Controller Manager 运行中")
	log.Println(strings.Repeat("=", 80))
	log.Println()
	log.Println("📋 服务信息:")
	log.Printf("  - Controller ID: %s\n", cfg.Server.ID)
	log.Printf("  - gRPC 端口: %d\n", cfg.Server.Port)
	log.Printf("  - MySQL: %s:%d/%s\n", cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)
	log.Printf("  - Redis: %s (缓存)\n", cfg.Redis.Addr)
	log.Printf("  - ETCD: %v\n", cfg.ETCD.Endpoints)
	log.Printf("  - OpenTelemetry: enabled\n")
	log.Printf("  - Metrics: enabled\n")
	log.Println()
	log.Println("⚙️  房间配置:")
	log.Printf("  - 默认最大用户数: %d\n", cfg.Room.DefaultMaxUsers)
	log.Printf("  - 缓存 TTL: %v\n", cfg.Room.CacheTTL)
	log.Println()
	log.Println("🔌 gRPC 方法:")
	log.Println("  - NotifyUserOnline: Connect-Node 通知用户上线")
	log.Println("  - NotifyUserOffline: Connect-Node 通知用户下线")
	log.Println("  - JoinRoom: 用户加入房间")
	log.Println("  - LeaveRoom: 用户离开房间")
	log.Println("  - GetRoomInfo: 获取房间信息")
	log.Println("  - GetRoomStats: 获取房间统计")
	log.Println("  - SelectConnectNode: 选择节点（负载均衡）")
	log.Println()
	log.Println("💡 使用示例:")
	log.Println("  grpcurl -plaintext localhost:50051 list")
	log.Println("  grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats")
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
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 优雅关闭
	log.Println("\n🛑 正在关闭服务...")
	grpcServer.GracefulStop()
	log.Println("👋 服务已关闭")
}

func newPushClient(c *config.RpcConfig, etcdEndpoints []string) (controllerClient push.CometClient) {

	log.Printf("🔍 连接 ETCD: %v", etcdEndpoints)
	resolverBuilder, err := etcd.GetETCDResolverBuilder(etcdEndpoints)
	if err != nil {
		log.Printf("❌ 获取 ETCD Resolver 失败: %v", err)
		panic(err)
	}

	log.Printf("🔗 通过 ETCD 连接 Push-Manager（非阻塞模式）...")

	// etcd v3 naming resolver: 使用 resolver 的 scheme
	target := fmt.Sprintf("%s:///services/push-manager", resolverBuilder.Scheme())
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

	log.Printf("✅ Push-Manager 客户端已创建（将在后台建立连接）")

	return push.NewCometClient(conn)

}

// printStats 定期打印统计信息
