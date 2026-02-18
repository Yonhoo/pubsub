package main

import (
	"context"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("🔍 ETCD Resolver 深度调试")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")

	etcdEndpoints := []string{"etcd:2379"}

	// 创建 ETCD 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Fatalf("❌ ETCD 连接失败: %v", err)
	}
	defer cli.Close()

	// 测试 1: 验证 Endpoints Manager 能读取数据
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 1: Endpoints Manager")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	em, err := endpoints.NewManager(cli, "/controller-manager")
	if err != nil {
		log.Fatalf("❌ 创建 Manager 失败: %v", err)
	}

	ctx := context.Background()
	eps, err := em.List(ctx)
	if err != nil {
		log.Fatalf("❌ List 失败: %v", err)
	}

	log.Printf("✅ 找到 %d 个 Endpoints:", len(eps))
	for key, ep := range eps {
		log.Printf("   %s -> %s", key, ep.Addr)
	}
	log.Println("")

	time.Sleep(1 * time.Second)

	// 测试 2: 创建 Resolver Builder
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 2: Resolver Builder")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	builder, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Fatalf("❌ 创建 Builder 失败: %v", err)
	}

	log.Printf("✅ Builder 创建成功")
	log.Printf("   Scheme: %s", builder.Scheme())
	log.Println("")

	time.Sleep(1 * time.Second)

	// 测试 3: 使用 WithBlock 强制同步连接
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 3: gRPC 连接（使用 WithBlock 强制同步）")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	target := fmt.Sprintf("%s:///controller-manager/", builder.Scheme())
	log.Printf("🔗 Target: %s", target)
	log.Println("")

	log.Println("⏱️  使用 grpc.WithBlock() 创建连接（最多等待 15 秒）...")
	startTime := time.Now()

	conn, err := grpc.Dial(
		"etcd:///controller-manager",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
		grpc.WithBlock(),
	)

	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ 连接失败: %v", err)
		log.Printf("   耗时: %v", elapsed)
		log.Println("")

		log.Println("💡 可能原因:")
		log.Println("   1. gRPC Resolver 无法解析 ETCD 中的地址")
		log.Println("   2. Resolver 返回的地址格式不正确")
		log.Println("   3. 目标服务不可达")
		log.Println("")

		// 直接测试能否连接
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println("📋 测试 4: 直接连接（不使用 Resolver）")
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println("")

		directTarget := "controller:50051"
		log.Printf("🔗 直接连接: %s", directTarget)

		directCtx, directCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer directCancel()

		directConn, directErr := grpc.DialContext(directCtx, directTarget,
			grpc.WithBlock(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)

		if directErr != nil {
			log.Printf("❌ 直接连接也失败: %v", directErr)
			log.Println("")
			log.Println("🔴 这说明 Controller 服务本身不可达！")
		} else {
			defer directConn.Close()
			log.Printf("✅ 直接连接成功！状态: %v", directConn.GetState())
			log.Println("")
			log.Println("🔴 这说明 Controller 服务正常，但 ETCD Resolver 有问题！")
		}

		return
	}
	defer conn.Close()

	log.Printf("✅ 连接成功！耗时: %v", elapsed)
	log.Printf("   状态: %v", conn.GetState())
	log.Println("")

	log.Println("🎉 ETCD 服务发现工作正常！")
	log.Println("")

	// 测试 5: 观察连接状态
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 5: 观察连接状态变化")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	for i := 0; i < 10; i++ {
		state := conn.GetState()
		log.Printf("   [%d] 状态: %v", i, state)

		if state == connectivity.Ready {
			log.Println("")
			log.Println("✅ 连接保持 READY 状态")
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	log.Println("")
	log.Println("📝 结论:")
	log.Println("   如果测试通过，说明 ETCD 服务发现配置正确")
	log.Println("   Connect-Node 的 JoinRoom 超时可能是其他原因")
	log.Println("")
}
