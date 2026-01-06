# 🚀 快速启动指南（更新版）

## 架构更新说明

**Web-Server 现在同时提供静态文件和 HTTP API**，不再需要独立的 Biz-Server。

## 本地开发启动

### 启动顺序

```bash
# 1. ETCD（服务发现）
etcd

# 2. Controller-Manager
cd controller-manager && go run *.go

# 3. Connect-Node
cd connect-node && go run *.go

# 4. Push-Manager
cd push-manager && go run *.go

# 5. Web-Server（静态文件 + API）
cd web && go run main.go
```

### 访问地址

- **Web 聊天**: http://localhost:8086/chat.html
- **API**: http://localhost:8086/broadcast
- **健康检查**: http://localhost:8086/health
- **WebSocket**: ws://localhost:8083/connect

## Docker Compose 启动

### 一键启动所有服务

```bash
docker-compose up --build
```

### 访问地址（与本地开发相同）

- **Web 聊天**: http://localhost:8086/chat.html
- **Jaeger UI**: http://localhost:16686
- **Metrics**: http://localhost:9090/metrics

### 查看日志

```bash
# 所有服务
docker-compose logs -f

# 特定服务
docker-compose logs -f web-server
docker-compose logs -f connect-node-1
docker-compose logs -f push-manager
```

### 停止服务

```bash
docker-compose down
```

## 测试验证

### 1. 健康检查

```bash
curl http://localhost:8086/health
```

预期输出：
```json
{"service":"web-server","status":"ok"}
```

### 2. 测试广播 API

```bash
curl -X POST http://localhost:8086/broadcast \
  -H "Content-Type: application/json" \
  -d '{"room_id":"room-001","message":"Hello from API!"}'
```

预期输出：
```json
{"code":"0","msg":"OK","desc":"消息广播成功"}
```

### 3. 多用户聊天测试

1. 打开 3 个浏览器窗口
2. 访问 http://localhost:8086/chat.html
3. 使用不同用户登录：
   - 窗口 1: user-001, Alice, room-001
   - 窗口 2: user-002, Bob, room-001
   - 窗口 3: user-003, Charlie, room-001
4. 任意窗口发送消息
5. 验证所有窗口都能收到

## 服务端口映射

| 服务 | 本地端口 | Docker 端口 | 说明 |
|------|---------|------------|------|
| ETCD | 2379 | 2379 | 服务发现 |
| Controller | 50051 | 50051 | 控制管理 |
| Connect-Node-1 | 8083 | 8083 | WebSocket |
| Connect-Node-2 | - | 8084 | WebSocket |
| Connect-Node-3 | - | 8085 | WebSocket |
| Push-Manager | 50053 | 50053 | 推送管理 |
| **Web-Server** | **8086** | **8086** | **Web + API** |

## 环境变量配置

### Web-Server 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `WEB_PORT` | 8086 | HTTP 服务端口 |
| `PUSH_MANAGER_ADDR` | localhost:50053 | Push-Manager 地址 |

### 设置示例

**本地开发**:
```bash
export WEB_PORT=8086
export PUSH_MANAGER_ADDR=localhost:50053
cd web && go run main.go
```

**Docker**（在 docker-compose.yml 中已配置）:
```yaml
environment:
  - WEB_PORT=8086
  - PUSH_MANAGER_ADDR=push-manager:50053
```

## 常见问题

### 1. Web-Server 连接 Push-Manager 失败

**现象**：
```
⚠️  连接 Push-Manager 失败: ...
⚠️  /broadcast API 将不可用
```

**解决**：
1. 确认 Push-Manager 已启动
2. 检查 `PUSH_MANAGER_ADDR` 配置是否正确
3. 本地开发使用 `localhost:50053`
4. Docker 使用 `push-manager:50053`

### 2. WebSocket 连接失败

**现象**：浏览器控制台显示 WebSocket 连接错误

**解决**：
1. 确认 Connect-Node 已启动在 8083 端口
2. 检查 `web/config.js` 中的 `WS_URL` 配置
3. 确认防火墙没有阻止 8083 端口

### 3. 消息发送失败

**现象**：点击发送按钮后提示"发送消息失败"

**解决**：
1. 打开浏览器开发者工具查看 Network 面板
2. 检查 POST /broadcast 请求是否成功
3. 查看 Web-Server 日志
4. 确认 Push-Manager 日志是否有错误

### 4. 收不到消息

**现象**：消息发送成功但其他用户收不到

**解决**：
1. 确认所有用户在同一个房间（`room_id` 相同）
2. 检查 Connect-Node 日志中的 Room 过滤信息
3. 确认客户端已订阅 `op=2` 消息

## 架构对比

### 旧架构（2个服务）
```
浏览器 → Web-Server (8086) 提供静态文件
浏览器 → Biz-Server (8082) 调用 API
```

### 新架构（1个服务）✅
```
浏览器 → Web-Server (8086) 提供静态文件 + API
```

## 总结

✅ **更简单**：只需一个 Web-Server（端口 8086）
✅ **功能完整**：静态文件 + HTTP API + WebSocket
✅ **易于部署**：本地和 Docker 配置一致
✅ **易于维护**：减少服务数量和配置复杂度

🎉 现在可以愉快地使用了！


