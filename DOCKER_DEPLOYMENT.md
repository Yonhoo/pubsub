# 🐳 Docker Compose 部署指南

## 📋 服务列表

### 基础服务
- **MySQL** (3306): 数据库
- **Redis** (6379): 缓存
- **ETCD** (2379, 2380): 服务发现
- **Jaeger** (16686): 链路追踪 UI

### 业务服务
- **Controller-Manager** (50051, 9090): 控制管理器
- **Connect-Node-1** (50052, 8083, 9091): 连接节点1
- **Connect-Node-2** (50055, 8084, 9092): 连接节点2  
- **Connect-Node-3** (50056, 8085, 9094): 连接节点3
- **Push-Manager** (50053, 9095): 推送管理器
- **Biz-Server** (8082): 业务服务器（HTTP API）
- **Web-Server** (8086): Web 聊天界面

## 🚀 快速启动

### 1. 构建并启动所有服务

```bash
docker-compose up --build
```

或后台运行：

```bash
docker-compose up -d --build
```

### 2. 查看服务状态

```bash
docker-compose ps
```

### 3. 查看日志

```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f connect-node-1
docker-compose logs -f push-manager
docker-compose logs -f biz-server
```

### 4. 停止服务

```bash
docker-compose down
```

### 5. 完全清理（包括数据卷）

```bash
docker-compose down -v
```

## 🌐 访问服务

### Web 聊天界面
- **地址**: http://localhost:8086/chat.html
- **说明**: 打开多个浏览器窗口进行多用户聊天测试

### 监控和追踪
- **Jaeger UI**: http://localhost:16686
- **Metrics**:
  - Controller: http://localhost:9090/metrics
  - Connect-Node-1: http://localhost:9091/metrics
  - Connect-Node-2: http://localhost:9092/metrics
  - Push-Manager: http://localhost:9095/metrics

## 📝 使用 Web 聊天室

### 1. 打开聊天页面
在浏览器中访问: http://localhost:8086/chat.html

### 2. 多用户测试

#### 窗口 1 (Alice)
```
用户 ID: user-001
用户名: Alice
房间 ID: room-001
```

#### 窗口 2 (Bob)
```
用户 ID: user-002
用户名: Bob
房间 ID: room-001
```

#### 窗口 3 (Charlie)
```
用户 ID: user-003
用户名: Charlie
房间 ID: room-001
```

### 3. 开始聊天
- 所有在同一房间的用户都能看到彼此的消息
- 消息会实时推送到所有在线用户
- 自己的消息显示在右侧（紫色气泡）
- 他人的消息显示在左侧（白色气泡）

## 🏗️ 架构说明

### WebSocket 连接
```
浏览器 → ws://localhost:8083/connect
    ↓
Connect-Node-1
    ↓
Room 过滤 + Op 订阅
    ↓
实时消息推送
```

### 消息发送流程
```
浏览器 → HTTP POST localhost:8082/broadcast
    ↓
Biz-Server
    ↓
Push-Manager (通过 ETCD 服务发现)
    ↓
所有 Connect-Node (广播)
    ↓
Room 过滤 (只发送给匹配房间的客户端)
    ↓
WebSocket 推送到浏览器
```

### 负载均衡
- 3 个 Connect-Node 实例（端口 8083, 8084, 8085）
- 客户端可以连接到任意一个 Connect-Node
- 消息会通过 Push-Manager 广播到所有节点

## 🔧 配置说明

### 本地开发 vs Docker 部署

Web 应用会自动检测环境：

**本地开发**:
- WebSocket: `ws://localhost:8083/connect`
- API: `http://localhost:8082`

**Docker 部署**:
- WebSocket: `ws://<your-host>:8083/connect`
- API: `http://<your-host>:8082`

配置文件位于: `web/config.js`

### 端口映射

| 服务 | 容器内端口 | 宿主机端口 |
|------|-----------|-----------|
| Connect-Node-1 | 8083 | 8083 |
| Connect-Node-2 | 8083 | 8084 |
| Connect-Node-3 | 8083 | 8085 |
| Biz-Server | 8082 | 8082 |
| Web-Server | 8086 | 8086 |

## 🐛 故障排查

### 服务无法启动
```bash
# 检查服务状态
docker-compose ps

# 查看详细日志
docker-compose logs <service-name>

# 重启特定服务
docker-compose restart <service-name>
```

### 数据库连接失败
```bash
# 检查 MySQL 是否健康
docker-compose ps mysql

# 查看 MySQL 日志
docker-compose logs mysql

# 重新初始化数据库
docker-compose down -v
docker-compose up -d mysql
```

### ETCD 连接失败
```bash
# 检查 ETCD 健康状态
docker-compose exec etcd etcdctl endpoint health

# 查看 ETCD 中注册的服务
docker-compose exec etcd etcdctl get --prefix /services/
```

### WebSocket 连接失败
1. 检查 Connect-Node 是否运行: `docker-compose ps connect-node-1`
2. 检查端口是否可访问: `curl http://localhost:8083`
3. 查看 Connect-Node 日志: `docker-compose logs connect-node-1`
4. 确认防火墙没有阻止 8083 端口

### 消息发送失败
1. 检查 Biz-Server 是否运行: `docker-compose ps biz-server`
2. 测试 API: `curl -X POST http://localhost:8082/broadcast -H "Content-Type: application/json" -d '{"room_id":"room-001","message":"test"}'`
3. 查看 Push-Manager 日志: `docker-compose logs push-manager`

## 📊 监控和调试

### 查看系统资源使用
```bash
docker stats
```

### 进入容器调试
```bash
# 进入 Connect-Node 容器
docker-compose exec connect-node-1 sh

# 进入 Biz-Server 容器
docker-compose exec biz-server sh
```

### 查看网络连接
```bash
# 查看网络列表
docker network ls

# 查看网络详情
docker network inspect pubsub_pubsub-network
```

## 🎯 性能测试

### 使用多个浏览器窗口
1. 打开 10+ 个浏览器标签页
2. 每个标签页使用不同的用户 ID
3. 加入相同的房间
4. 测试消息广播性能

### 使用压测工具
```bash
# 安装 websocat (WebSocket 测试工具)
# 连接测试
websocat ws://localhost:8083/connect?user_id=test&user_name=test&room_id=room-001

# 使用 ab 测试 HTTP API
ab -n 1000 -c 10 -p data.json -T application/json http://localhost:8082/broadcast
```

## 🔐 生产环境建议

1. **环境变量**: 使用 `.env` 文件管理敏感配置
2. **SSL/TLS**: 使用 Nginx 反向代理提供 HTTPS
3. **资源限制**: 在 docker-compose.yml 中添加 CPU 和内存限制
4. **日志**: 配置日志轮转和持久化存储
5. **备份**: 定期备份 MySQL 和 ETCD 数据
6. **监控**: 集成 Prometheus + Grafana

## 📚 相关文档

- [系统架构](../ARCHITECTURE.md)
- [API 文档](../API.md)
- [Web 客户端文档](../web/README.md)

## 🎉 开始使用吧！

现在您可以通过 Docker Compose 一键部署整个 PubSub 聊天系统了！


