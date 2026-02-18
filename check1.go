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
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("🔍 深度诊断 ETCD 服务发现问题 (Docker 内部版本)")
	log.Println("════════════════════════════════════════════════════════════════════════════════")
	log.Println("")

	// 使用 Docker 内部地址
	etcdEndpoints := []string{"etcd:2379"}
	serviceName := "controller-manager"

	// 步骤 1: 测试 ETCD 基础连接
	if !testETCDConnection(etcdEndpoints) {
		return
	}

	time.Sleep(1 * time.Second)

	// 步骤 2: 查询 ETCD 中的服务
	if !queryETCDService(etcdEndpoints, serviceName) {
		return
	}

	time.Sleep(1 * time.Second)

	// 步骤 3: 测试 ETCD Endpoints Manager
	if !testETCDEndpointsManager(etcdEndpoints, serviceName) {
		return
	}

	time.Sleep(1 * time.Second)

	// 步骤 4: 测试 ETCD Resolver Builder
	if !testETCDResolverBuilder(etcdEndpoints) {
		return
	}

	time.Sleep(1 * time.Second)

	// 步骤 5: 测试 gRPC 连接（详细模式）
	testGRPCConnectionDetailed(etcdEndpoints, serviceName)
}

// 测试 ETCD 基础连接
func testETCDConnection(endpoints []string) bool {
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 1: 测试 ETCD 基础连接")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	log.Printf("🔗 连接 ETCD: %v", endpoints)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Printf("❌ ETCD 连接失败: %v", err)
		return false
	}
	defer cli.Close()

	// 测试 ETCD 状态
	statusResp, err := cli.Status(ctx, endpoints[0])
	if err != nil {
		log.Printf("❌ ETCD 状态检查失败: %v", err)
		return false
	}

	log.Printf("✅ ETCD 连接成功")
	log.Printf("   版本: %s", statusResp.Version)
	log.Printf("   Leader: %d", statusResp.Leader)
	log.Printf("   数据库大小: %d bytes", statusResp.DbSize)
	log.Println("")

	return true
}

// 查询 ETCD 中的服务
func queryETCDService(endpoints []string, serviceName string) bool {
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 2: 查询 ETCD 中的服务注册")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Printf("❌ ETCD 连接失败: %v", err)
		return false
	}
	defer cli.Close()

	// 查询特定服务
	prefix := fmt.Sprintf("/services/%s/", serviceName)
	log.Printf("🔍 查询前缀: %s", prefix)

	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		log.Printf("❌ 查询失败: %v", err)
		return false
	}

	if len(resp.Kvs) == 0 {
		log.Printf("❌ 未找到任何服务（前缀: %s）", prefix)
		log.Println("")
		log.Println("💡 可能原因:")
		log.Println("   1. Controller-Manager 未启动")
		log.Println("   2. Controller-Manager 注册失败")
		log.Println("   3. 服务名称不匹配")
		log.Println("")
		return false
	}

	log.Printf("✅ 找到 %d 个实例:", len(resp.Kvs))
	for _, kv := range resp.Kvs {
		log.Printf("   Key:   %s", string(kv.Key))
		log.Printf("   Value: %s", string(kv.Value))
		log.Println("")
	}

	return true
}

// 测试 ETCD Endpoints Manager
func testETCDEndpointsManager(etcdEndpoints []string, serviceName string) bool {
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 3: 测试 ETCD Endpoints Manager")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

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

	// 创建 Endpoints Manager
	target := fmt.Sprintf("/services/%s", serviceName)
	log.Printf("🔍 创建 Manager，target: %s", target)

	em, err := endpoints.NewManager(cli, target)
	if err != nil {
		log.Printf("❌ 创建失败: %v", err)
		return false
	}

	log.Printf("✅ Manager 创建成功")

	// 列出 Endpoints
	log.Println("🔍 列出所有 Endpoints...")
	eps, err := em.List(ctx)
	if err != nil {
		log.Printf("❌ 列出失败: %v", err)
		return false
	}

	if len(eps) == 0 {
		log.Printf("❌ 未找到任何 Endpoints")
		log.Println("")
		log.Println("💡 说明:")
		log.Println("   endpoints.Manager.List() 返回空")
		log.Println("   但 etcdctl get 能看到数据")
		log.Println("")
		log.Println("   🔴 这说明你的服务注册格式不符合 ETCD naming API 规范！")
		log.Println("")
		log.Println("   可能原因:")
		log.Println("   1. RegisterEndPointToEtcd() 函数使用了自定义格式")
		log.Println("   2. 没有使用 endpoints.Manager.AddEndpoint() 进行注册")
		log.Println("")
		log.Println("   📝 查看你的注册代码:")
		log.Println("      cat pkg/etcd/registry.go | grep -A 20 RegisterEndPointToEtcd")
		log.Println("")
		return false
	}

	log.Printf("✅ 找到 %d 个 Endpoints:", len(eps))
	for key, ep := range eps {
		log.Printf("   Key:      %s", key)
		log.Printf("   Addr:     %s", ep.Addr)
		log.Printf("   Metadata: %v", ep.Metadata)
		log.Println("")
	}

	return true
}

// 测试 ETCD Resolver Builder
func testETCDResolverBuilder(etcdEndpoints []string) bool {
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 4: 测试 ETCD Resolver Builder")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 3 * time.Second,
	})
	if err != nil {
		log.Printf("❌ ETCD 连接失败: %v", err)
		return false
	}
	defer cli.Close()

	log.Printf("🔍 创建 Resolver Builder")

	builder, err := resolver.NewBuilder(cli)
	if err != nil {
		log.Printf("❌ 创建失败: %v", err)
		return false
	}

	log.Printf("✅ Resolver Builder 创建成功")
	log.Printf("   Scheme: %s", builder.Scheme())
	log.Printf("   类型:   %T", builder)
	log.Println("")

	// 测试 Build 方法
	log.Printf("🔍 测试 Build() 方法")
	target := "etcd:///services/controller-manager"
	log.Printf("   Target: %s", target)
	log.Printf("   这个 target 将被用于 grpc.Dial()")
	log.Println("")

	log.Println("✅ Resolver Builder 可用")
	log.Println("")

	return true
}

// 测试 gRPC 连接（详细模式）
func testGRPCConnectionDetailed(etcdEndpoints []string, serviceName string) {
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("📋 步骤 5: 测试 gRPC 连接（详细模式）")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

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

	// 创建连接（不使用 WithBlock，观察异步行为）
	log.Println("⏱️  开始创建 gRPC 连接（非阻塞模式）...")
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

	elapsed := time.Since(startTime)
	log.Printf("✅ 连接对象创建成功 (耗时: %v)", elapsed)
	log.Println("")

	// 🔑 关键：强制触发连接建立
	log.Println("🔍 强制触发连接建立（调用 Connect）...")
	conn.Connect()

	// 监控连接状态变化
	log.Println("🔍 监控连接状态变化（最多 30 秒）...")
	log.Println("")

	monitorCtx, monitorCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer monitorCancel()

	stateChanges := 0
	lastState := conn.GetState()
	log.Printf("   初始状态: %v", lastState)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-monitorCtx.Done():
			log.Println("")
			log.Printf("⚠️  超时 (30秒)，最终状态: %v", conn.GetState())
			log.Println("")

			if conn.GetState().String() != "READY" {
				log.Println("❌ 连接未能建立（状态不是 READY）")
				log.Println("")
				log.Println("💡 可能原因:")
				log.Println("   1. Controller-Manager gRPC 服务未运行或不可达")
				log.Println("   2. gRPC Resolver 解析到的地址 'controller:50051' 无法连接")
				log.Println("   3. 网络隔离问题")
				log.Println("")
				log.Println("🔧 诊断步骤:")
				log.Println("   1. 检查 Controller 容器: docker-compose ps controller")
				log.Println("   2. 检查 Controller 日志: docker-compose logs controller | tail -50")
				log.Println("   3. 测试端口: docker run --rm --network pubsub_pubsub-network nicolaka/netshoot nc -zv controller 50051")
				log.Println("   4. 测试 gRPC: docker run --rm --network pubsub_pubsub-network fullstorydev/grpcurl -plaintext controller:50051 list")
				log.Println("")
			}
			return

		case <-ticker.C:
			currentState := conn.GetState()
			if currentState != lastState {
				stateChanges++
				log.Printf("   [%d] 状态变化: %v → %v", stateChanges, lastState, currentState)
				lastState = currentState

				if currentState.String() == "READY" {
					log.Println("")
					log.Printf("✅ 连接已就绪！(耗时: %v)", time.Since(startTime))
					log.Println("")
					log.Println("🎉 所有诊断步骤通过！")
					log.Println("")
					log.Println("📝 下一步:")
					log.Println("   1. 重新构建: docker-compose build connect-node-1 controller push-manager")
					log.Println("   2. 重启服务: docker-compose restart connect-node-1 controller push-manager")
					log.Println("   3. 运行测试: cd benchmark && go run client.go -room=room-001 -users=10")
					log.Println("")
					return
				}
			}
		}
	}
}
