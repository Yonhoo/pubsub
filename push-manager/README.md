# Push-Manager 推送管理服务

## 概述

Push-Manager 是消息推送系统的核心组件，负责接收 Biz-Server 的推送请求，并将消息路由到正确的 Connect-Node 进行推送。

## 架构设计

```
┌─────────────┐
│ Biz-Server  │
└──────┬──────┘
       │ gRPC
       ▼
┌─────────────────┐      ┌──────────────────┐
│  Push-Manager   │◄────►│ Controller-Mgr   │
│                 │      │ (查询房间信息)    │
└────────┬────────┘      └──────────────────┘
         │
         │ 通过 ETCD 发现
         │
    ┌────┴────┬────────┐
    ▼         ▼        ▼
┌────────┐┌────────┐┌────────┐
│Connect ││Connect ││Connect │
│Node-1  ││Node-2  ││Node-3  │
└────────┘└────────┘└────────┘
```

## 核心功能

### 1. 推送消息到房间 (PushToRoom)
- 从 Controller-Manager 查询房间信息
- 获取房间内所有用户及其所在节点
- 按节点分组，批量推送到各个 Connect-Node

### 2. 推送消息给指定用户 (PushToUser)
- 从 Controller-Manager 查询用户所在节点
- 直接推送到对应的 Connect-Node

### 3. 广播消息 (BroadcastMessage)
- 推送消息到所有在线用户
- 并发推送到所有 Connect-Node

## 技术特点

### 服务发现
- 通过 ETCD 自动发现所有 Connect-Node
- 实时监听节点上下线，动态更新连接池

### 连接管理
- 维护到所有 Connect-Node 的 gRPC 连接池
- 按需创建连接，避免资源浪费
- 自动清理下线节点的连接

### 高性能
- 并发推送到多个节点
- 按节点分组，减少网络开销
- 连接复用，提高推送效率

### 可观测性
- OpenTelemetry 链路追踪
- Prometheus Metrics 监控
- 详细的日志记录

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| MANAGER_ID | Push-Manager ID | push-manager-1 |
| GRPC_PORT | gRPC 服务端口 | 50053 |
| METRICS_PORT | Metrics 端口 | 9093 |
| CONTROLLER_ADDRESS | Controller 地址 | localhost:50051 |

### 配置文件

使用共享的 `pkg/config` 配置，包括：
- ETCD 配置
- Tracing 配置

## 启动方式

### 本地启动

```bash
cd /Users/yon/repo/psrpc/examples/pubsub/push-manager

# 使用默认配置
go run .

# 自定义配置
MANAGER_ID=push-manager-1 \
GRPC_PORT=50053 \
CONTROLLER_ADDRESS=localhost:50051 \
go run .
```

### Docker 启动

```bash
docker run -d \
  --name push-manager-1 \
  -p 50053:50053 \
  -p 9093:9093 \
  -e MANAGER_ID=push-manager-1 \
  -e CONTROLLER_ADDRESS=controller:50051 \
  -e ETCD_ENDPOINTS=etcd:2379 \
  push-manager:latest
```

## API 使用示例

### 1. 推送消息到房间

```bash
grpcurl -plaintext \
  -d '{
    "room_id": "room-001",
    "content": {
      "type": "TEXT",
      "data": "Hello Room!",
      "timestamp": 1234567890,
      "metadata": {}
    }
  }' \
  localhost:50053 pubsub.PushManagerService/PushToRoom
```

### 2. 推送消息给用户

```bash
grpcurl -plaintext \
  -d '{
    "user_id": "user-001",
    "content": {
      "type": "TEXT",
      "data": "Hello User!",
      "timestamp": 1234567890,
      "metadata": {}
    }
  }' \
  localhost:50053 pubsub.PushManagerService/PushToUser
```

### 3. 广播消息

```bash
grpcurl -plaintext \
  -d '{
    "content": {
      "type": "SYSTEM",
      "data": "System Announcement",
      "timestamp": 1234567890,
      "metadata": {}
    }
  }' \
  localhost:50053 pubsub.PushManagerService/BroadcastMessage
```

## 监控指标

### Metrics 端点
```
http://localhost:9093/metrics
```

### 关键指标
- `push_manager_api_requests_total` - API 请求总数
- `push_manager_api_duration_seconds` - API 请求耗时
- `push_manager_node_connections` - Connect-Node 连接数
- `push_manager_push_success_total` - 推送成功总数
- `push_manager_push_failed_total` - 推送失败总数

## 故障处理

### Connect-Node 下线
- 自动从连接池移除
- 推送失败会记录日志
- 用户下次连接到其他节点后可以继续接收消息

### Controller 不可用
- 推送请求会失败
- 返回错误信息给调用方
- 需要确保 Controller-Manager 高可用

### ETCD 不可用
- 无法发现新的 Connect-Node
- 现有连接可以继续使用
- 恢复后自动重新发现

## 性能优化

### 批量推送
- 相同节点的用户批量推送
- 减少 gRPC 调用次数

### 连接池
- 复用 gRPC 连接
- 避免频繁建立连接

### 并发推送
- 多节点并发推送
- 充分利用系统资源

## 开发建议

### 扩展功能
1. 支持消息优先级
2. 支持消息持久化
3. 支持推送重试机制
4. 支持推送结果回调

### 性能调优
1. 调整 gRPC 连接池大小
2. 优化并发推送的 goroutine 数量
3. 添加请求限流

## 相关文档

- [架构设计](../pub-msg.jpg)
- [Controller-Manager](../controller-manager/README.md)
- [Connect-Node](../connect-node/README.md)
- [整体快速开始](../QUICKSTART.md)


