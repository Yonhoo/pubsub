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

package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"

	eclient "go.etcd.io/etcd/client/v3"
	eresolver "go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc/resolver"
)

const (
	// 服务前缀
	ServicePrefix = "/services"

	// TTL
	DefaultTTL = 10 * time.Second

	// etcd 服务器的地址
	EtcdAddr = "http://localhost:2379"
)

// EventType 端点事件类型
type EventType int

const (
	EventAdd    EventType = iota // 节点上线
	EventDelete                  // 节点下线
)

// EndpointEvent 端点变化事件
type EndpointEvent struct {
	Type EventType // 事件类型：Add/Delete
	Addr string    // 端点地址
	Key  string    // ETCD key
}

// ServiceRegistry ETCD 服务注册
type ServiceRegistry struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// ServiceDiscovery ETCD 服务发现（事件驱动）
type ServiceDiscovery struct {
	client      *eclient.Client
	serviceName string
	ctx         context.Context
	cancel      context.CancelFunc
	endpointsMu sync.RWMutex
	endpoints   map[string]string // key -> Addr

	// 事件通道：Push-Manager 从此通道接收节点变化事件
	eventChan chan EndpointEvent
}

// NewServiceDiscovery 创建服务发现
func NewServiceDiscovery(etcdEndpoints []string, serviceName string) (*ServiceDiscovery, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := eclient.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	}

	client, err := eclient.New(cfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建 ETCD 客户端失败: %w", err)
	}

	sd := &ServiceDiscovery{
		client:      client,
		serviceName: serviceName,
		ctx:         ctx,
		cancel:      cancel,
		endpoints:   make(map[string]string),
		eventChan:   make(chan EndpointEvent, 100), // 100 缓冲
	}

	// 初始化获取现有的端点
	sd.refreshEndpoints()

	// 🔥 启动事件监听协程（而不是定期轮询）
	go sd.watchEndpointEvents()

	return sd, nil
}

// GetEndpoints 获取所有可用的服务端点地址
func (sd *ServiceDiscovery) GetEndpoints() ([]string, error) {
	sd.endpointsMu.RLock()
	defer sd.endpointsMu.RUnlock()

	var addresses []string
	for _, addr := range sd.endpoints {
		addresses = append(addresses, addr)
	}

	if len(addresses) == 0 {
		log.Printf("⚠️  [ServiceDiscovery] 未找到任何 %s 实例\n", sd.serviceName)
	}

	return addresses, nil
}

// GetEventChan 返回事件通道供外部监听
func (sd *ServiceDiscovery) GetEventChan() <-chan EndpointEvent {
	return sd.eventChan
}

// refreshEndpoints 刷新端点列表（初始化使用）
func (sd *ServiceDiscovery) refreshEndpoints() {
	ctx, cancel := context.WithTimeout(sd.ctx, 5*time.Second)
	defer cancel()

	em, err := endpoints.NewManager(sd.client, sd.serviceName)
	if err != nil {
		log.Printf("❌ [ServiceDiscovery] 创建 endpoints manager 失败: %v\n", err)
		return
	}

	eps, err := em.List(ctx)
	if err != nil {
		log.Printf("⚠️  [ServiceDiscovery] 获取端点列表失败: %v\n", err)
		return
	}

	sd.endpointsMu.Lock()
	defer sd.endpointsMu.Unlock()

	// 清空旧的端点
	sd.endpoints = make(map[string]string)

	// 更新新的端点
	for key, ep := range eps {
		sd.endpoints[key] = ep.Addr
		log.Printf("✅ [ServiceDiscovery] 初始化发现 %s 实例: %s -> %s\n", sd.serviceName, key, ep.Addr)
	}
}

// watchEndpointEvents 🔥 监听 ETCD 端点变化事件（事件驱动）
func (sd *ServiceDiscovery) watchEndpointEvents() {
	prefix := fmt.Sprintf("%s/", sd.serviceName)
	log.Printf("👀 [ServiceDiscovery] 开始监听 ETCD 事件: %s\n", prefix)

	// 使用 ETCD Watch API 监听前缀下的所有变化
	watchChan := sd.client.Watch(sd.ctx, prefix, clientv3.WithPrefix())

	for {
		select {
		case <-sd.ctx.Done():
			log.Printf("⚠️  [ServiceDiscovery] 停止监听 ETCD 事件: %s\n", prefix)
			close(sd.eventChan)
			return

		case wresp := <-watchChan:
			if wresp.Err() != nil {
				log.Printf("❌ [ServiceDiscovery] Watch 错误: %v\n", wresp.Err())
				// 重新连接
				continue
			}

			// 处理每个事件
			for _, event := range wresp.Events {
				key := string(event.Kv.Key)
				value := string(event.Kv.Value)

				log.Printf("📡 [ServiceDiscovery] 收到 ETCD 事件: Type=%s Key=%s Value=%s\n",
					event.Type.String(), key, value)

				switch event.Type {
				case clientv3.EventTypePut:
					// 节点上线或更新
					sd.handleEndpointAdd(key, value)

				case clientv3.EventTypeDelete:
					// 节点下线
					sd.handleEndpointDelete(key, value)
				}
			}
		}
	}
}

// handleEndpointAdd 处理节点上线事件
func (sd *ServiceDiscovery) handleEndpointAdd(key string, value string) {
	// 解析 JSON 以提取地址
	var endpoint struct {
		Op       int
		Addr     string
		Metadata interface{}
	}

	if err := json.Unmarshal([]byte(value), &endpoint); err != nil {
		log.Printf("❌ [ServiceDiscovery] 解析端点 JSON 失败: %v, value=%s\n", err, value)
		return
	}

	addr := endpoint.Addr

	sd.endpointsMu.Lock()
	isNew := sd.endpoints[key] != addr
	sd.endpoints[key] = addr
	sd.endpointsMu.Unlock()

	if isNew {
		log.Printf("📍 [ServiceDiscovery] 节点上线: %s\n", addr)
		// 发送上线事件
		select {
		case sd.eventChan <- EndpointEvent{
			Type: EventAdd,
			Addr: addr,
			Key:  key,
		}:
		case <-sd.ctx.Done():
			return
		}
	}
}

// handleEndpointDelete 处理节点下线事件
func (sd *ServiceDiscovery) handleEndpointDelete(key string, addr string) {
	sd.endpointsMu.Lock()
	delete(sd.endpoints, key)
	sd.endpointsMu.Unlock()

	log.Printf("📴 [ServiceDiscovery] 节点下线: %s\n", addr)
	// 发送下线事件
	select {
	case sd.eventChan <- EndpointEvent{
		Type: EventDelete,
		Addr: addr,
		Key:  key,
	}:
	case <-sd.ctx.Done():
		return
	}
}

// Close 关闭服务发现
func (sd *ServiceDiscovery) Close() {
	sd.cancel()
	if sd.client != nil {
		sd.client.Close()
	}
	log.Printf("✅ [ServiceDiscovery] 已关闭\n")
}

func RegisterEndPointToEtcd(ctx context.Context, serverAddr, serverName string, etcdEndpoints []string) {
	if len(etcdEndpoints) == 0 {
		// 从环境变量或配置获取 ETCD 地址
		etcdEndpoints = []string{"localhost:2379"} // 本地环境默认
		if endpoints := getETCDEndpoints(); len(endpoints) > 0 {
			etcdEndpoints = endpoints
		}
	}

	log.Printf("🔍 [ETCD] 注册服务，连接地址: %v", etcdEndpoints)

	// 创建 etcd 客户端
	cfg := eclient.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	}

	etcdClient, err := eclient.New(cfg)
	if err != nil {
		log.Printf("❌ [RegisterEndPoint] 创建 ETCD 客户端失败: %v\n", err)
		return
	}
	defer etcdClient.Close()

	etcdManager, err := endpoints.NewManager(etcdClient, serverName)
	if err != nil {
		log.Printf("❌ [RegisterEndPoint] 创建 endpoints manager 失败: %v\n", err)
		return
	}

	// 创建一个租约，每隔 10s 需要向 etcd 汇报一次心跳，证明当前节点仍然存活
	var ttl int64 = 10
	lease, err := etcdClient.Grant(ctx, ttl)
	if err != nil {
		log.Printf("❌ [RegisterEndPoint] 创建租约失败: %v\n", err)
		return
	}

	// 添加注册节点到 etcd 中，并且携带上租约 id
	// 🔑 ETCD endpoints API 要求：
	// - Manager target 不能以 / 开头: 例如 "controller-manager"
	// - Endpoint key 必须以 target 为前缀: "controller-manager/controller:50051"
	// - gRPC Dial target 使用 "etcd:///controller-manager" 格式
	endpointKey := fmt.Sprintf("%s/%s", serverName, serverAddr)
	err = etcdManager.AddEndpoint(ctx, endpointKey, endpoints.Endpoint{Addr: serverAddr}, eclient.WithLease(lease.ID))
	if err != nil {
		log.Printf("❌ [RegisterEndPoint] 注册端点失败: %v\n", err)
		return
	}

	log.Printf("✅ [RegisterEndPoint] 成功注册: %s -> %s\n", endpointKey, serverAddr)

	// 每隔 5 s 进行一次续约；若 lease 不存在（如 etcd 重启），则重新注册
	for {
		select {
		case <-time.After(5 * time.Second):
			_, err := etcdClient.KeepAliveOnce(ctx, lease.ID)
			if err != nil {
				if strings.Contains(err.Error(), "lease not found") {
					// etcd 重启或 lease 过期，重新注册
					newLease, errGrant := etcdClient.Grant(ctx, ttl)
					if errGrant != nil {
						log.Printf("⚠️  [RegisterEndPoint] 重新注册失败(Grant): %v\n", errGrant)
						continue
					}
					errAdd := etcdManager.AddEndpoint(ctx, endpointKey, endpoints.Endpoint{Addr: serverAddr}, eclient.WithLease(newLease.ID))
					if errAdd != nil {
						log.Printf("⚠️  [RegisterEndPoint] 重新注册失败(AddEndpoint): %v\n", errAdd)
						continue
					}
					lease = newLease
					log.Printf("✅ [RegisterEndPoint] 续约失败后已重新注册: %s -> %s\n", endpointKey, serverAddr)
				} else {
					log.Printf("⚠️  [RegisterEndPoint] 续约失败: %v\n", err)
				}
			}
		case <-ctx.Done():
			log.Printf("🛑 [RegisterEndPoint] 停止注册: %s\n", endpointKey)
			return
		}
	}
}

// getETCDEndpoints 从环境变量获取 ETCD 地址
func getETCDEndpoints() []string {
	// 这里可以从环境变量或全局配置读取
	// 暂时返回空，让调用者使用默认值
	return nil
}

// GetETCDResolverBuilder 获取 gRPC resolver builder (用于 gRPC 客户端负载均衡)
func GetETCDResolverBuilder(etcdEndpoints []string) (resolver.Builder, error) {
	if len(etcdEndpoints) == 0 {
		// 从环境变量或配置获取 ETCD 地址
		etcdEndpoints = []string{"localhost:2379"} // 本地环境默认
		if endpoints := getETCDEndpoints(); len(endpoints) > 0 {
			etcdEndpoints = endpoints
		}
	}

	log.Printf("🔍 [ETCD] 创建 Resolver Builder，连接地址: %v", etcdEndpoints)

	cfg := eclient.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	}

	etcdClient, err := eclient.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ETCD 客户端失败: %w", err)
	}

	// etcd v3 resolver: 使用 endpoints naming
	// gRPC Dial target 格式: "etcd:///<service-name>" (不带开头斜杠)
	// 例如: grpc.Dial("etcd:///controller-manager", grpc.WithResolvers(builder))
	builder, err := eresolver.NewBuilder(etcdClient)
	if err != nil {
		return nil, fmt.Errorf("创建 resolver builder 失败: %w", err)
	}

	log.Printf("✅ [ETCD] Resolver Builder 创建成功")
	log.Printf("   提示: 使用 'etcd:///<service-name>' 格式作为 Dial target")
	return builder, nil
}
