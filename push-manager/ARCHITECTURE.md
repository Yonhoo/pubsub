# Push-Manager 架构设计

## 概述

Push-Manager 是消息推送系统的路由中心，负责将 Biz-Server 的推送请求路由到正确的 Connect-Node。

## 核心职责

### 1. 服务发现
- 通过 ETCD 自动发现所有在线的 Connect-Node
- 实时监听节点上下线事件
- 维护节点地址映射表

### 2. 路由决策
- 查询 Controller-Manager 获取用户/房间信息
- 确定消息应该推送到哪些 Connect-Node
- 处理用户不在线、房间不存在等异常情况

### 3. 消息分发
- 将消息推送到目标 Connect-Node
- 支持单用户推送、房间推送、广播推送
- 按节点分组，批量推送优化

### 4. 连接管理
- 维护到所有 Connect-Node 的 gRPC 连接池
- 按需创建连接，懒加载模式
- 自动清理下线节点的连接

## 架构流程

### 场景 1: 推送消息给指定用户

```
┌─────────────┐
│ Biz-Server  │
└──────┬──────┘
       │ 1. PushToUser(user_id, content)
       ▼
┌─────────────────┐
│  Push-Manager   │
└────────┬────────┘
         │ 2. GetUserNode(user_id)
         ▼
┌──────────────────┐
│ Controller-Mgr   │ ← 查询 MySQL
│                  │   SELECT node_id, room_id 
│                  │   FROM room_users 
│                  │   WHERE user_id = ?
└────────┬─────────┘
         │ 3. Returns: {node_id, room_id}
         ▼
┌─────────────────┐
│  Push-Manager   │
└────────┬────────┘
         │ 4. 从连接池获取 node 客户端
         │    或从 ETCD 获取 node 地址创建连接
         ▼
┌────────────────┐
│  Connect-Node  │
│   (node_id)    │
└────────┬───────┘
         │ 5. PushMessage(user_id, room_id, content)
         ▼
     WebSocket 推送给用户
```

### 场景 2: 推送消息到房间

```
┌─────────────┐
│ Biz-Server  │
└──────┬──────┘
       │ 1. PushToRoom(room_id, content)
       ▼
┌─────────────────┐
│  Push-Manager   │
└────────┬────────┘
         │ 2. GetRoomInfo(room_id)
         ▼
┌──────────────────┐
│ Controller-Mgr   │ ← 查询 MySQL
│                  │   SELECT user_id, node_id
│                  │   FROM room_users
│                  │   WHERE room_id = ?
└────────┬─────────┘
         │ 3. Returns: [{user_id, node_id}, ...]
         ▼
┌─────────────────┐
│  Push-Manager   │ ← 按 node_id 分组
│                  │   node1: [user1, user2, user3]
│                  │   node2: [user4, user5]
└─────┬───────┬───┘
      │       │ 4. 并发推送
      ▼       ▼
┌──────────┐┌──────────┐
│Connect-1 ││Connect-2 │
└────┬─────┘└────┬─────┘
     │           │ 5. PushMessageBatch
     ▼           ▼
  WebSocket    WebSocket
```

### 场景 3: 广播消息

```
┌─────────────┐
│ Biz-Server  │
└──────┬──────┘
       │ 1. BroadcastMessage(content)
       ▼
┌─────────────────┐
│  Push-Manager   │
└─────┬───────┬───┘
      │       │ 2. 获取所有 Connect-Node
      │       │    (从 ETCD 或缓存)
      ▼       ▼
┌──────────┐┌──────────┐┌──────────┐
│Connect-1 ││Connect-2 ││Connect-3 │
└────┬─────┘└────┬─────┘└────┬─────┘
     │           │           │ 3. 各节点广播给自己的所有用户
     ▼           ▼           ▼
  WebSocket    WebSocket   WebSocket
```

## 数据结构

### 节点地址映射
```go
nodeAddresses map[string]string
// 格式: {nodeID -> gRPC address}
// 示例: {
//   "connect-node-1": "192.168.1.10:50052",
//   "connect-node-2": "192.168.1.11:50052",
// }
```

### 节点客户端池
```go
nodeClients map[string]pb.ConnectNodeServiceClient
// 格式: {nodeID -> gRPC client}
// 懒加载，按需创建
```

## 容错处理

### Connect-Node 下线
- ETCD Watch 检测到节点下线
- 从 nodeAddresses 移除
- 从 nodeClients 移除连接
- 正在推送的请求失败，返回错误

### Controller-Manager 不可用
- GetUserNode / GetRoomInfo 调用失败
- 返回错误给 Biz-Server
- Biz-Server 可以选择重试或降级处理

### 网络分区
- gRPC 连接超时
- 记录错误日志
- 返回推送失败

### 消息丢失处理
Push-Manager 本身是无状态的，不存储消息：
- 推送失败直接返回失败状态
- 由 Biz-Server 决定是否重试
- 建议 Biz-Server 实现消息持久化和重试机制

## 性能优化

### 1. 连接复用
- 维护长连接池，避免频繁建立连接
- gRPC 连接支持多路复用

### 2. 批量推送
- 房间推送按节点分组
- 同一节点的多个用户批量推送
- 减少 RPC 调用次数

### 3. 并发推送
- 多个节点并发推送
- 使用 goroutine + WaitGroup
- 充分利用多核 CPU

### 4. 缓存优化
- 缓存节点地址，减少 ETCD 查询
- 缓存 gRPC 连接，避免重复创建

## 扩展性

### 水平扩展
Push-Manager 是无状态的，可以轻松水平扩展：
- 部署多个 Push-Manager 实例
- Biz-Server 通过负载均衡调用
- 每个实例独立管理自己的连接池

### 垂直扩展
- 调整连接池大小
- 增加并发推送的 goroutine 数量
- 优化网络和 CPU 资源

## 监控指标

### 关键指标
1. **推送成功率**
   - `push_success_total / (push_success_total + push_failed_total)`
   
2. **推送延迟**
   - P50, P95, P99 延迟
   - 分场景统计（单用户、房间、广播）

3. **节点健康度**
   - 在线节点数量
   - 各节点推送成功率

4. **连接池状态**
   - 活跃连接数
   - 连接创建失败次数

### 告警规则
- 推送成功率 < 95%
- P99 延迟 > 500ms
- 连接创建失败次数 > 10/min
- 无可用 Connect-Node

## 安全考虑

### 1. 认证授权
- Push-Manager 应该验证 Biz-Server 的身份
- 使用 gRPC 拦截器实现认证
- 支持 TLS 加密通信

### 2. 限流保护
- 限制单个 Biz-Server 的请求频率
- 防止恶意推送攻击

### 3. 内容过滤
- 可选的内容审核
- 防止推送违规内容

## 部署建议

### 开发环境
- 单实例部署
- 连接到开发环境的 ETCD 和 Controller

### 生产环境
- 多实例部署（建议 ≥ 3 个）
- 使用负载均衡（如 Nginx、HAProxy）
- 独立的 ETCD 集群
- 高可用 Controller-Manager

### 资源配置
- CPU: 2-4 核
- 内存: 2-4 GB
- 网络: 千兆网卡
- 磁盘: 无需持久化存储

## 相关组件

- [Controller-Manager](../controller-manager/ARCHITECTURE.md) - 房间信息管理
- [Connect-Node](../connect-node/ARCHITECTURE.md) - 长连接管理
- [ETCD](../SETUP.md) - 服务发现


