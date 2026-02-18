package main

import (
	"context"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("🔍 完整的 ETCD 服务发现测试（注册 + 发现）")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")

	etcdAddr := "etcd:2379"
	serviceName := "test-grpc-service"
	serviceAddr := "controller:50051" // 使用已知可达的地址

	// 步骤 1: 连接 ETCD
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 1: 连接 ETCD")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddr},
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Fatalf("❌ ETCD 连接失败: %v", err)
	}
	defer cli.Close()

	log.Println("✅ ETCD 连接成功")
	log.Println("")

	time.Sleep(500 * time.Millisecond)

	// 步骤 2: 注册服务
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 2: 注册服务到 ETCD")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	ctx := context.Background()

	// 创建 Endpoints Manager
	em, err := endpoints.NewManager(cli, serviceName)
	if err != nil {
		log.Fatalf("❌ 创建 Manager 失败: %v", err)
	}

	// 创建租约
	lease, err := cli.Grant(ctx, 60)
	if err != nil {
		log.Fatalf("❌ 创建租约失败: %v", err)
	}

	// 注册端点
	endpointKey := serviceName + "/" + serviceAddr
	log.Printf("📝 注册: %s -> %s", endpointKey, serviceAddr)

	err = em.AddEndpoint(ctx, endpointKey,
		endpoints.Endpoint{Addr: serviceAddr},
		clientv3.WithLease(lease.ID))
	if err != nil {
		log.Fatalf("❌ 注册失败: %v", err)
	}

	log.Println("✅ 服务注册成功")
	log.Println("")

	time.Sleep(500 * time.Millisecond)

	// 步骤 3: 验证注册
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 3: 验证注册数据")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	eps, err := em.List(ctx)
	if err != nil {
		log.Fatalf("❌ List 失败: %v", err)
	}

	log.Printf("✅ 找到 %d 个 Endpoints:", len(eps))
	for key, ep := range eps {
		log.Printf("   %s -> %s", key, ep.Addr)
	}
	log.Println("")

	time.Sleep(500 * time.Millisecond)

	// 步骤 4: 创建 Resolver 并连接
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 4: 使用 ETCD Resolver 建立 gRPC 连接")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	// 创建 Resolver Builder
	builder, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Fatalf("❌ 创建 Builder 失败: %v", err)
	}

	log.Printf("✅ Resolver Builder 创建成功 (scheme: %s)", builder.Scheme())
	log.Println("")

	// 构建 target
	target := builder.Scheme() + "://" + serviceName
	log.Printf("🔗 Target: %s", target)
	log.Println("⏱️  尝试连接（最多 10 秒）...")
	log.Println("")

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dialCancel()

	startTime := time.Now()

	conn, err := grpc.DialContext(dialCtx, target,
		grpc.WithBlock(),
		grpc.WithResolvers(builder),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)

	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ 连接失败: %v", err)
		log.Printf("   耗时: %v", elapsed)
		log.Println("")

		log.Println("🔴 ETCD Resolver 无法工作！")
		log.Println("")
		log.Println("💡 可能原因:")
		log.Println("   1. Target 格式不正确")
		log.Println("   2. ETCD v3 naming API 版本不兼容")
		log.Println("   3. Resolver 实现有 bug")
		log.Println("")

		// 清理
		cli.Delete(ctx, serviceName+"/", clientv3.WithPrefix())
		return
	}
	defer conn.Close()

	log.Printf("✅ 连接成功！耗时: %v", elapsed)
	log.Printf("   状态: %v", conn.GetState())
	log.Println("")

	// 步骤 5: 观察连接状态
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 5: 观察连接状态")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	for i := 0; i < 5; i++ {
		state := conn.GetState()
		log.Printf("   [%d] 状态: %v", i, state)
		time.Sleep(500 * time.Millisecond)
	}

	log.Println("")
	log.Println("🎉 ETCD 服务发现测试成功！")
	log.Println("")
	log.Println("📝 这证明:")
	log.Println("   ✅ ETCD 注册正确")
	log.Println("   ✅ ETCD Resolver 工作正常")
	log.Println("   ✅ gRPC 连接能够建立")
	log.Println("")
	log.Println("💡 如果这个测试通过，说明你的 Controller 注册代码也应该能工作")
	log.Println("   需要检查的是 Connect-Node 中使用 Resolver 的方式")
	log.Println("")

	// 清理测试数据
	cli.Delete(ctx, serviceName+"/", clientv3.WithPrefix())
}
