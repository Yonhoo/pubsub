# 快速开始 - Controller Manager (MySQL + Redis 版本)

## 🎉 新架构

```
Controller Manager
       ↓
   ┌───┴────┐
   │        │
MySQL      Redis
(主存储)   (缓存)
```

**关键改进：**
- ✅ MySQL + GORM 作为主存储
- ✅ 事务保证一致性（支持多 Controller 节点）
- ✅ Redis 仅用于缓存热点数据
- ✅ 关系查询、复杂统计
- ✅ 数据持久化、历史记录

## 🚀 快速启动

### 1. 启动依赖服务

```bash
# MySQL
docker run -d \
  --name mysql \
  -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=password \
  -e MYSQL_DATABASE=pubsub \
  mysql:8.0

# Redis (可选，用于缓存)
docker run -d \
  --name redis \
  -p 6379:6379 \
  redis:latest

# ETCD (可选，用于服务发现)
docker run -d \
  --name etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

### 2. 安装依赖

```bash
cd /Users/yon/repo/psrpc/examples/pubsub
go mod tidy
```

### 3. 生成 Proto 代码

```bash
cd proto
./gen.sh
```

### 4. 运行 Controller

```bash
cd controller-manager

# 设置 MySQL 密码（可选）
export MYSQL_PASSWORD=password

# 运行
go run . controller-1 50051
```

你将看到：

```
================================================================================
🚀 启动 Controller Manager: controller-1 (端口: 50051)
================================================================================

🔭 初始化 OpenTelemetry...
✅ OpenTelemetry 初始化成功

🗄️  连接到 MySQL...
✅ [Database] 数据库 'pubsub' 已就绪
✅ MySQL 连接成功
📦 [Database] 开始自动迁移...
✅ [Database] 表结构迁移完成

📡 连接到 Redis...
✅ Redis 连接成功

📊 创建 Metrics Collector...
✅ Metrics Collector 创建成功

🏗️  创建 Controller Server...

🔧 创建 gRPC Server...

📝 注册到 ETCD...

================================================================================
✅ Controller Manager 运行中
================================================================================

📋 服务信息:
  - Controller ID: controller-1
  - gRPC 端口: 50051
  - MySQL: localhost:3306/pubsub
  - Redis: localhost:6379 (缓存)
  - ETCD: localhost:2379
  - OpenTelemetry: enabled
  - Metrics: enabled
```

## 🧪 测试

### 使用 grpcurl

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 用户加入房间
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "room_id": "room-001",
  "user_name": "Alice",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/JoinRoom

# 查看房间统计
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats

# 获取房间信息
grpcurl -plaintext -d '{
  "room_id": "room-001"
}' localhost:50051 pubsub.ControllerService/GetRoomInfo
```

### 使用 MySQL 客户端

```bash
# 连接到 MySQL
mysql -h localhost -u root -ppassword pubsub

# 查看所有房间
SELECT * FROM rooms;

# 查看房间用户关系
SELECT * FROM room_users WHERE left_at IS NULL;

# 查看在线节点
SELECT * FROM connect_nodes WHERE status = 'online';
```

## 📊 数据库表结构

### rooms (房间表)
```sql
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(64) | 房间ID (主键) |
| name | VARCHAR(128) | 房间名称 |
| description | VARCHAR(512) | 房间描述 |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |
| deleted_at | TIMESTAMP | 删除时间 (软删除) |
```

### room_users (用户-房间关系表)
```sql
| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT | 自增ID (主键) |
| user_id | VARCHAR(64) | 用户ID |
| user_name | VARCHAR(128) | 用户名 |
| room_id | VARCHAR(64) | 房间ID |
| node_id | VARCHAR(64) | 连接节点ID |
| joined_at | TIMESTAMP | 加入时间 |
| left_at | TIMESTAMP | 离开时间 (NULL=在线) |
| deleted_at | TIMESTAMP | 删除时间 (软删除) |
```

### connect_nodes (连接节点表)
```sql
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(64) | 节点ID (主键) |
| address | VARCHAR(256) | 节点地址 |
| max_connections | INT | 最大连接数 |
| current_connections | INT | 当前连接数 |
| cpu_usage | FLOAT | CPU使用率 |
| memory_usage | FLOAT | 内存使用率 |
| status | VARCHAR(32) | 状态 (online/offline/unhealthy) |
| last_heartbeat | TIMESTAMP | 最后心跳时间 |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |
| deleted_at | TIMESTAMP | 删除时间 (软删除) |
```

## 🔄 核心流程

### 用户加入房间（带事务）

```
1. gRPC 请求 → Controller.JoinRoom
   ↓
2. Repository.UserJoinRoom (MySQL 事务)
   BEGIN TRANSACTION
   ├─ SELECT room (检查房间)
   ├─ INSERT room (如果不存在)
   ├─ SELECT COUNT (检查是否已满)
   ├─ SELECT user (检查是否已在房间)
   └─ INSERT room_user (添加关系)
   COMMIT
   ↓
3. 缓存到 Redis
   SET room_cache:{room_id}
   ↓
4. 返回成功响应
```

**优势：**
- ✅ 事务保证原子性
- ✅ 多个 Controller 同时操作不会冲突
- ✅ 数据库行级锁自动处理并发

### 查询房间信息（缓存优化）

```
1. 请求房间信息
   ↓
2. 先查 Redis 缓存
   GET room_cache:{room_id}
   ├─ 命中 → 直接返回 (快！)
   └─ 未命中 ↓
3. 查询 MySQL
   SELECT * FROM rooms
   JOIN room_users ...
   ↓
4. 写入 Redis 缓存 (TTL: 10分钟)
   SET room_cache:{room_id}
   ↓
5. 返回结果
```

## 🎯 多节点支持

现在支持多个 Controller 节点同时运行：

```bash
# 启动 Controller 1
go run . controller-1 50051

# 启动 Controller 2 (另一个终端)
go run . controller-2 50052

# 启动 Controller 3
go run . controller-3 50053
```

**一致性保证：**
- ✅ MySQL 事务处理所有写操作
- ✅ Redis 仅作缓存，数据不一致不影响业务
- ✅ 数据库行级锁防止并发冲突
- ✅ 每个 Controller 都可以独立处理请求

## 📈 性能优化

### 1. 数据库索引

```sql
-- 已自动创建的索引
INDEX idx_user_room (user_id, room_id)  -- 复合索引
INDEX idx_deleted_at (deleted_at)        -- 软删除索引

-- 查询优化示例
EXPLAIN SELECT * FROM room_users 
WHERE user_id = 'user-1' AND left_at IS NULL;
-- 使用索引，查询速度快
```

### 2. Redis 缓存策略

```
热点数据缓存 (TTL):
- room_cache:{room_id} → 10分钟
- user_online:{user_id} → 1小时
- node_heartbeat:{node_id} → 1分钟
```

### 3. 连接池

```go
// 已配置
MaxIdleConns: 10
MaxOpenConns: 100
ConnMaxLifetime: 1小时
```

## 🐛 故障排查

### MySQL 连接失败

```bash
# 检查 MySQL 是否运行
docker ps | grep mysql

# 检查端口
netstat -an | grep 3306

# 测试连接
mysql -h localhost -u root -ppassword pubsub
```

### 表结构未创建

```bash
# 手动迁移
cd controller-manager
go run . controller-1 50051
# 会自动执行 AutoMigrate
```

### Redis 缓存失效

```bash
# 检查 Redis
redis-cli PING

# 清除缓存
redis-cli FLUSHDB
```

## 📝 环境变量

```bash
# MySQL
export MYSQL_HOST=localhost
export MYSQL_PORT=3306
export MYSQL_USER=root
export MYSQL_PASSWORD=password
export MYSQL_DATABASE=pubsub

# Redis (可选)
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=""

# ETCD (可选)
export ETCD_ENDPOINTS=localhost:2379
```

## 🎓 下一步

1. ✅ Controller Manager 已完成
2. ⏳ 实现 Connect-Node (WebSocket + gRPC 客户端)
3. ⏳ 实现 Push-Manager (gRPC 服务端和客户端)
4. ⏳ 实现 Biz-Server (业务逻辑)

---

**Controller Manager (MySQL 版本) 已完成** ✅


