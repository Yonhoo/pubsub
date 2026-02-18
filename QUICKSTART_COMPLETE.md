# 完整系统快速启动指南

本指南将带你从零开始启动整个 PubSub 消息推送系统。

## 系统架构

```
┌──────────────┐
│  Biz-Server  │  业务服务（音频、翻译等）
└──────┬───────┘
       │ gRPC
       ▼
┌───────────────────┐      ┌─────────────────────┐
│  Push-Manager     │◄────►│ Controller-Manager  │
│  (消息路由)       │      │ (房间管理)          │
└────────┬──────────┘      └──────────┬──────────┘
         │                             │
         │ 发现节点                     │ 注册
         │ 推送消息                     │ 心跳
    ┌────┴────┬────────┐              │
    ▼         ▼        ▼              ▼
┌──────────┐┌──────────┐┌──────────┐
│Connect-1 ││Connect-2 ││Connect-3 │  长连接节点
└────┬─────┘└────┬─────┘└────┬─────┘
     │          │          │
     ▼          ▼          ▼
  WebSocket  WebSocket  WebSocket  客户端连接
```

## 前置依赖

### 1. 安装基础服务

```bash
# MySQL
docker run -d \
  --name mysql \
  -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=root123 \
  -e MYSQL_DATABASE=pubsub \
  mysql:8.0

# Redis
docker run -d \
  --name redis \
  -p 6379:6379 \
  redis:7-alpine

# ETCD
docker run -d \
  --name etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  -e ETCD_ADVERTISE_CLIENT_URLS=http://0.0.0.0:2379 \
  -e ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379 \
  quay.io/coreos/etcd:v3.5.9
```

### 2. 安装 Jaeger (可选 - 用于链路追踪)

```bash
docker run -d \
  --name jaeger \
  -p 16686:16686 \
  -p 4318:4318 \
  jaegertracing/all-in-one:latest
```

访问: http://localhost:16686

## 配置文件

创建 `examples/pubsub/config.yaml`:

```yaml
database:
  host: localhost
  port: 3306
  user: root
  password: root123
  dbname: pubsub

redis:
  addr: localhost:6379
  password: ""
  db: 0

etcd:
  endpoints:
    - localhost:2379

server:
  id: controller-1
  port: 50051

room:
  default_max_users: 100
  cache_ttl: 300s
```

## 启动步骤

### 步骤 1: 启动 Controller-Manager

```bash
cd examples/pubsub/controller-manager

# 使用默认配置
go run .

# 或指定配置
CONTROLLER_ID=controller-1 \
GRPC_PORT=50051 \
go run .
```

验证启动:
```bash
# 检查健康状态
grpcurl -plaintext localhost:50051 list

# 查看房间统计
grpcurl -plaintext localhost:50051 \
  pubsub.ControllerService/GetRoomStats
```

### 步骤 2: 启动 Connect-Node (多个实例)

```bash
# 启动第一个 Connect-Node
cd examples/pubsub/connect-node

NODE_ID=connect-node-1 \
GRPC_PORT=50052 \
HTTP_PORT=8080 \
METRICS_PORT=9091 \
go run .
```

```bash
# 启动第二个 Connect-Node
NODE_ID=connect-node-2 \
GRPC_PORT=50055 \
HTTP_PORT=8081 \
METRICS_PORT=9092 \
go run .
```

```bash
# 启动第三个 Connect-Node
NODE_ID=connect-node-3 \
GRPC_PORT=50056 \
HTTP_PORT=8082 \
METRICS_PORT=9093 \
go run .
```

验证启动:
```bash
# 检查节点状态
curl http://localhost:8080/stats
curl http://localhost:8081/stats
curl http://localhost:8082/stats

# 检查 Metrics
curl http://localhost:9091/metrics
```

### 步骤 3: 启动 Push-Manager

```bash
cd examples/pubsub/push-manager

MANAGER_ID=push-manager-1 \
GRPC_PORT=50053 \
METRICS_PORT=9094 \
CONTROLLER_ADDRESS=localhost:50051 \
go run .
```

验证启动:
```bash
# 检查服务
grpcurl -plaintext localhost:50053 list
```

### 步骤 4: 连接客户端

使用浏览器打开 `examples/pubsub/connect-node/client.html`:

```bash
# macOS
open examples/pubsub/connect-node/client.html

# Linux
xdg-open examples/pubsub/connect-node/client.html
```

或使用任意 HTTP 服务器:

```bash
cd examples/pubsub/connect-node
python3 -m http.server 8000

# 访问 http://localhost:8000/client.html
```

**连接参数:**
- WebSocket URL: `ws://localhost:8080/ws`
- 房间 ID: `room-001`
- 用户 ID: `user-001`
- 用户名: `Alice`

### 步骤 5: 测试推送消息 (Biz-Server)

```bash
cd examples/pubsub/biz-server

# 推送消息到房间
go run example_client.go \
  --action push-to-room \
  --room room-001 \
  --message "Hello everyone!"

# 推送消息给指定用户
go run example_client.go \
  --action push-to-user \
  --user user-001 \
  --message "Hello Alice!"

# 广播消息
go run example_client.go \
  --action broadcast \
  --message "System announcement" \
  --type SYSTEM
```

## 完整测试流程

### 1. 创建房间并加入用户

打开 3 个浏览器窗口，分别连接:
- Alice (user-001) -> Connect-Node-1
- Bob (user-002) -> Connect-Node-2
- Charlie (user-003) -> Connect-Node-3

### 2. 查看房间信息

```bash
# 查询房间统计
grpcurl -plaintext localhost:50051 \
  pubsub.ControllerService/GetRoomStats

# 查询房间详情
grpcurl -plaintext \
  -d '{"room_id": "room-001"}' \
  localhost:50051 \
  pubsub.ControllerService/GetRoomInfo
```

### 3. 推送消息测试

```bash
# 1. 推送到房间（所有人收到）
go run biz-server/example_client.go \
  --action push-to-room \
  --room room-001 \
  --message "Hello Room!"

# 2. 推送给指定用户（只有 Alice 收到）
go run biz-server/example_client.go \
  --action push-to-user \
  --user user-001 \
  --message "Hi Alice!"

# 3. 推送音频消息
go run biz-server/example_client.go \
  --action push-to-room \
  --room room-001 \
  --type AUDIO \
  --message "audio_data_base64..."

# 4. 推送翻译消息
go run biz-server/example_client.go \
  --action push-to-user \
  --user user-001 \
  --type TRANSLATION \
  --message "你好！"
```

### 4. 观察日志

所有组件都会输出详细的日志，观察消息流转过程:

1. **Biz-Server** -> Push-Manager (推送请求)
2. **Push-Manager** -> Controller (查询房间/用户)
3. **Push-Manager** -> Connect-Node (推送消息)
4. **Connect-Node** -> WebSocket (推送给客户端)

### 5. 监控指标

访问 Metrics 端点:
- Controller: http://localhost:9090/metrics
- Connect-Node-1: http://localhost:9091/metrics
- Connect-Node-2: http://localhost:9092/metrics
- Connect-Node-3: http://localhost:9093/metrics
- Push-Manager: http://localhost:9094/metrics

### 6. 链路追踪

访问 Jaeger UI: http://localhost:16686

搜索服务:
- controller-manager
- connect-node
- push-manager

## 故障测试

### 测试 1: Connect-Node 下线

```bash
# 停止 Connect-Node-1
# Ctrl+C 停止进程

# 观察:
# 1. ETCD 自动移除该节点（10秒后）
# 2. Push-Manager 自动更新节点列表
# 3. Alice 的连接断开
# 4. Alice 重新连接到其他节点
```

### 测试 2: 负载均衡

```bash
# 查看每个节点的连接数
curl http://localhost:8080/stats  # Node-1
curl http://localhost:8081/stats  # Node-2
curl http://localhost:8082/stats  # Node-3

# 连接10个客户端，观察负载分布
```

### 测试 3: 房间推送性能

```bash
# 房间内有100个用户，分布在3个节点
# Push-Manager 会并发推送到3个节点
# 观察推送耗时
```

## 性能指标

### 预期性能 (单机部署)

| 指标 | 数值 |
|------|------|
| 单节点连接数 | 10,000+ |
| 房间推送延迟 (100人) | < 50ms |
| 点对点推送延迟 | < 10ms |
| 广播延迟 (10个节点) | < 100ms |

### 资源占用

| 组件 | CPU | 内存 |
|------|-----|------|
| Controller-Manager | 0.5 核 | 512MB |
| Connect-Node | 1 核 | 1GB |
| Push-Manager | 0.5 核 | 512MB |

## 常见问题

### 1. Connect-Node 连接不上 ETCD
```bash
# 检查 ETCD 是否运行
docker ps | grep etcd

# 测试 ETCD 连接
etcdctl --endpoints=http://localhost:2379 get / --prefix
```

### 2. WebSocket 连接失败
```bash
# 检查 Connect-Node 是否启动
curl http://localhost:8080/health

# 检查防火墙设置
```

### 3. 推送消息没收到
```bash
# 1. 检查用户是否在线
grpcurl -plaintext \
  -d '{"user_id": "user-001"}' \
  localhost:50051 \
  pubsub.ControllerService/GetUserNode

# 2. 检查房间信息
grpcurl -plaintext \
  -d '{"room_id": "room-001"}' \
  localhost:50051 \
  pubsub.ControllerService/GetRoomInfo

# 3. 查看 Push-Manager 日志
```

### 4. 数据库连接失败
```bash
# 检查 MySQL
docker exec -it mysql mysql -uroot -proot123 -e "SHOW DATABASES;"

# 重新初始化数据库
mysql -h localhost -u root -proot123 pubsub < schema.sql
```

## 生产环境部署

### 1. 使用 Docker Compose

参见 `docker-compose.yml`

### 2. 使用 Kubernetes

参见 `k8s/` 目录

### 3. 高可用配置

- Controller-Manager: 3+ 实例 + 负载均衡
- Connect-Node: 根据连接数动态扩容
- Push-Manager: 3+ 实例 + 负载均衡
- MySQL: 主从复制 or 集群
- Redis: 哨兵模式 or 集群
- ETCD: 3节点集群

## 下一步

- [系统架构详解](./pub-msg.jpg)
- [Controller-Manager 详解](./controller-manager/README.md)
- [Connect-Node 详解](./connect-node/README.md)
- [Push-Manager 详解](./push-manager/README.md)
- [Biz-Server 集成](./biz-server/README.md)


