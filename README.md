# 🚀 PubSub - 高性能实时消息推送系统

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

一个基于 **gRPC + WebSocket** 架构的高性能实时消息推送系统，支持大规模并发连接和实时消息广播。

## ✨ 核心特性

- 🎯 **高性能架构**：基于 gRPC 和 WebSocket，支持百万级并发连接
- 🔄 **水平扩展**：Connect-Node 支持多实例部署，自动负载均衡
- 📡 **实时推送**：毫秒级消息推送，支持房间广播、单用户推送
- 🏗️ **微服务架构**：Controller-Manager、Connect-Node、Push-Manager 解耦设计
- 🔍 **服务发现**：基于 ETCD 的自动服务发现和注册
- 💾 **数据持久化**：Redis + MySQL 双重存储，支持数据恢复
- 📊 **可观测性**：集成 OpenTelemetry 链路追踪和 Prometheus Metrics
- 🐳 **Docker 部署**：一键启动，开箱即用

## 🏗️ 系统架构

```
┌─────────────┐
│   Browser   │ (WebSocket)
└──────┬──────┘
       │
       ▼
┌─────────────────┐      ┌──────────────────┐
│  Connect-Node   │◄────►│ Controller-Mgr   │
│  (WebSocket)    │ gRPC │ (房间/用户管理)   │
└──────┬──────────┘      └──────────────────┘
       │                          │
       │                          │
       │                    ┌─────┴─────┐
       │                    │   MySQL    │
       │                    │   Redis    │
       │                    └────────────┘
       │
       ▼
┌─────────────────┐      ┌──────────────────┐
│  Push-Manager   │◄────►│   Biz-Server     │
│  (消息路由)     │ gRPC │  (业务逻辑)       │
└─────────────────┘      └──────────────────┘
       │
       │ (通过 ETCD 发现)
       │
  ┌────┴────┬────────┐
  ▼         ▼        ▼
Connect  Connect  Connect
Node-1   Node-2   Node-3
```

### 核心组件

| 组件 | 职责 | 端口 | 状态 |
|------|------|------|------|
| **Controller-Manager** | 房间/用户管理、节点注册 | 50051 | ✅ |
| **Connect-Node** | WebSocket 连接管理、消息推送 | 8083 | ✅ |
| **Push-Manager** | 消息路由、节点发现 | 50053 | ✅ |
| **Web-Server** | Web 聊天界面、HTTP API | 8086 | ✅ |
| **Biz-Server** | 业务逻辑示例 | 8082 | ✅ |

## 🚀 快速开始

### 方式 1: Docker Compose（推荐）

```bash
# 1. 克隆项目
git clone <repository-url>
cd pubsub

# 2. 构建并启动所有服务
docker-compose up -d

# 3. 查看服务状态
docker-compose ps

# 4. 访问 Web 界面
# 打开浏览器: http://localhost:8086/chat.html
```

### 方式 2: 使用 Makefile

```bash
# 构建所有镜像
make build-images

# 启动所有服务
make start

# 查看服务状态
make ps

# 查看日志
make logs
```

### 方式 3: 本地开发

```bash
# 1. 启动基础服务
docker-compose up -d mysql redis etcd

# 2. 运行各个服务
# Terminal 1: Controller-Manager
cd controller-manager && go run main.go

# Terminal 2: Connect-Node
cd connect-node && go run main.go

# Terminal 3: Push-Manager
cd push-manager && go run main.go

# Terminal 4: Web-Server
cd web && go run main.go
```

## 📂 项目结构

```
pubsub/
├── controller-manager/      # 控制器服务（房间/用户管理）
│   ├── main.go
│   ├── controller.go
│   └── config.yaml
├── connect-node/           # 连接节点（WebSocket 服务器）
│   ├── main.go
│   ├── server.go
│   ├── server_websocket.go
│   ├── bucket.go
│   ├── channel.go
│   └── config.yaml
├── push-manager/           # 推送管理器（消息路由）
│   ├── main.go
│   ├── server.go
│   └── README.md
├── web/                    # Web 服务器（聊天界面）
│   ├── main.go
│   ├── chat.html
│   └── config.js
├── biz-server/             # 业务服务器示例
│   ├── main.go
│   └── example_client.go
├── pkg/                    # 公共包
│   ├── config/             # 配置管理
│   ├── etcd/               # ETCD 服务发现
│   ├── redis/              # Redis 客户端
│   ├── database/           # MySQL 数据库
│   ├── getty/              # WebSocket 编解码
│   ├── metrics/            # Metrics 收集
│   └── tracing/            # 链路追踪
├── protocol/               # Protocol Buffers 定义
│   ├── controller/
│   ├── push/
│   └── protocol/
├── docker-compose.yml      # Docker Compose 配置
├── Dockerfile.*           # 各服务 Dockerfile
└── Makefile               # 构建脚本
```

## 🎯 核心功能

### 1. 实时消息推送

- **房间广播**：向房间内所有用户推送消息
- **单用户推送**：向指定用户推送消息
- **全局广播**：向所有在线用户推送消息

### 2. 连接管理

- **WebSocket 长连接**：支持百万级并发连接
- **自动重连**：客户端断线自动重连
- **心跳保活**：定期心跳检测连接状态

### 3. 房间管理

- **动态创建**：房间自动创建和销毁
- **用户管理**：用户加入/离开房间
- **状态同步**：房间状态实时同步到 Redis

### 4. 服务发现

- **自动注册**：服务启动自动注册到 ETCD
- **动态发现**：自动发现其他服务实例
- **健康检查**：自动移除不健康节点

## 🔧 配置说明

### 环境变量

所有服务支持通过环境变量配置，优先级：**环境变量 > YAML 配置 > 默认值**

```bash
# Controller-Manager
CONTROLLER_ID=controller-1
GRPC_PORT=50051
DB_HOST=mysql
DB_PORT=3306
DB_USER=pubsub
DB_PASSWORD=pubsub123
REDIS_ADDR=redis:6379
ETCD_ENDPOINTS=etcd:2379

# Connect-Node
NODE_ID=connect-node-1
HTTP_PORT=8083
GRPC_PORT=50052
CONTROLLER_ADDRESS=controller:50051

# Push-Manager
MANAGER_ID=push-manager-1
GRPC_PORT=50053
ETCD_ENDPOINTS=etcd:2379

# Web-Server
WEB_PORT=8086
PUSH_MANAGER_ADDR=push-manager:50053
```

### YAML 配置

各服务目录下的 `config.yaml` 支持环境变量替换：

```yaml
database:
  host: ${DB_HOST:localhost}
  port: ${DB_PORT:3306}
```

详细配置说明请查看各服务的 README：
- [Controller-Manager 配置](controller-manager/README.md)
- [Connect-Node 配置](connect-node/config.yaml)
- [Push-Manager 配置](push-manager/README.md)

## 📊 监控和可观测性

### Metrics

所有服务暴露 Prometheus Metrics：

- Controller-Manager: `http://localhost:9090/metrics`
- Connect-Node: `http://localhost:9091/metrics`
- Push-Manager: `http://localhost:9093/metrics`

### 链路追踪

集成 OpenTelemetry，支持 Jaeger：

- Jaeger UI: `http://localhost:16686`

### 健康检查

```bash
# Controller-Manager
curl http://localhost:9090/health

# Connect-Node
curl http://localhost:8083/health

# Web-Server
curl http://localhost:8086/health
```

## 🧪 测试

### Web 界面测试

1. 打开浏览器访问：`http://localhost:8086/chat.html`
2. 在多个浏览器窗口中使用不同用户 ID 登录
3. 加入相同房间，开始聊天

### gRPC 测试

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 测试 Controller-Manager
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 controller.ControllerService/GetRoomStats

# 测试 Push-Manager
grpcurl -plaintext localhost:50053 list
```

## 📚 文档

### 各组件文档

- [Controller-Manager](controller-manager/README.md) - 控制器服务文档
- [Connect-Node](connect-node/) - 连接节点文档
- [Push-Manager](push-manager/README.md) - 推送管理器文档
- [Web-Server](web/README.md) - Web 服务器文档

## 💻 技术栈

- **语言**: Go 1.21+
- **通信协议**: gRPC, WebSocket
- **服务发现**: ETCD
- **数据存储**: MySQL, Redis
- **消息编码**: Protocol Buffers
- **WebSocket 框架**: Getty
- **可观测性**: OpenTelemetry, Prometheus
- **容器化**: Docker, Docker Compose

## 🎓 关键设计

### 1. 零拷贝优化

- WebSocket 消息处理采用零拷贝设计
- Ring Buffer 复用，减少内存分配
- 直接引用 buffer 内存，避免数据拷贝

### 2. 水平扩展

- Connect-Node 支持多实例部署
- 通过 ETCD 自动发现和负载均衡
- 消息按节点分组，优化推送效率

### 3. 数据一致性

- Redis 作为缓存层，MySQL 作为持久化层
- 启动时从 Redis 恢复数据
- 内存和 Redis 自动同步

### 4. 高可用性

- 服务自动注册和发现
- 节点健康检查
- 自动故障转移

## 🔍 故障排查

### 常见问题

1. **WebSocket 连接失败**
   - 检查 Connect-Node 是否运行在 8083 端口
   - 查看浏览器控制台错误信息
   - 检查防火墙设置

2. **消息收不到**
   - 确认用户在同一房间
   - 检查 Push-Manager 日志
   - 验证 Connect-Node 是否正确注册

3. **服务无法启动**
   - 检查依赖服务（MySQL、Redis、ETCD）是否运行
   - 查看服务日志：`docker logs <container-name>`
   - 验证配置是否正确

### 查看日志

```bash
# 查看所有服务日志
docker-compose logs -f

# 查看特定服务日志
docker logs pubsub-controller-1
docker logs pubsub-connect-node-1
docker logs pubsub-push-manager-1
docker logs pubsub-web-server
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

### 开发流程

1. Fork 项目
2. 创建功能分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📝 许可证

本项目采用 Apache 2.0 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情

## 🙏 致谢

- [Getty](https://github.com/AlexStocks/getty) - WebSocket 框架
- [gRPC](https://grpc.io/) - 高性能 RPC 框架
- [ETCD](https://etcd.io/) - 分布式键值存储

---

**⭐ 如果这个项目对你有帮助，请给个 Star！**
