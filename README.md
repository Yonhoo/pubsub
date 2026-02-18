# PubSub 系统 - gRPC + WebSocket 架构

## 🎉 Controller Manager 已完成！

### ✅ 已实现的功能

#### 1. **标准 gRPC Proto 定义**
- ✅ `controller.proto` - Controller 服务（9 个 RPC 方法）
- ✅ `connect_node.proto` - Connect Node 服务
- ✅ `push_manager.proto` - Push Manager 服务
- ✅ 使用标准 gRPC（不使用 PSRPC）

#### 2. **Controller Manager 完整实现**
- ✅ 用户上线/下线通知处理
- ✅ 房间管理（创建、加入、离开）
- ✅ Redis 数据持久化
- ✅ 节点注册和心跳管理
- ✅ 提供查询接口给 Push-Manager
- ✅ 自动健康检查
- ✅ 统计信息打印

#### 3. **基础设施**
- ✅ ETCD 服务发现和注册
- ✅ Redis 数据存储
- ✅ 公共类型定义（Room, User, ConnectNode）

## 📂 项目结构

```
pubsub/
├── proto/                          # gRPC Proto
│   ├── controller.proto           ✅
│   ├── connect_node.proto         ✅
│   ├── push_manager.proto         ✅
│   └── gen.sh                     ✅
├── pkg/
│   ├── etcd/                      # ETCD 服务发现
│   │   └── registry.go            ✅
│   ├── redis/                     # Redis 存储
│   │   └── redis.go               ✅
│   └── types/                     # 公共类型
│       └── types.go               ✅
├── controller-manager/            # ✅ Controller (已完成)
│   ├── controller.go              ✅
│   └── main.go                    ✅
├── connect-node/                  # ⏳ 待实现
├── push-manager/                  # ⏳ 待实现
├── biz-server/                    # ⏳ 待实现
├── go.mod                         ✅
├── README.md                      ✅
└── QUICKSTART.md                  ✅
```

## 🏗️ 架构设计

```
用户 <--WebSocket--> Connect-Node <--gRPC--> Controller <--gRPC--> Push-Manager <--gRPC--> Biz-Server
                           |                      |
                           |                      |
                           +-------- ETCD --------+  (服务发现)
                                      |
                                    Redis (数据存储)
```

**核心设计：**
- ❌ 不使用 PSRPC
- ❌ 不使用 Redis Pub/Sub
- ✅ 使用标准 gRPC
- ✅ 使用 ETCD 服务发现
- ✅ 使用 Redis 数据持久化
- ✅ Connect-Node 与用户使用 WebSocket

## 🚀 快速开始

### 1. 启动依赖服务

```bash
# Redis
docker run -d --name redis -p 6379:6379 redis:latest

# ETCD (可选)
docker run -d --name etcd \
  -p 2379:2379 -p 2380:2380 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

### 2. 生成代码

```bash
cd proto
chmod +x gen.sh
./gen.sh
```

### 3. 运行 Controller

```bash
cd controller-manager
go run . controller-1 50051
```

### 4. 测试

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 测试
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats
```

详细文档请查看 [QUICKSTART.md](QUICKSTART.md)

## 📊 Controller Manager 功能

### gRPC 方法

1. **NotifyUserOnline** - Connect-Node 通知用户上线
2. **NotifyUserOffline** - Connect-Node 通知用户下线  
3. **JoinRoom** - 用户加入房间
4. **LeaveRoom** - 用户离开房间
5. **GetRoomInfo** - 获取房间信息（Push-Manager 查询）
6. **GetUserNode** - 获取用户所在节点（Push-Manager 查询）
7. **GetRoomStats** - 获取房间统计
8. **RegisterNode** - Connect-Node 注册
9. **NodeHeartbeat** - Connect-Node 心跳

### 数据管理

- **Redis 持久化**：Room、User、Node 数据
- **内存缓存**：快速访问
- **自动同步**：内存 ↔ Redis
- **启动加载**：从 Redis 恢复数据

### 健康管理

- 每 30 秒检查节点健康
- 自动移除超时节点
- 定期打印统计信息

## 🎯 已实现的核心流程

### 用户上线并加入房间

```
1. Connect-Node 发起 gRPC 调用
   NotifyUserOnline(user_id, room_id, node_id)
   
2. Controller 处理
   - 创建 User 对象
   - 保存到 Redis
   - 自动调用 JoinRoom
   
3. 房间处理
   - 如果房间不存在，创建新房间
   - 添加用户到房间
   - 保存到 Redis
   
4. 返回成功响应
```

### Push-Manager 查询用户位置

```
1. Push-Manager 调用
   GetUserNode(user_id)
   
2. Controller 返回
   {node_id, node_address}
   
3. Push-Manager 使用 node_address
   连接到 Connect-Node 推送消息
```

## 🔮 下一步计划

### 待实现模块

1. **Connect-Node** (优先级：高)
   - WebSocket 服务器
   - gRPC 客户端（调用 Controller）
   - 用户连接管理

2. **Push-Manager** (优先级：中)
   - gRPC 服务端（接收 Biz-Server 请求）
   - gRPC 客户端（查询 Controller，调用 Connect-Node）

3. **Biz-Server** (优先级：低)
   - 业务逻辑示例
   - gRPC 客户端（调用 Push-Manager）

4. **监控系统** (优先级：中)
   - Metrics 集成
   - OpenTelemetry 链路追踪

## 📚 文档

- [QUICKSTART.md](QUICKSTART.md) - 快速开始指南
- [ARCHITECTURE_GRPC.md](ARCHITECTURE_GRPC.md) - 完整架构设计
- Proto 文件中有详细的接口说明

## 💡 技术栈

- **Go**: 1.21+
- **gRPC**: 标准 gRPC 通信
- **Redis**: 数据持久化
- **ETCD**: 服务发现
- **WebSocket**: 用户长连接（待实现）
- **Protocol Buffers**: 接口定义

## 🎓 关键特性

1. **标准 gRPC**：不依赖 PSRPC，使用标准 gRPC
2. **数据持久化**：Redis 存储所有关键数据
3. **服务发现**：ETCD 自动注册和发现
4. **自动恢复**：启动时从 Redis 加载数据
5. **健康检查**：自动移除不健康节点
6. **实时统计**：定期打印系统状态

## ⚡ 性能考虑

- 内存缓存提升访问速度
- Redis 异步持久化
- gRPC 高效二进制协议
- 连接复用和池化

## 📝 示例用法

```bash
# 用户加入房间
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "room_id": "room-001",
  "user_name": "Alice",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/JoinRoom

# 查看统计
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats

# 查看 Redis 数据
redis-cli
> KEYS room:*
> GET room:room-001
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

---

**Controller Manager 模块已完成** ✅

下一步：实现 Connect-Node 或 Push-Manager？
