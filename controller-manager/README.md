# Controller-Manager 运行指南

## 配置方式

Controller-Manager 支持三种配置方式，优先级从高到低：

1. **环境变量** （最高优先级）
2. **YAML 配置文件** （`config.yaml`）
3. **默认值** （代码中定义）

### 方式 1：使用 YAML 配置文件（推荐）

1. **编辑配置文件**：

```bash
cd /home/yonhoo/pubsub/controller-manager
vim config.yaml
```

2. **运行服务**：

```bash
go run main.go
```

配置文件会自动加载 `config.yaml`，支持环境变量替换：

```yaml
database:
  host: ${DB_HOST:localhost}  # 优先使用环境变量 DB_HOST，否则使用 localhost
  port: ${DB_PORT:3306}
```

### 方式 2：使用环境变量覆盖

即使有 YAML 配置文件，环境变量也会覆盖配置文件中的值：

```bash
export DB_HOST=192.168.1.100
export DB_PORT=3307
export REDIS_ADDR=192.168.1.101:6379
go run main.go
```

### 方式 3：纯环境变量（不使用 YAML）

如果没有 `config.yaml` 文件，程序会使用环境变量和默认值：

```bash
SERVER_ID=controller-1 \
SERVER_PORT=50051 \
DB_HOST=localhost \
DB_PORT=3306 \
DB_USER=pubsub \
DB_PASSWORD=pubsub123 \
DB_NAME=pubsub \
REDIS_ADDR=localhost:6379 \
ETCD_ENDPOINTS=localhost:2379 \
go run main.go
```

## 快速启动

### 本地开发（使用 localhost 服务）

```bash
cd /home/yonhoo/pubsub/controller-manager
go run main.go
```

默认配置会连接到：
- MySQL: `localhost:3306`
- Redis: `localhost:6379`
- ETCD: `localhost:2379`

### 连接到 Docker 服务

如果你的 MySQL、Redis、ETCD 运行在 Docker 中：

```bash
# 启动依赖服务
cd /home/yonhoo/pubsub
docker-compose up -d mysql redis etcd

# 运行 Controller-Manager
cd controller-manager
go run main.go
```

### 完全使用 Docker

```bash
cd /home/yonhoo/pubsub
docker-compose up -d
```

## 配置项说明

### 核心配置

| 配置项 | 环境变量 | YAML 路径 | 默认值 | 说明 |
|--------|----------|-----------|--------|------|
| 服务器 ID | `SERVER_ID` | `server.id` | `controller-1` | 服务器唯一标识 |
| gRPC 端口 | `SERVER_PORT` | `server.port` | `50051` | gRPC 监听端口 |
| 监听地址 | `SERVER_ADDR` | `server.addr` | `0.0.0.0:50051` | 完整监听地址 |

### 数据库配置

| 配置项 | 环境变量 | YAML 路径 | 默认值 |
|--------|----------|-----------|--------|
| 主机 | `DB_HOST` | `database.host` | `localhost` |
| 端口 | `DB_PORT` | `database.port` | `3306` |
| 用户名 | `DB_USER` | `database.user` | `pubsub` |
| 密码 | `DB_PASSWORD` | `database.password` | `pubsub123` |
| 数据库名 | `DB_NAME` | `database.database` | `pubsub` |

### Redis 配置

| 配置项 | 环境变量 | YAML 路径 | 默认值 |
|--------|----------|-----------|--------|
| 地址 | `REDIS_ADDR` | `redis.addr` | `localhost:6379` |
| 密码 | `REDIS_PASSWORD` | `redis.password` | `` |
| 数据库 | `REDIS_DB` | `redis.db` | `0` |

### ETCD 配置

| 配置项 | 环境变量 | YAML 路径 | 默认值 |
|--------|----------|-----------|--------|
| 端点 | `ETCD_ENDPOINTS` | `etcd.endpoints` | `localhost:2379` |

## 验证运行

### 检查服务状态

```bash
# 检查 gRPC 端口
netstat -tlnp | grep 50051

# 检查进程
ps aux | grep controller-manager
```

### 使用 grpcurl 测试

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 列出服务
grpcurl -plaintext localhost:50051 list

# 调用方法（示例）
grpcurl -plaintext localhost:50051 controller.ControllerService/GetSystemStats
```

### 查看日志

日志会输出到标准输出（stdout）。

## 故障排查

### 问题 1：无法加载配置文件

```
⚠️  无法加载 config.yaml: open config.yaml: no such file or directory，使用默认配置和环境变量
```

**解决方案**：
- 这是正常的警告，程序会使用默认配置和环境变量
- 如果需要使用 YAML 配置，确保在 `controller-manager` 目录下有 `config.yaml` 文件

### 问题 2：无法连接数据库

```
❌ 连接数据库失败
```

**解决方案**：
1. 检查 MySQL 是否运行：`mysql -h localhost -u pubsub -ppubsub123`
2. 检查配置是否正确：查看 `config.yaml` 或环境变量
3. 如果使用 Docker：确保 `docker-compose up -d mysql` 已运行

### 问题 3：无法连接 Redis

```
❌ Redis 连接失败
```

**解决方案**：
1. 检查 Redis 是否运行：`redis-cli ping`
2. 检查配置：`redis.addr` 或 `REDIS_ADDR`

### 问题 4：无法连接 ETCD

```
❌ ETCD 连接失败
```

**解决方案**：
1. 检查 ETCD 是否运行：`etcdctl endpoint health`
2. 检查配置：`etcd.endpoints` 或 `ETCD_ENDPOINTS`

## 配置示例

### 示例 1：开发环境

```yaml
# config.yaml
server:
  id: dev-controller
  port: 50051

database:
  host: localhost
  port: 3306
  user: root
  password: root123

redis:
  addr: localhost:6379

etcd:
  endpoints:
    - localhost:2379
```

### 示例 2：生产环境

```yaml
# config.yaml
server:
  id: prod-controller-1
  port: 50051

database:
  host: mysql.prod.internal
  port: 3306
  user: pubsub_prod
  password: ${DB_PASSWORD}  # 从环境变量读取敏感信息

redis:
  addr: redis.prod.internal:6379
  password: ${REDIS_PASSWORD}

etcd:
  endpoints:
    - etcd-1.prod.internal:2379
    - etcd-2.prod.internal:2379
    - etcd-3.prod.internal:2379
```

### 示例 3：Docker Compose 环境

```yaml
# config.yaml
database:
  host: ${DB_HOST:mysql}  # Docker Compose 服务名
  port: 3306

redis:
  addr: ${REDIS_ADDR:redis:6379}

etcd:
  endpoints:
    - ${ETCD_ENDPOINTS:etcd:2379}
```

## 相关文档

- [系统架构](../README.md)
- [Docker 部署](../docker-compose.yml)
- [配置源码](../pkg/config/config.go)






