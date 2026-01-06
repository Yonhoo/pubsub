# 数据库架构设计

## 🏗️ 整体架构

```
┌─────────────────┐
│  Controller     │
│   Manager       │
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
┌───▼───┐ ┌──▼──────┐
│ MySQL │ │  Redis  │
│ GORM  │ │  Cache  │
└───────┘ └─────────┘
```

## 📊 数据分层

### MySQL（持久化层）- 使用 GORM

**用途：**
- ✅ 房间信息持久化
- ✅ 用户-房间关系管理
- ✅ 复杂查询和统计
- ✅ 事务保证一致性
- ✅ 历史数据记录

**表结构：**

```sql
-- 房间表
rooms
  - id (PK)
  - name
  - description
  - max_users
  - created_at
  - updated_at
  - deleted_at (软删除)

-- 用户-房间关系表
room_users
  - id (PK)
  - user_id (索引)
  - user_name
  - room_id (索引)
  - node_id
  - joined_at
  - left_at (NULL = 在线)
  - deleted_at (软删除)

-- 连接节点表
connect_nodes
  - id (PK)
  - address
  - max_connections
  - current_connections
  - cpu_usage
  - memory_usage
  - status (online/offline/unhealthy)
  - last_heartbeat
  - created_at
  - updated_at
  - deleted_at (软删除)
```

### Redis（缓存层）

**用途：**
- ✅ 用户在线状态（短期）
- ✅ 节点心跳检测
- ✅ 热点数据缓存
- ✅ 分布式锁

**数据结构：**
```
user_online:{user_id} -> {node_id, timestamp} (TTL: 1小时)
node_heartbeat:{node_id} -> {connections, cpu, memory} (TTL: 1分钟)
room_cache:{room_id} -> {json} (TTL: 10分钟)
```

## 🔄 数据流

### 用户加入房间

```
1. Controller 接收请求
   ↓
2. MySQL 事务操作：
   - 检查房间是否存在（不存在则创建）
   - 检查房间是否已满
   - 检查用户是否已在房间
   - 插入 room_users 记录
   ↓
3. Redis 缓存：
   - SET user_online:{user_id}
   - SET room_cache:{room_id}
   ↓
4. 返回成功
```

### 用户离开房间

```
1. Controller 接收请求
   ↓
2. MySQL 更新：
   - UPDATE room_users SET left_at = NOW()
   ↓
3. Redis 清理：
   - DEL user_online:{user_id}
   - DEL room_cache:{room_id}
   ↓
4. 返回成功
```

## ✅ 优势

### MySQL + GORM

1. **事务支持** - 保证操作原子性
```go
repo.UserJoinRoom(ctx, userID, userName, roomID, nodeID)
// 内部使用事务，要么全部成功，要么全部回滚
```

2. **关系查询** - 复杂的关联查询
```go
// 获取用户加入的所有房间
rooms := repo.GetUserRooms(ctx, userID)

// 获取房间的所有用户
users := repo.GetRoomUsers(ctx, roomID)
```

3. **软删除** - 数据可恢复
```go
// 软删除，数据不会真正删除
repo.DeleteRoom(ctx, roomID)
```

4. **索引优化** - 快速查询
```sql
-- 复合索引
INDEX idx_user_room (user_id, room_id)

-- 条件查询非常快
WHERE user_id = ? AND room_id = ? AND left_at IS NULL
```

## 🚀 API 使用示例

### 初始化数据库

```go
// 创建数据库
config := database.DefaultConfig()
config.Password = "your_password"

err := database.CreateDatabaseIfNotExists(config)

// 连接数据库
db, err := database.NewDatabase(config)

// 自动迁移表结构
err = database.AutoMigrate(db)

// 创建仓库
repo := database.NewRepository(db)
```

### 用户加入房间（带事务）

```go
// 🔥 关键：使用事务自动处理一致性
err := repo.UserJoinRoom(ctx, "user-1", "Alice", "room-001", "node-1")
if err == gorm.ErrInvalidData {
    return errors.New("房间已满")
}
```

### 查询房间用户

```go
// 获取房间及用户数
room, userCount, err := repo.GetRoomWithStats(ctx, "room-001")

// 获取房间中的所有用户
users, err := repo.GetRoomUsers(ctx, "room-001")

// 获取用户加入的所有房间
rooms, err := repo.GetUserRooms(ctx, "user-1")
```

### 节点管理

```go
// 注册节点
node := &database.ConnectNode{
    ID:             "node-1",
    Address:        "localhost:50061",
    MaxConnections: 1000,
}
repo.RegisterNode(ctx, node)

// 更新心跳
repo.UpdateNodeHeartbeat(ctx, "node-1", 10, 25.5, 40.0)

// 标记不健康的节点
repo.MarkUnhealthyNodes(ctx, 1*time.Minute)
```

## 🔒 并发安全

### MySQL 事务隔离

```go
// GORM 自动使用事务
repo.UserJoinRoom(ctx, ...)
// 内部实现：
// BEGIN
// SELECT ... FOR UPDATE  -- 行级锁
// INSERT ...
// COMMIT
```

### 乐观锁（可选）

```go
type Room struct {
    Version int `gorm:"default:0"` // 版本号
}

// 更新时检查版本
db.Model(&Room{}).
    Where("id = ? AND version = ?", roomID, oldVersion).
    Update("version", oldVersion+1)
```

## 📈 性能优化

### 1. 索引策略

```sql
-- 复合索引（user_id, room_id）
-- 支持查询：
--   WHERE user_id = ?
--   WHERE user_id = ? AND room_id = ?

-- 单独索引 left_at
-- 支持查询：WHERE left_at IS NULL
```

### 2. 连接池

```go
sqlDB.SetMaxIdleConns(10)     // 空闲连接数
sqlDB.SetMaxOpenConns(100)    // 最大连接数
sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大生命周期
```

### 3. 预加载（避免 N+1 查询）

```go
// 一次查询获取房间及所有用户
db.Preload("RoomUsers", "left_at IS NULL").
   First(&room, "id = ?", roomID)
```

### 4. Redis 缓存

```go
// 先查缓存
cachedRoom := redis.Get("room:" + roomID)
if cachedRoom != nil {
    return cachedRoom
}

// 缓存未命中，查数据库
room := db.GetRoom(roomID)

// 写入缓存
redis.Set("room:"+roomID, room, 10*time.Minute)
```

## 🐳 Docker 快速启动

```bash
# MySQL
docker run -d \
  --name mysql \
  -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=password \
  -e MYSQL_DATABASE=pubsub \
  mysql:8.0

# Redis
docker run -d \
  --name redis \
  -p 6379:6379 \
  redis:latest
```

## 📝 配置示例

```go
// config.yaml
database:
  host: localhost
  port: 3306
  user: root
  password: password
  dbname: pubsub
  charset: utf8mb4

redis:
  addr: localhost:6379
  password: ""
  db: 0
```

## 🎯 最佳实践

1. **使用事务** - 保证多步操作的原子性
2. **软删除** - 便于数据恢复和审计
3. **合理使用缓存** - 热点数据优先
4. **定期清理** - 删除过期的历史记录
5. **监控慢查询** - 优化性能瓶颈

---

**数据库层已完成** ✅  
下一步：更新 Controller 使用新的数据库层


