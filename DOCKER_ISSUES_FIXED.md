# Docker Compose 问题修复总结

## 修复的问题

### 1. ✅ Controller-Manager 数据库连接失败

**问题**：
```
❌ 连接数据库失败: dial tcp [::1]:3306: connect: connection refused
```

**原因**：
- 代码中使用环境变量 `MYSQL_HOST`、`MYSQL_PORT` 等
- docker-compose.yml 中使用 `DB_HOST`、`DB_PORT` 等
- 环境变量名不匹配导致使用默认值 `localhost`

**修复**：
修改 `pkg/config/config.go`，将环境变量名统一为 `DB_*`：
```go
Database: &DatabaseConfig{
    Host:     getEnv("DB_HOST", "localhost"),
    Port:     getEnvAsInt("DB_PORT", 3306),
    User:     getEnv("DB_USER", "root"),
    Password: getEnv("DB_PASSWORD", "password"),
    DBName:   getEnv("DB_NAME", "pubsub"),
    Charset:  getEnv("DB_CHARSET", "utf8mb4"),
},
```

### 2. ✅ Connect-Node gRPC 连接安全凭证缺失

**问题**：
```
panic: grpc: no transport security set (use grpc.WithTransportCredentials(insecure.NewCredentials()) explicitly or set credentials)
```

**原因**：
- gRPC v1.64.0+ 要求明确设置传输凭证
- 代码中没有设置 TLS 或 insecure 凭证

**修复**：
修改 `connect-node/main.go` 中的 `newLogicClient` 函数：
```go
import "google.golang.org/grpc/credentials/insecure"

conn, err := grpc.DialContext(ctx, "etcd://services/controller-manager",
    grpc.WithResolvers(resolverBuilder),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

### 3. ⚠️ 端口冲突（需要手动处理）

**问题**：
```
Bind for 0.0.0.0:8080 failed: port is already allocated
```

**原因**：
- 主机上的 8080 端口已被其他进程占用

**解决方案**：

#### 方案 A：停止占用 8080 的进程
```bash
# 查找占用 8080 的进程
sudo lsof -i :8080
# 或
sudo netstat -tulpn | grep 8080

# 停止该进程
sudo kill <PID>
```

#### 方案 B：修改 docker-compose.yml 端口映射
将 connect-node-1 的端口改为其他端口：
```yaml
connect-node-1:
  ports:
    - "50052:50052"
    - "8083:8080"  # 改为 8083
    - "9091:9091"
```

## 重新构建和启动

### 1. 停止并清理现有容器
```bash
docker-compose down
```

### 2. 重新构建镜像
```bash
export DOCKER_BUILDKIT=1
docker-compose build
```

或使用脚本：
```bash
./build.sh
```

### 3. 启动服务
```bash
docker-compose up -d
```

### 4. 查看日志
```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f controller
docker-compose logs -f connect-node-1
```

### 5. 检查服务状态
```bash
docker-compose ps
```

## 验证服务正常运行

### Controller Manager
```bash
# 检查健康状态
curl http://localhost:9090/health

# 查看 Metrics
curl http://localhost:9090/metrics
```

### Connect Node
```bash
# Connect Node 1
curl http://localhost:8080/health

# Connect Node 2
curl http://localhost:8081/health

# Connect Node 3
curl http://localhost:8082/health
```

### Push Manager
```bash
curl http://localhost:9093/health
```

### ETCD
```bash
# 检查 ETCD 健康状态
docker exec pubsub-etcd etcdctl endpoint health

# 查看注册的服务
docker exec pubsub-etcd etcdctl get --prefix services/
```

### MySQL
```bash
# 连接 MySQL
docker exec -it pubsub-mysql mysql -u pubsub -ppubsub123 pubsub

# 查看表
SHOW TABLES;
```

## 常见问题

### Q1: 容器启动后立即退出
**A**: 查看日志找出具体原因：
```bash
docker-compose logs <service-name>
```

### Q2: 数据库连接超时
**A**: 确保 MySQL 容器已完全启动：
```bash
docker-compose ps mysql
# 等待 healthy 状态
```

### Q3: ETCD 连接失败
**A**: 检查 ETCD 是否正常运行：
```bash
docker-compose logs etcd
docker exec pubsub-etcd etcdctl endpoint health
```

### Q4: 端口已被占用
**A**: 
1. 查找占用端口的进程并停止
2. 或修改 docker-compose.yml 中的端口映射

## 架构图

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │
       ↓
┌─────────────────────────────────────┐
│        Connect Nodes (3)            │
│  ┌──────────┬──────────┬──────────┐ │
│  │  Node 1  │  Node 2  │  Node 3  │ │
│  │ :8080    │ :8081    │ :8082    │ │
│  └──────────┴──────────┴──────────┘ │
└──────────┬──────────────────────────┘
           │
           ↓
┌─────────────────────┐
│  Controller Manager │
│      :50051         │
└──────┬──────────────┘
       │
       ↓
┌──────────────────────────────────┐
│    Infrastructure Services       │
│  ┌────────┬────────┬──────────┐  │
│  │ MySQL  │ Redis  │  ETCD    │  │
│  │ :3306  │ :6379  │  :2379   │  │
│  └────────┴────────┴──────────┘  │
└──────────────────────────────────┘
```

## 下一步

1. ✅ 确保所有服务正常启动
2. ✅ 测试 WebSocket 连接
3. ✅ 测试消息推送功能
4. ✅ 监控 Metrics 和 Traces
5. ✅ 压力测试

## 相关文档

- [Docker 构建优化](./DOCKER_BUILD_OPTIMIZATION.md)
- [快速开始指南](./QUICKSTART.md)
- [MySQL 快速开始](./QUICKSTART_MYSQL.md)







