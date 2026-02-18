# PubSub 系统 Docker 部署指南

## 快速开始

### 1. 构建镜像

```bash
make build-images
```

或使用 docker-compose:

```bash
docker-compose build
```

### 2. 启动所有服务

```bash
make start
```

或:

```bash
docker-compose up -d
```

### 3. 查看服务状态

```bash
make ps
```

或:

```bash
docker-compose ps
```

### 4. 健康检查

```bash
make health
```

## 服务端口映射

### 基础服务

| 服务 | 容器端口 | 主机端口 | 说明 |
|------|---------|---------|------|
| MySQL | 3306 | 3306 | 数据库 |
| Redis | 6379 | 6379 | 缓存 |
| ETCD | 2379 | 2379 | 服务发现 |
| Jaeger UI | 16686 | 16686 | 链路追踪 |

### 业务服务

| 服务 | gRPC 端口 | HTTP 端口 | Metrics 端口 |
|------|----------|-----------|--------------|
| Controller | 50051 | - | 9090 |
| Connect-Node-1 | 50052 | 8080 | 9091 |
| Connect-Node-2 | 50055 | 8081 | 9092 |
| Connect-Node-3 | 50056 | 8082 | 9094 |
| Push-Manager | 50053 | - | 9095 |

## 常用命令

### 查看日志

```bash
# 所有服务
make logs

# Controller
make logs-controller

# Connect-Node
make logs-connect

# Push-Manager
make logs-push
```

### 服务管理

```bash
# 停止服务
make stop

# 重启服务
make restart

# 清理所有（包括数据卷）
make clean

# 重新构建并启动
make rebuild
```

## 测试连接

### 1. 测试 WebSocket 连接

```bash
# 在浏览器中打开
open http://localhost:8080/stats

# 或使用 wscat
wscat -c "ws://localhost:8080/ws?user_id=user-001&user_name=Alice&room_id=room-001"
```

### 2. 测试 gRPC 调用

```bash
# 查看服务列表
grpcurl -plaintext localhost:50051 list

# 查看房间统计
grpcurl -plaintext localhost:50051 \
  pubsub.ControllerService/GetRoomStats

# 推送消息到房间
grpcurl -plaintext \
  -d '{
    "room_id": "room-001",
    "content": {
      "type": "TEXT",
      "data": "SGVsbG8gV29ybGQh",
      "timestamp": 1234567890
    }
  }' \
  localhost:50053 \
  pubsub.PushManagerService/PushToRoom
```

### 3. 访问监控

```bash
# Jaeger UI
open http://localhost:16686

# Prometheus Metrics
curl http://localhost:9090/metrics   # Controller
curl http://localhost:9091/metrics   # Connect-Node-1
curl http://localhost:9095/metrics   # Push-Manager
```

## 环境变量配置

所有服务都支持通过环境变量进行配置。在 `docker-compose.yml` 中可以修改：

### Controller-Manager

```yaml
environment:
  - CONTROLLER_ID=controller-1
  - GRPC_PORT=50051
  - METRICS_PORT=9090
  - DB_HOST=mysql
  - DB_PORT=3306
  - DB_USER=pubsub
  - DB_PASSWORD=pubsub123
  - DB_NAME=pubsub
  - REDIS_ADDR=redis:6379
  - ETCD_ENDPOINTS=etcd:2379
```

### Connect-Node

```yaml
environment:
  - NODE_ID=connect-node-1
  - GRPC_PORT=50052
  - HTTP_PORT=8080
  - METRICS_PORT=9091
  - CONTROLLER_ADDRESS=controller:50051
  - ETCD_ENDPOINTS=etcd:2379
```

### Push-Manager

```yaml
environment:
  - MANAGER_ID=push-manager-1
  - GRPC_PORT=50053
  - METRICS_PORT=9093
  - ETCD_ENDPOINTS=etcd:2379
```

## 扩展节点

### 手动扩展 Connect-Node

在 `docker-compose.yml` 中添加新的 connect-node 服务：

```yaml
connect-node-4:
  build:
    context: .
    dockerfile: Dockerfile.connect-node
  container_name: pubsub-connect-node-4
  environment:
    - NODE_ID=connect-node-4
    - GRPC_PORT=50052
    - HTTP_PORT=8080
    - METRICS_PORT=9091
    - CONTROLLER_ADDRESS=controller:50051
    - ETCD_ENDPOINTS=etcd:2379
  ports:
    - "50057:50052"
    - "8083:8080"
    - "9096:9091"
  depends_on:
    - controller
    - etcd
  networks:
    - pubsub-network
  restart: unless-stopped
```

然后启动：

```bash
docker-compose up -d connect-node-4
```

## 数据持久化

数据卷：

- `mysql-data`: MySQL 数据
- `redis-data`: Redis 数据
- `etcd-data`: ETCD 数据

查看数据卷：

```bash
docker volume ls | grep pubsub
```

备份数据卷：

```bash
# 备份 MySQL
docker exec pubsub-mysql mysqldump -u root -proot123 pubsub > backup.sql

# 恢复 MySQL
docker exec -i pubsub-mysql mysql -u root -proot123 pubsub < backup.sql
```

## 故障排查

### 1. 查看容器日志

```bash
docker logs pubsub-controller-1
docker logs pubsub-connect-node-1
docker logs pubsub-push-manager-1
```

### 2. 进入容器调试

```bash
docker exec -it pubsub-controller-1 sh
docker exec -it pubsub-mysql sh
```

### 3. 检查网络连接

```bash
# 检查容器网络
docker network inspect pubsub_pubsub-network

# 测试容器间连接
docker exec pubsub-controller-1 ping mysql
docker exec pubsub-connect-node-1 wget -O- http://controller:50051
```

### 4. 重启单个服务

```bash
docker-compose restart controller
docker-compose restart connect-node-1
```

## 生产环境建议

### 1. 使用外部数据库

生产环境建议使用云厂商的托管数据库（RDS），而不是容器化的 MySQL。

修改 `docker-compose.yml`:

```yaml
controller:
  environment:
    - DB_HOST=your-rds-endpoint.amazonaws.com
    - DB_PORT=3306
    - DB_USER=your-user
    - DB_PASSWORD=your-password
```

### 2. 集群化部署

- Controller-Manager: 部署 3+ 个实例
- Connect-Node: 根据负载动态扩展
- Push-Manager: 部署 3+ 个实例
- ETCD: 部署 3 或 5 节点集群

### 3. 监控告警

配置 Prometheus + Grafana + AlertManager:

```bash
# 添加到 docker-compose.yml
prometheus:
  image: prom/prometheus
  volumes:
    - ./prometheus.yml:/etc/prometheus/prometheus.yml
  ports:
    - "9000:9090"

grafana:
  image: grafana/grafana
  ports:
    - "3000:3000"
```

### 4. 负载均衡

在 Controller 和 Push-Manager 前加 Nginx:

```yaml
nginx:
  image: nginx:alpine
  volumes:
    - ./nginx.conf:/etc/nginx/nginx.conf
  ports:
    - "80:80"
  depends_on:
    - controller
    - push-manager
```

## 清理

```bash
# 停止并删除所有容器
make clean

# 或
docker-compose down -v

# 删除镜像
docker rmi pubsub-controller:latest
docker rmi pubsub-connect-node:latest
docker rmi pubsub-push-manager:latest
```

## 更多信息

- [完整系统架构](./pub-msg.jpg)
- [快速开始指南](./QUICKSTART_COMPLETE.md)
- [Controller 文档](./controller-manager/README.md)
- [Connect-Node 文档](./connect-node/README.md)
- [Push-Manager 文档](./push-manager/README.md)


