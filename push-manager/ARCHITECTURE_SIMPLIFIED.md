# Push-Manager 简化架构设计

## 核心理念

Push-Manager 采用**纯广播模式**，不需要查询 Controller 来获取用户/房间信息。所有消息都直接广播到每个 Connect-Node，由 Connect-Node 根据自己维护的订阅关系决定推送给哪些用户。

## 架构图

```
┌──────────────┐
│  Biz-Server  │  业务服务（音频、翻译等）
└──────┬───────┘
       │ gRPC (PushToRoom/PushToUser/Broadcast)
       ▼
┌───────────────────┐
│  Push-Manager     │  消息广播中心
│                   │  • 通过 ETCD 发现节点
│                   │  • 广播到所有节点
│                   │  • 不查询用户信息
└────────┬──────────┘
         │
         │ 广播到所有 Connect-Node
         │
    ┌────┴────┬────────┐
    ▼         ▼        ▼
┌──────────┐┌──────────┐┌──────────┐
│Connect-1 ││Connect-2 ││Connect-3 │
│          ││          ││          │
│ room-001 ││ room-001 ││ room-002 │
│ - user1  ││ - user3  ││ - user5  │
│ - user2  ││ - user4  ││ - user6  │
└────┬─────┘└────┬─────┘└────┬─────┘
     │          │          │
     ▼          ▼          ▼
   推送       推送        推送
```

## 工作流程

### 场景 1: 推送到房间 (PushToRoom)

```
Biz-Server: PushToRoom(room_id="room-001", content=...)
                │
                ▼
        Push-Manager
                │
                ├─ 广播到所有 Connect-Node
                │
    ┌───────────┼───────────┐
    ▼           ▼           ▼
Connect-1    Connect-2    Connect-3
    │           │           │
    ├─ 检查是否有 room-001 的用户
    │           │           │
user1,user2   user3,user4  (没有)
    │           │           
    ▼           ▼
   推送        推送
```

**特点:**
- Push-Manager 不需要知道哪些用户在房间里
- 每个 Connect-Node 自己判断是否有该房间的用户
- 由 Connect-Node 负责推送

### 场景 2: 推送给用户 (PushToUser)

```
Biz-Server: PushToUser(user_id="user-003", content=...)
                │
                ▼
        Push-Manager
                │
                ├─ 广播到所有 Connect-Node
                │
    ┌───────────┼───────────┐
    ▼           ▼           ▼
Connect-1    Connect-2    Connect-3
    │           │           │
    ├─ 查找用户是否在该节点
    │           │           │
  (没有)      找到!       (没有)
              user-003
                │
                ▼
               推送
```

**特点:**
- Push-Manager 不需要查询用户在哪个节点
- 并发广播到所有节点
- 只有拥有该用户的节点会推送成功

### 场景 3: 全局广播 (BroadcastMessage)

```
Biz-Server: BroadcastMessage(content=...)
                │
                ▼
        Push-Manager
                │
                ├─ 广播到所有 Connect-Node
                │
    ┌───────────┼───────────┐
    ▼           ▼           ▼
Connect-1    Connect-2    Connect-3
    │           │           │
    ├─ 推送给所有用户
    │           │           │
所有用户      所有用户      所有用户
```

## 优势

### 1. 架构简单
- Push-Manager 不依赖 Controller
- 不需要查询数据库或 Redis
- 无状态，易于扩展

### 2. 性能高效
- 无额外查询开销
- 并发广播到所有节点
- 网络延迟固定

### 3. 容错能力强
- 某个节点故障不影响其他节点
- 无需维护用户-节点映射
- 节点可以随时上下线

### 4. 扩展性好
- 水平扩展 Push-Manager（多实例）
- 水平扩展 Connect-Node（动态增减）
- ETCD 自动服务发现

## 权衡

### 优点
✅ 架构简单，易于理解和维护
✅ 无需查询用户信息，性能好
✅ 容错能力强
✅ 易于扩展

### 缺点
❌ 每条消息都广播到所有节点（网络开销）
❌ Connect-Node 需要处理所有消息（即使没有相关用户）

### 适用场景
- 节点数量不多（< 100个节点）
- 消息推送频率适中
- 房间内用户分布均匀

### 不适用场景
- 节点数量特别多（> 1000个节点）
- 推送频率极高（> 10万/秒）
- 用户集中在少数节点（会浪费广播）

## 实现细节

### Push-Manager

```go
// 推送到房间 - 广播模式
func (s *PushManagerServer) PushToRoom(req *pb.PushToRoomRequest) {
    // 获取所有 Connect-Node
    nodes := s.getAllConnectNodes()
    
    // 并发广播到所有节点
    for _, node := range nodes {
        go func(n *Node) {
            // 节点自己判断是否有该房间的用户
            n.PushMessageBatch(
                userIds: [],        // 空列表
                roomId: req.RoomId, // 指定房间
                content: req.Content,
            )
        }(node)
    }
}
```

### Connect-Node

```go
// 批量推送 - 支持广播
func (s *ConnectNodeServer) PushMessageBatch(req *pb.PushMessageBatchRequest) {
    if len(req.UserIds) == 0 {
        if req.RoomId != "" {
            // 推送给该房间的所有用户（如果有）
            s.wsManager.PushToRoom(req.RoomId, req.Content, nil)
        } else {
            // 推送给所有用户
            rooms := s.wsManager.GetAllRooms()
            for _, roomId := range rooms {
                s.wsManager.PushToRoom(roomId, req.Content, nil)
            }
        }
    } else {
        // 推送给指定用户列表
        for _, userId := range req.UserIds {
            s.wsManager.PushToUser(req.RoomId, userId, req.Content)
        }
    }
}
```

## 对比传统架构

### 传统架构（需要查询）

```
Push-Manager
    ↓ 1. GetRoomInfo(room_id)
Controller (查询 MySQL/Redis)
    ↓ 2. 返回用户列表和节点分布
Push-Manager
    ↓ 3. 按节点分组
    ↓ 4. 推送到对应节点
Connect-Node
```

**特点:**
- 需要查询用户信息
- 精准推送到目标节点
- 适合节点很多的场景

### 简化架构（纯广播）

```
Push-Manager
    ↓ 直接广播到所有节点
Connect-Node (自己判断)
    ↓ 推送给订阅的用户
客户端
```

**特点:**
- 无需查询
- 所有节点都收到消息
- 适合节点较少的场景

## 性能对比

### 消息延迟

| 架构 | 查询延迟 | 网络延迟 | 总延迟 |
|------|----------|----------|--------|
| 传统 | 10-50ms | 5-10ms | 15-60ms |
| 简化 | 0ms | 5-10ms | 5-10ms |

### 网络开销

假设：
- 10 个 Connect-Node
- 100 个用户，平均分布
- 推送到 1 个房间（10个用户在2个节点）

| 架构 | 查询请求 | 推送请求 | 总请求 |
|------|----------|----------|--------|
| 传统 | 1 | 2 | 3 |
| 简化 | 0 | 10 | 10 |

## 监控指标

```
# 广播效率
push_manager_broadcast_nodes_total{api="PushToRoom"} 10
push_manager_broadcast_delivered_total{api="PushToRoom"} 5

# 节点处理
connect_node_push_received_total{has_users="true"} 2
connect_node_push_received_total{has_users="false"} 8
```

## 优化建议

### 1. 智能过滤
```go
// Connect-Node 快速判断
if !s.wsManager.HasRoom(req.RoomId) {
    return // 快速返回，不处理
}
```

### 2. 批量合并
```go
// Push-Manager 合并短时间内的多次推送
batcher.Add(message)
// 定时批量发送
```

### 3. 连接复用
```go
// 维护长连接池
nodeClients map[string]pb.ConnectNodeServiceClient
```

## 总结

简化架构通过**纯广播模式**实现了：
- ✅ 架构简单
- ✅ 无需查询
- ✅ 低延迟
- ✅ 易扩展

非常适合**中小规模**的实时推送场景！


