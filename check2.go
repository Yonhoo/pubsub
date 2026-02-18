package main

import (
	"context"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("🔍 gRPC 连接测试 - 使用实际 RPC 调用触发 Resolver")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")

	etcdEndpoints := []string{"etcd:2379"}
	serviceName := "controller-manager"

	// 步骤 1: 验证 ETCD 和服务注册
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 1: 快速验证 ETCD 和服务注册")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !quickCheck(etcdEndpoints, serviceName) {
		return
	}

	time.Sleep(1 * time.Second)

	// 步骤 2: 测试 gRPC 连接（使用实际 RPC 调用）
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 2: 测试 gRPC 连接（使用实际 RPC 调用）")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	testGRPCWithRPC(etcdEndpoints, serviceName)
}

func quickCheck(etcdEndpoints []string, serviceName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Printf("❌ ETCD 连接失败: %v", err)
		return false
	}
	defer cli.Close()

	// 检查服务注册
	prefix := fmt.Sprintf("/services/%s/", serviceName)
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		log.Printf("❌ 查询失败: %v", err)
		return false
	}

	if len(resp.Kvs) == 0 {
		log.Printf("❌ 未找到服务: %s", prefix)
		return false
	}

	log.Printf("✅ 找到服务: %s (%d 个实例)", serviceName, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		log.Printf("   %s", string(kv.Value))
	}
	log.Println("")

	return true
}

func testGRPCWithRPC(etcdEndpoints []string, serviceName string) {
	// 创建 ETCD 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Printf("❌ ETCD 连接失败: %v", err)
		return
	}
	defer cli.Close()

	// 创建 Resolver Builder
	builder, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Printf("❌ 创建 Resolver Builder 失败: %v", err)
		return
	}

	// 构建 Target
	target := fmt.Sprintf("%s:///services/%s", builder.Scheme(), serviceName)
	log.Printf("🔗 Target: %s", target)
	log.Println("")

	// 创建 gRPC 连接
	log.Println("⏱️  创建 gRPC 连接...")
	startTime := time.Now()

	conn, err := grpc.Dial(target,
		grpc.WithResolvers(builder),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{
			"loadBalancingPolicy":"round_robin",
			"healthCheckConfig": {
				"serviceName": "controller"
			}
		}`),
	)
	if err != nil {
		log.Printf("❌ 创建连接失败: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("✅ 连接对象创建成功 (耗时: %v)", time.Since(startTime))
	log.Printf("   初始状态: %v", conn.GetState())
	log.Println("")

	// 🔑 关键：使用 WaitForReady 的实际 RPC 调用
	log.Println("🔑 发起实际 RPC 调用（这会触发 Resolver 和连接建立）...")
	log.Println("   使用 grpc.WaitForReady(true) 等待连接就绪")
	log.Println("")

	// 创建一个简单的调用上下文（10秒超时）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 调用一个简单的方法（例如 Health Check 或 List Services）
	// 这里我们直接调用一个已知存在的方法
	callStartTime := time.Now()

	// 使用 reflection API 调用（所有 gRPC 服务都支持）
	err = conn.Invoke(ctx, "/grpc.health.v1.Health/Check", nil, nil,
		grpc.WaitForReady(true))

	callElapsed := time.Since(callStartTime)

	if err != nil {
		// 即使调用失败，连接状态也会改变
		log.Printf("⚠️  RPC 调用返回错误（这是预期的）: %v", err)
		log.Printf("   耗时: %v", callElapsed)
	} else {
		log.Printf("✅ RPC 调用成功！耗时: %v", callElapsed)
	}

	log.Println("")

	// 检查连接状态
	finalState := conn.GetState()
	log.Printf("📊 最终连接状态: %v", finalState)
	log.Println("")

	if finalState.String() == "READY" {
		log.Println("🎉 连接成功建立！")
		log.Println("")
		log.Println("✅ 这证明:")
		log.Println("   1. ETCD Resolver 工作正常")
		log.Println("   2. 服务发现工作正常")
		log.Println("   3. gRPC 连接可以建立")
		log.Println("")
		log.Println("📝 结论:")
		log.Println("   你的 Connect-Node 代码应该能正常工作！")
		log.Println("   JoinRoom 超时可能是其他原因（如业务逻辑、数据库等）")
		log.Println("")
	} else if finalState.String() == "TRANSIENT_FAILURE" || finalState.String() == "CONNECTING" {
		log.Println("⚠️  连接建立失败或正在尝试")
		log.Println("")
		log.Println("💡 可能原因:")
		log.Println("   1. Resolver 解析到的地址不可达")
		log.Println("   2. gRPC 服务未运行")
		log.Println("   3. 网络问题")
		log.Println("")
	} else {
		log.Println("❌ 连接状态异常")
		log.Println("")
		log.Println("💡 这说明:")
		log.Println("   gRPC 的懒加载机制导致连接未被触发")
		log.Println("   需要在实际代码中使用 WaitForReady 选项")
		log.Println("")
	}

	// 额外测试：等待一段时间，观察状态变化
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 3: 观察连接状态变化（5 秒）")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("")

	lastState := conn.GetState()
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		currentState := conn.GetState()
		if currentState != lastState {
			log.Printf("   状态变化: %v → %v", lastState, currentState)
			lastState = currentState
		}
	}

	log.Println("")
	log.Printf("📊 5秒后状态: %v", conn.GetState())
	log.Println("")
}
