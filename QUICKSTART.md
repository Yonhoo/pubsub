# 快速开始 - Controller Manager

## ✅ Controller Manager 已完成

Controller Manager 模块已经完整实现，包含以下功能：

### 核心功能
- ✅ 接收 Connect-Node 的上线/下线通知（gRPC）
- ✅ 使用 Redis 持久化 Room 数据
- ✅ 房间管理（创建、加入、离开）
- ✅ 节点注册和心跳管理
- ✅ 提供查询接口给 Push-Manager

### 技术栈
- **gRPC**: 标准 gRPC 服务（不使用 PSRPC）
- **Redis**: 数据持久化
- **ETCD**: 服务发现和注册
- **Go**: 1.21+

## 🚀 运行 Controller Manager

### 步骤 1: 启动依赖服务

```bash
# 启动 Redis
docker run -d --name redis -p 6379:6379 redis:latest

# 启动 ETCD（可选，用于服务发现）
docker run -d --name etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

### 步骤 2: 生成 gRPC 代码

```bash
cd /Users/yon/repo/psrpc/examples/pubsub

# 安装 protoc 插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 生成代码
cd proto
./gen.sh
```

### 步骤 3: 安装依赖

```bash
cd /Users/yon/repo/psrpc/examples/pubsub
go mod tidy
```

### 步骤 4: 运行 Controller

```bash
cd controller-manager
go run . controller-1 50051
```

你应该看到：

```
================================================================================
🚀 启动 Controller Manager: controller-1 (端口: 50051)
================================================================================

📡 连接到 Redis...
✅ Redis 连接成功

🏗️  创建 Controller Server...
📥 [Controller] 从 Redis 加载数据...
✅ [Controller] 加载了 0 个房间
✅ [Controller] 加载了 0 个节点

🔧 创建 gRPC Server...

📝 注册到 ETCD...

================================================================================
✅ Controller Manager 运行中
================================================================================

📋 服务信息:
  - Controller ID: controller-1
  - gRPC 端口: 50051
  - Redis: localhost:6379
  - ETCD: localhost:2379

🔌 gRPC 方法:
  - NotifyUserOnline: Connect-Node 通知用户上线
  - NotifyUserOffline: Connect-Node 通知用户下线
  - JoinRoom: 用户加入房间
  - LeaveRoom: 用户离开房间
  - GetRoomInfo: 获取房间信息（Push-Manager 查询）
  - GetUserNode: 获取用户所在节点（Push-Manager 查询）
  - GetRoomStats: 获取房间统计
  - RegisterNode: Connect-Node 注册
  - NodeHeartbeat: Connect-Node 心跳
```

## 🧪 测试 Controller

### 使用 grpcurl 测试

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 列出所有服务
grpcurl -plaintext localhost:50051 list

# 列出 ControllerService 的方法
grpcurl -plaintext localhost:50051 list pubsub.ControllerService

# 获取房间统计
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats

# 加入房间
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "room_id": "room-001",
  "user_name": "Alice",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/JoinRoom

# 查看房间信息
grpcurl -plaintext -d '{
  "room_id": "room-001"
}' localhost:50051 pubsub.ControllerService/GetRoomInfo

# 用户离开房间
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "room_id": "room-001"
}' localhost:50051 pubsub.ControllerService/LeaveRoom
```

### 查看 Redis 数据

```bash
redis-cli

# 查看所有房间
KEYS room:*

# 查看房间详情
GET room:room-001

# 查看所有用户
KEYS user:*

# 查看用户详情
GET user:user-1

# 查看所有节点
KEYS node:*
```

## 📊 功能演示

### 1. 用户上线流程

```bash
# Connect-Node 通知用户上线
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "user_name": "Alice",
  "room_id": "room-001",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/NotifyUserOnline
```

Controller 会：
1. 创建用户对象
2. 保存到 Redis
3. 自动加入指定房间
4. 如果房间不存在，自动创建

### 2. 创建房间并加入

```bash
# 用户加入房间（如果不存在会自动创建）
grpcurl -plaintext -d '{
  "user_id": "user-2",
  "room_id": "room-002",
  "user_name": "Bob",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/JoinRoom
```

### 3. 查询房间统计

```bash
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats
```

输出示例：
```json
{
  "totalRooms": 2,
  "totalUsers": 3,
  "rooms": [
    {
      "roomId": "room-001",
      "userCount": 2,
      "createdAt": "1234567890"
    },
    {
      "roomId": "room-002",
      "userCount": 1,
      "createdAt": "1234567891"
    }
  ]
}
```

### 4. 节点注册

```bash
# Connect-Node 注册
grpcurl -plaintext -d '{
  "node_id": "node-1",
  "node_address": "localhost:50061",
  "max_connections": 1000
}' localhost:50051 pubsub.ControllerService/RegisterNode

# Node 心跳
grpcurl -plaintext -d '{
  "node_id": "node-1",
  "current_connections": 10,
  "cpu_usage": 25,
  "memory_usage": 40
}' localhost:50051 pubsub.ControllerService/NodeHeartbeat
```

## 🔍 数据流详解

### 用户加入房间完整流程

```
1. Connect-Node 通知用户上线
   Connect-Node --[gRPC: NotifyUserOnline]--> Controller
   
2. Controller 处理
   - 创建 User 对象
   - 保存到 Redis (user:user-1)
   - 调用 JoinRoom 自动加入房间
   
3. 加入房间
   - 获取或创建 Room
   - 添加 User 到 Room.Users
   - 保存 Room 到 Redis (room:room-001)
   
4. 数据同步
   - 内存缓存更新
   - Redis 持久化
   - 返回成功响应
```

### Redis 数据结构

```
room:room-001 = {
  "ID": "room-001",
  "Name": "room-001",
  "Users": {
    "user-1": {...},
    "user-2": {...}
  },
  "CreatedAt": "...",
  "UpdatedAt": "..."
}

user:user-1 = {
  "ID": "user-1",
  "Name": "Alice",
  "RoomID": "room-001",
  "NodeID": "node-1",
  "JoinedAt": "..."
}

node:node-1 = {
  "ID": "node-1",
  "Address": "localhost:50061",
  "CurrentConnections": 10,
  "CPUUsage": 25,
  "MemoryUsage": 40,
  "LastHeartbeat": "..."
}
```

## 🎓 关键特性

### 1. 自动房间创建
用户加入不存在的房间时，自动创建

### 2. 数据双写
- 内存缓存（快速访问）
- Redis 持久化（数据恢复）

### 3. 启动时加载
Controller 启动时从 Redis 加载所有房间和节点数据

### 4. 健康检查
每 30 秒检查一次，移除超时的节点

### 5. 统计信息
每 30 秒自动打印房间统计

## ⚠️ 注意事项

1. **Redis 必须运行**，否则无法启动
2. **ETCD 可选**，如果没有 ETCD 只会跳过服务注册
3. **端口冲突**：确保 50051 端口未被占用
4. **数据持久化**：Room 和 User 数据都在 Redis 中

## 🔮 下一步

现在 Controller Manager 已完成，接下来可以实现：

1. **Connect-Node**: 管理 WebSocket 连接，调用 Controller gRPC 接口
2. **Push-Manager**: 查询 Controller，推送消息到 Connect-Node
3. **Biz-Server**: 业务逻辑处理，调用 Push-Manager

## 💡 开发建议

1. 使用 grpcurl 测试所有接口
2. 观察 Controller 日志了解处理流程
3. 检查 Redis 数据验证持久化
4. 启动多个 Controller 实例测试（不同端口）

---

**Controller Manager 已完成** ✅
