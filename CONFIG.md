# 配置说明

## 📝 环境变量配置

Controller Manager 支持通过环境变量进行配置。

### 服务器配置

```bash
# 服务器 ID (默认: controller-1)
export SERVER_ID=controller-1

# gRPC 端口 (默认: 50051)
export SERVER_PORT=50051
```

### MySQL 数据库配置

```bash
# MySQL 主机 (默认: localhost)
export MYSQL_HOST=localhost

# MySQL 端口 (默认: 3306)
export MYSQL_PORT=3306

# MySQL 用户名 (默认: root)
export MYSQL_USER=root

# MySQL 密码 (默认: password)
export MYSQL_PASSWORD=your_password

# 数据库名 (默认: pubsub)
export MYSQL_DATABASE=pubsub

# 字符集 (默认: utf8mb4)
export MYSQL_CHARSET=utf8mb4
```

### Redis 配置

```bash
# Redis 地址 (默认: localhost:6379)
export REDIS_ADDR=localhost:6379

# Redis 密码 (默认: 空)
export REDIS_PASSWORD=""

# Redis 数据库编号 (默认: 0)
export REDIS_DB=0
```

### ETCD 配置

```bash
# ETCD 端点 (默认: localhost:2379)
export ETCD_ENDPOINTS=localhost:2379
```

### 房间配置

```bash
# 默认房间最大用户数 (默认: 100, 0=无限制)
export ROOM_MAX_USERS=100

# 房间缓存 TTL (分钟) (默认: 10)
export ROOM_CACHE_TTL_MINUTES=10
```

## 🚀 使用示例

### 1. 使用默认配置

```bash
cd controller-manager
go run .
```

### 2. 使用环境变量配置

```bash
# 设置环境变量
export MYSQL_PASSWORD=mypassword
export ROOM_MAX_USERS=50
export REDIS_ADDR=redis.example.com:6379

# 运行
go run . controller-1 50051
```

### 3. 使用 .env 文件 (推荐)

创建 `.env` 文件：

```bash
# .env
SERVER_ID=controller-prod-1
SERVER_PORT=50051

MYSQL_HOST=mysql.example.com
MYSQL_PORT=3306
MYSQL_USER=pubsub_user
MYSQL_PASSWORD=secure_password
MYSQL_DATABASE=pubsub_prod

REDIS_ADDR=redis.example.com:6379
REDIS_PASSWORD=redis_password
REDIS_DB=1

ETCD_ENDPOINTS=etcd1.example.com:2379

ROOM_MAX_USERS=200
ROOM_CACHE_TTL_MINUTES=15
```

加载并运行：

```bash
# 使用 dotenv
source .env
go run . controller-1 50051

# 或者使用 env 命令
env $(cat .env | xargs) go run .
```

### 4. Docker 部署

```bash
docker run -d \
  --name controller \
  -e MYSQL_HOST=mysql \
  -e MYSQL_PASSWORD=password \
  -e REDIS_ADDR=redis:6379 \
  -e ROOM_MAX_USERS=150 \
  -p 50051:50051 \
  controller-manager
```

### 5. Kubernetes ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: controller-config
data:
  MYSQL_HOST: "mysql-service"
  MYSQL_PORT: "3306"
  MYSQL_USER: "pubsub"
  MYSQL_DATABASE: "pubsub"
  REDIS_ADDR: "redis-service:6379"
  ETCD_ENDPOINTS: "etcd-service:2379"
  ROOM_MAX_USERS: "100"
  ROOM_CACHE_TTL_MINUTES: "10"

---
apiVersion: v1
kind: Secret
metadata:
  name: controller-secret
type: Opaque
stringData:
  MYSQL_PASSWORD: "your_password"
  REDIS_PASSWORD: "redis_password"
```

## 📊 配置示例

### 开发环境

```bash
SERVER_ID=controller-dev
SERVER_PORT=50051
MYSQL_HOST=localhost
MYSQL_PASSWORD=dev_password
REDIS_ADDR=localhost:6379
ROOM_MAX_USERS=10  # 小房间用于测试
ROOM_CACHE_TTL_MINUTES=1  # 短缓存便于测试
```

### 生产环境

```bash
SERVER_ID=controller-prod-1
SERVER_PORT=50051
MYSQL_HOST=mysql-primary.prod.internal
MYSQL_PASSWORD=strong_prod_password
MYSQL_USER=pubsub_prod
MYSQL_DATABASE=pubsub_prod
REDIS_ADDR=redis-cluster.prod.internal:6379
REDIS_PASSWORD=redis_prod_password
ETCD_ENDPOINTS=etcd1.prod.internal:2379,etcd2.prod.internal:2379,etcd3.prod.internal:2379
ROOM_MAX_USERS=500  # 大房间
ROOM_CACHE_TTL_MINUTES=30  # 长缓存提升性能
```

### 测试环境

```bash
SERVER_ID=controller-test
SERVER_PORT=50051
MYSQL_HOST=mysql-test
MYSQL_PASSWORD=test_password
MYSQL_DATABASE=pubsub_test
REDIS_ADDR=redis-test:6379
ROOM_MAX_USERS=0  # 无限制用于压测
```

## 🔍 配置验证

启动时会显示完整配置：

```
================================================================================
🚀 启动 Controller Manager: controller-1 (端口: 50051)
================================================================================

📋 服务信息:
  - Controller ID: controller-1
  - gRPC 端口: 50051
  - MySQL: localhost:3306/pubsub
  - Redis: localhost:6379 (缓存)
  - ETCD: [localhost:2379]
  - OpenTelemetry: enabled
  - Metrics: enabled

⚙️  房间配置:
  - 默认最大用户数: 100
  - 缓存 TTL: 10m0s
```

## 💡 配置优化建议

### 1. MaxUsers 设置

```bash
# 小型讨论组
ROOM_MAX_USERS=10

# 标准会议室
ROOM_MAX_USERS=50

# 大型直播
ROOM_MAX_USERS=1000

# 无限制（危险）
ROOM_MAX_USERS=0
```

### 2. 缓存 TTL 设置

```bash
# 高频访问，短 TTL 保持新鲜
ROOM_CACHE_TTL_MINUTES=5

# 标准配置
ROOM_CACHE_TTL_MINUTES=10

# 低频访问，长 TTL 减少数据库压力
ROOM_CACHE_TTL_MINUTES=30
```

### 3. 数据库连接

```bash
# 本地开发
MYSQL_HOST=localhost

# Docker Compose
MYSQL_HOST=mysql

# Kubernetes
MYSQL_HOST=mysql-service.default.svc.cluster.local

# 外部数据库
MYSQL_HOST=rds.amazonaws.com
```

## 🔐 安全建议

1. **永远不要在代码中硬编码密码**
2. **使用密钥管理服务**（AWS Secrets Manager, Vault）
3. **限制数据库用户权限**
4. **使用 TLS/SSL 连接**
5. **定期轮换密码**

## 🐛 故障排查

### 配置未生效

```bash
# 检查环境变量是否设置
env | grep MYSQL
env | grep REDIS
env | grep ROOM

# 确认优先级：命令行参数 > 环境变量 > 默认值
```

### 数据库连接失败

```bash
# 测试 MySQL 连接
mysql -h $MYSQL_HOST -u $MYSQL_USER -p$MYSQL_PASSWORD $MYSQL_DATABASE

# 检查防火墙
telnet $MYSQL_HOST $MYSQL_PORT
```

### Redis 连接失败

```bash
# 测试 Redis 连接
redis-cli -h $REDIS_HOST -p $REDIS_PORT -a $REDIS_PASSWORD PING
```

---

**配置系统已完成** ✅


