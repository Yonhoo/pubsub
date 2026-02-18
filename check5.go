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
	gresolver "google.golang.org/grpc/resolver"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("🔍 ETCD Resolver 最终测试 - 根据官方文档")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")
	log.Println("📚 官方文档说明:")
	log.Println("   Target 格式: 直接使用服务名 (如 'my-service')")
	log.Println("   不需要 'etcd:///' 前缀！")
	log.Println("   注册 key: 'my-service/addr'")
	log.Println("   Manager target: 'my-service'")
	log.Println("   gRPC Dial target: 'my-service'")
	log.Println("")

	etcdAddr := "etcd:2379"

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddr},
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Fatalf("❌ ETCD 连接失败: %v", err)
	}
	defer cli.Close()

	ctx := context.Background()
	builder, _ := resolver.NewBuilder(cli)

	log.Printf("✅ Builder scheme: %s", builder.Scheme())
	log.Println("")

	// ═══════════════════════════════════════════════════════════════════════
	// 测试 1: 使用简单名称 (官方推荐格式)
	// ═══════════════════════════════════════════════════════════════════════
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 1: 简单名称 (官方推荐)")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	svcName1 := "test-svc-simple"

	em1, _ := endpoints.NewManager(cli, svcName1)
	lease1, _ := cli.Grant(ctx, 60)
	em1.AddEndpoint(ctx, svcName1+"/controller:50051",
		endpoints.Endpoint{Addr: "controller:50051"},
		clientv3.WithLease(lease1.ID))

	log.Printf("📝 注册: Manager target=%q, key=%q", svcName1, svcName1+"/controller:50051")

	eps1, _ := em1.List(ctx)
	log.Printf("✅ List 返回 %d 个 endpoints", len(eps1))
	for k, v := range eps1 {
		log.Printf("   %s -> %s", k, v.Addr)
	}
	log.Println("")

	// gRPC Dial 直接用服务名
	log.Printf("🔗 Dial target: %q", svcName1)
	testDial(builder, svcName1)

	cli.Delete(ctx, svcName1+"/", clientv3.WithPrefix())

	log.Println("")
	time.Sleep(1 * time.Second)

	// ═══════════════════════════════════════════════════════════════════════
	// 测试 2: 使用你的现有格式 /services/controller-manager
	// ═══════════════════════════════════════════════════════════════════════
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 2: 使用现有格式 /services/controller-manager")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	// 使用 ETCD 中已有的 controller-manager 注册数据
	log.Printf("🔗 Dial target: %q", "/services/controller-manager")
	testDial(builder, "/services/controller-manager")

	log.Println("")
	time.Sleep(1 * time.Second)

	// ═══════════════════════════════════════════════════════════════════════
	// 测试 3: 不带开头斜杠
	// ═══════════════════════════════════════════════════════════════════════
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 3: 不带开头斜杠 services/controller-manager")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	svcName3 := "services/controller-manager"

	em3, _ := endpoints.NewManager(cli, svcName3)
	lease3, _ := cli.Grant(ctx, 60)
	em3.AddEndpoint(ctx, svcName3+"/controller:50051",
		endpoints.Endpoint{Addr: "controller:50051"},
		clientv3.WithLease(lease3.ID))

	log.Printf("📝 注册: Manager target=%q, key=%q", svcName3, svcName3+"/controller:50051")

	eps3, _ := em3.List(ctx)
	log.Printf("✅ List 返回 %d 个 endpoints", len(eps3))
	for k, v := range eps3 {
		log.Printf("   %s -> %s", k, v.Addr)
	}
	log.Println("")

	log.Printf("🔗 Dial target: %q", svcName3)
	testDial(builder, svcName3)

	cli.Delete(ctx, svcName3+"/", clientv3.WithPrefix())

	log.Println("")
	time.Sleep(1 * time.Second)

	// ═══════════════════════════════════════════════════════════════════════
	// 测试 4: 使用 etcd:/// 前缀 + 简单名称
	// ═══════════════════════════════════════════════════════════════════════
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 测试 4: etcd:/// + 简单名称")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	svcName4 := "test-svc-scheme"

	em4, _ := endpoints.NewManager(cli, svcName4)
	lease4, _ := cli.Grant(ctx, 60)
	em4.AddEndpoint(ctx, svcName4+"/controller:50051",
		endpoints.Endpoint{Addr: "controller:50051"},
		clientv3.WithLease(lease4.ID))

	log.Printf("📝 注册: Manager target=%q", svcName4)
	log.Printf("🔗 Dial target: %q", "etcd:///"+svcName4)
	testDial(builder, "etcd:///"+svcName4)

	cli.Delete(ctx, svcName4+"/", clientv3.WithPrefix())

	log.Println("")

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("📊 测试完成")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")
}

func testDial(builder gresolver.Builder, target string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startTime := time.Now()

	conn, err := grpc.DialContext(ctx, target,
		grpc.WithBlock(),
		grpc.WithResolvers(builder),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)

	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("   ❌ 失败: %v (耗时: %v)", err, elapsed)
	} else {
		defer conn.Close()
		log.Printf("   ✅ 成功！状态: %v (耗时: %v)", conn.GetState(), elapsed)
		log.Println("")
		log.Println("   🎉🎉🎉 这个格式有效！")
	}
}
