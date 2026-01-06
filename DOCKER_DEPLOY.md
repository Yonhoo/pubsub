# 🐳 Docker 部署指南

## 📋 系统架构

```
┌─────────────────────────────────────────────────────┐
│                  Docker Compose                     │
│                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │   MySQL      │  │    Redis     │  │   ETCD   │  │
│  │  (数据库)    │  │   (缓存)     │  │ (发现)   │  │
│  └──────────────┘  └──────────────┘  └──────────┘  │
│         ↓               ↓                  ↓         │
│  ┌──────────────────────────────────────────────┐   │
│  │         Controller-Manager                  │   │
│  │  (管理器，room/node/user 管理)              │   │
│  └──────────────────────────────────────────────┘   │
│         ↓                                            │
│  ┌───────────┐  ┌───────────┐  ┌───────────┐       │
│  │Connect-   │  │Connect-   │  │Connect-   │       │
│  │Node-1     │  │Node-2     │  │Node-3     │       │
│  │(长连接)   │  │(长连接)   │  │(长连接)   │       │
│  └───────────┘  └───────────┘  └───────────┘       │
│         ↓              ↓              ↓              │
│  ┌──────────────────────────────────────────────┐   │
│  │         Push-Manager                        │   │
│  │  (推送管理，事件驱动)                       │   │
│  └──────────────────────────────────────────────┘   │
│                                                     │
│  ┌──────────────┐                                   │
│  │   Jaeger     │  (链路追踪)                       │
│  └──────────────┘                                   │
└─────────────────────────────────────────────────────┘
```

---

## 🚀 快速启动

### 前置条件

- ✅ Docker 20.10+
- ✅ Docker Compose 2.0+
- ✅ 至少 4GB RAM
- ✅ 20GB 可用磁盘空间

### 一键启动

```bash
# 1. 克隆项目
git clone <repo-url>
cd examples/pubsub

# 2. 构建镜像
./build.sh
# 或使用 make
make build-images

# 3. 启动服务
docker-compose up -d
# 或使用 make
make start

# 4. 检查服务状态
docker-compose ps
# 或使用 make
make ps

# 5. 查看日志
docker-compose logs -f
# 或使用 make
make logs
```

---

## 📦 服务详细说明

### 基础服务

#### 1. MySQL (数据库)

```
端口: 3306
用户: pubsub
密码: pubsub123
数据库: pubsub
```

**数据表**:
- `rooms` - 聊天室表
- `room_users` - 房间用户关系表
- `connect_nodes` - 连接节点表

#### 2. Redis (缓存)

```
端口: 6379
用途: 消息缓存、会话存储
```

#### 3. ETCD (服务发现)

```
端口: 2379 (客户端), 2380 (对等通信)
用途: 服务注册与发现、配置管理
```

#### 4. Jaeger (链路追踪)

```
端口: 16686 (UI), 4318 (OTLP HTTP)
用途: 分布式追踪，帮助诊断性能问题
访问: http://localhost:16686
```

### 业务服务

#### 1. Controller-Manager

```
端口: 50051 (gRPC), 9090 (Metrics)
职责: 
  - 管理聊天室
  - 管理连接节点
  - 管理用户（加入/退出/更新）
  - 通知用户变更
```

**启动日志**:
```
✅ Controller-Manager 启动成功
📍 gRPC 服务器启动: :50051
📊 Metrics 服务器启动: :9090
```

#### 2. Connect-Node (×3)

```
Node-1:
  gRPC: 50052, HTTP: 8080, Metrics: 9091

Node-2:
  gRPC: 50055, HTTP: 8081, Metrics: 9092

Node-3:
  gRPC: 50056, HTTP: 8082, Metrics: 9094

职责:
  - 维护与用户的 WebSocket 连接
  - 推送消息到用户
  - 上报节点状态到 Controller
  - 向 ETCD 注册自己
```

**访问方式**:
```bash
# 健康检查
curl http://localhost:8080/health

# WebSocket 连接
ws://localhost:8080/ws?user_id=user1&user_name=user1&room_id=room1

# Metrics
curl http://localhost:9091/metrics
```

#### 3. Push-Manager

```
端口: 50053 (gRPC), 9093 (Metrics)

职责:
  - 发现所有 Connect-Node 实例（通过 ETCD）
  - 为每个节点维护客户端连接
  - 接收推送请求
  - 分发消息到所有 Connect-Node
  - 多 Worker 并发处理
```

**关键特性**:
- ✅ 事件驱动架构（ETCD Watch API）
- ✅ 10 个 Worker 并发处理
- ✅ 1000 消息队列缓冲
- ✅ 5 秒 RPC 超时保护
- ✅ 实时节点发现（毫秒级响应）

---

## 📊 常用命令

### Docker Compose 命令

```bash
# 启动所有服务（后台）
docker-compose up -d

# 启动特定服务
docker-compose up -d mysql redis etcd

# 查看服务状态
docker-compose ps

# 查看服务日志
docker-compose logs -f [service-name]

# 进入容器
docker-compose exec [service-name] sh

# 停止所有服务
docker-compose stop

# 重启所有服务
docker-compose restart

# 删除所有容器和数据卷
docker-compose down -v

# 查看特定服务的配置
docker-compose config --services
```

### Make 命令

```bash
# 查看所有可用命令
make help

# 构建镜像
make build-images

# 启动服务
make start

# 停止服务
make stop

# 重启服务
make restart

# 查看日志
make logs
make logs-controller
make logs-connect
make logs-push

# 查看服务状态
make ps

# 健康检查
make health

# 清理所有
make clean

# 重新构建并启动
make rebuild
```

---

## 🔍 监控和调试

### 1. 检查服务状态

```bash
# 查看所有容器
docker-compose ps

# 查看容器详细信息
docker-compose ps --no-trunc

# 查看容器资源使用情况
docker stats
```

### 2. 查看日志

```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志（最后 100 行）
docker-compose logs --tail=100 push-manager

# 查看特定时间范围的日志
docker-compose logs --since 2024-01-01 --until 2024-01-02 controller

# 只显示错误日志
docker-compose logs push-manager | grep ERROR
```

### 3. 进入容器调试

```bash
# 进入 Push-Manager 容器
docker-compose exec push-manager sh

# 在容器内执行命令
docker-compose exec mysql mysql -u pubsub -ppubsub123 -D pubsub

# 查看容器网络配置
docker-compose exec push-manager ifconfig

# 测试连通性
docker-compose exec push-manager ping etcd
```

### 4. 性能监控

#### Metrics

```bash
# Controller Metrics
curl http://localhost:9090/metrics

# Connect-Node-1 Metrics
curl http://localhost:9091/metrics

# Push-Manager Metrics
curl http://localhost:9095/metrics
```

#### Jaeger 链路追踪

访问: http://localhost:16686

可以查看：
- 服务拓扑
- 链路追踪
- 性能分析
- 错误追踪

#### 数据库查询

```bash
# 进入 MySQL
docker-compose exec mysql mysql -u pubsub -ppubsub123 -D pubsub

# 查询房间列表
SELECT * FROM rooms;

# 查询房间用户
SELECT * FROM room_users WHERE room_id = 'room1';

# 查询连接节点
SELECT * FROM connect_nodes;
```

---

## 🧪 测试

### 1. 基础连通性测试

```bash
# 测试 MySQL
docker-compose exec mysql mysqladmin ping -h localhost -u root -proot123

# 测试 Redis
docker-compose exec redis redis-cli ping

# 测试 ETCD
docker-compose exec etcd etcdctl endpoint health

# 测试 Controller gRPC
docker run --rm --network=pubsub_pubsub-network \
  nicolaka/netcat -zv controller 50051

# 测试 Connect-Node HTTP
curl -v http://localhost:8080/health

# 测试 Push-Manager gRPC
docker run --rm --network=pubsub_pubsub-network \
  nicolaka/netcat -zv push-manager 50053
```

### 2. 功能测试

```bash
# 连接 WebSocket
wscat -c "ws://localhost:8080/ws?user_id=user1&user_name=user1&room_id=room1"

# 在另一个终端连接
wscat -c "ws://localhost:8080/ws?user_id=user2&user_name=user2&room_id=room1"

# 发送消息（在 wscat 中输入）
{"type": "message", "content": "hello"}
```

### 3. 数据库测试

```bash
# 查询房间
mysql -h localhost -u pubsub -ppubsub123 -D pubsub \
  -e "SELECT * FROM rooms;"

# 查询用户
mysql -h localhost -u pubsub -ppubsub123 -D pubsub \
  -e "SELECT * FROM room_users;"

# 查询节点
mysql -h localhost -u pubsub -ppubsub123 -D pubsub \
  -e "SELECT * FROM connect_nodes;"
```

---

## 📝 配置文件说明

### config.yaml

位置: `./config.yaml`

主要配置项：
- **server.addr** - 服务监听地址
- **database** - MySQL 连接配置
- **redis** - Redis 连接配置
- **etcd** - ETCD 连接配置
- **rpc** - gRPC RPC 配置
- **logging** - 日志配置
- **tracing** - 追踪配置
- **metrics** - 指标配置

环境变量支持：
```yaml
host: ${DB_HOST:localhost}  # 使用 DB_HOST 环境变量，默认 localhost
port: ${DB_PORT:3306}       # 使用 DB_PORT 环境变量，默认 3306
```

---

## 🆘 故障排查

### 问题 1: 容器启动失败

```bash
# 查看错误日志
docker-compose logs [service-name]

# 重新构建镜像
docker-compose build --no-cache [service-name]

# 重新启动服务
docker-compose restart [service-name]
```

### 问题 2: 服务无法连接

```bash
# 检查网络
docker network ls
docker network inspect pubsub_pubsub-network

# 检查服务间连通性
docker-compose exec [service1] ping [service2]

# 查看防火墙
docker-compose exec [service] netstat -tlnp
```

### 问题 3: 数据库初始化失败

```bash
# 检查数据库日志
docker-compose logs mysql

# 重新初始化数据库
docker-compose down -v
docker-compose up -d mysql
# 等待 MySQL 启动
sleep 10
docker-compose up -d
```

### 问题 4: ETCD 服务发现不工作

```bash
# 查看 ETCD 键值
docker-compose exec etcd etcdctl get /services/connect-node/ --prefix

# 清理 ETCD 数据
docker-compose exec etcd etcdctl del /services --prefix

# 重启 ETCD
docker-compose restart etcd
```

---

## 📈 性能优化

### 1. 增加 Connect-Node 副本

```bash
# 在 docker-compose.yml 中添加更多实例
docker-compose up -d --scale connect-node=5

# 或在 docker-compose.yml 中手动添加服务
```

### 2. 调整资源限制

编辑 `docker-compose.yml`:
```yaml
services:
  push-manager:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

### 3. 优化数据库连接池

编辑 `config.yaml`:
```yaml
database:
  max_open_conns: 200    # 增加连接数
  max_idle_conns: 50
```

---

## 🛑 关闭和清理

### 优雅关闭

```bash
# 停止所有服务（保留数据）
docker-compose stop

# 停止特定服务
docker-compose stop push-manager

# 重启服务
docker-compose restart
```

### 完全清理

```bash
# 删除所有容器和数据卷
docker-compose down -v

# 删除所有未使用的镜像
docker image prune -a

# 删除所有未使用的数据卷
docker volume prune
```

---

## 🔗 相关资源

- Docker 文档: https://docs.docker.com
- Docker Compose 文档: https://docs.docker.com/compose
- ETCD 文档: https://etcd.io/docs
- Jaeger 文档: https://www.jaegertracing.io/docs

---

**👍 部署完成！系统已就绪！**
