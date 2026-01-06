# 🏗️ 架构更新说明

## 更新日期
2025-12-27

## 更新内容

### 架构简化：合并 Web-Server 和 Biz-Server

## 之前的架构

```
┌─────────────────────────────────────────────────┐
│              旧架构（2个服务）                    │
├─────────────────────────────────────────────────┤
│                                                  │
│  Web-Server (端口 8086)                         │
│  └─ 功能：静态文件服务器                         │
│     └─ 提供 chat.html, config.js 等            │
│                                                  │
│  Biz-Server (端口 8082)                         │
│  └─ 功能：HTTP API                              │
│     └─ POST /broadcast                          │
│                                                  │
└─────────────────────────────────────────────────┘
```

### 问题
- Biz-Server 实际上只有测试客户端代码，没有 HTTP 服务器实现
- 需要维护两个独立的服务
- 配置复杂（两个端口、两个服务）

## 现在的架构

```
┌─────────────────────────────────────────────────┐
│              新架构（1个服务）                    │
├─────────────────────────────────────────────────┤
│                                                  │
│  Web-Server (端口 8086)                         │
│  ├─ 静态文件服务器                              │
│  │  └─ 提供 chat.html, config.js 等            │
│  │                                              │
│  └─ HTTP API                                    │
│     ├─ POST /broadcast  (广播消息)              │
│     └─ GET  /health     (健康检查)              │
│                                                  │
└─────────────────────────────────────────────────┘
```

### 优势
✅ 只需要一个服务
✅ 配置更简单（只有一个端口 8086）
✅ Web 应用和 API 在同一服务，无需 CORS 跨域
✅ 减少部署复杂度

## 服务端口映射

| 服务 | 端口 | 功能 |
|------|------|------|
| Controller-Manager | 50051 | gRPC 控制管理 |
| Connect-Node-1 | 8083 | WebSocket 连接 |
| Connect-Node-2 | 8084 | WebSocket 连接 |
| Connect-Node-3 | 8085 | WebSocket 连接 |
| Push-Manager | 50053 | gRPC 推送管理 |
| ~~Biz-Server~~ | ~~8082~~ | ❌ **已移除** |
| **Web-Server** | **8086** | **静态文件 + HTTP API** |

## 修改的文件

### 1. `web/main.go`
- ✅ 添加 gRPC 客户端连接到 Push-Manager
- ✅ 添加 `/broadcast` API 端点
- ✅ 添加 `/health` 健康检查端点
- ✅ 保留静态文件服务功能

### 2. `web/config.js`
- ✅ 修改 `API_URL` 从 `8082` 改为 `8086`
- ✅ 添加注释说明 API 和 Web 在同一服务

### 3. `docker-compose.yml`
- ❌ 移除 `biz-server` 服务
- ✅ 更新 `web-server` 服务配置
  - 添加 `PUSH_MANAGER_ADDR` 环境变量
  - 添加 `depends_on: push-manager`

### 4. `Dockerfile.web`
- ✅ 添加项目依赖（protocol、go.mod 等）
- ✅ 修改编译方式以支持 gRPC 依赖

## 消息流程

### 完整的聊天消息流程

```
┌──────────────┐
│   浏览器      │
└──────┬───────┘
       │
       │ 1️⃣ 访问 http://localhost:8086/chat.html
       ↓
┌──────────────────────────────────────┐
│  Web-Server (端口 8086)              │
│  ├─ 返回 HTML/JS 文件                │
│  └─ 加载 config.js (API_URL=8086)   │
└──────────────────────────────────────┘
       │
       │ 2️⃣ 建立 WebSocket 连接
       │    ws://localhost:8083/connect
       ↓
┌──────────────────────────────────────┐
│  Connect-Node (端口 8083)            │
│  └─ 接受客户端连接                   │
└──────────────────────────────────────┘
       │
       │ 3️⃣ 用户发送消息
       │    POST http://localhost:8086/broadcast
       ↓
┌──────────────────────────────────────┐
│  Web-Server (端口 8086)              │
│  └─ /broadcast API 处理              │
│     └─ 调用 Push-Manager gRPC       │
└──────┬───────────────────────────────┘
       │
       │ 4️⃣ gRPC 调用
       ↓
┌──────────────────────────────────────┐
│  Push-Manager (端口 50053)           │
│  └─ 广播到所有 Connect-Node          │
└──────┬───────────────────────────────┘
       │
       │ 5️⃣ 推送消息（带 Room 过滤）
       ↓
┌──────────────────────────────────────┐
│  Connect-Node                        │
│  └─ 通过 WebSocket 推送给客户端      │
└──────┬───────────────────────────────┘
       │
       │ 6️⃣ WebSocket 消息
       ↓
┌──────────────┐
│   浏览器      │
│  └─ 显示消息  │
└──────────────┘
```

## 配置说明

### 本地开发

**启动 Web-Server**:
```bash
cd web
go run main.go
```

**环境变量**（可选）:
```bash
export WEB_PORT=8086
export PUSH_MANAGER_ADDR=localhost:50053
```

**访问**:
- Web 页面: http://localhost:8086/chat.html
- API: http://localhost:8086/broadcast
- 健康检查: http://localhost:8086/health

### Docker 部署

**启动所有服务**:
```bash
docker-compose up --build
```

**Web-Server 环境变量**（在 docker-compose.yml 中）:
```yaml
environment:
  - WEB_PORT=8086
  - PUSH_MANAGER_ADDR=push-manager:50053
```

**访问**:
- Web 页面: http://localhost:8086/chat.html
- API: http://localhost:8086/broadcast

## 测试验证

### 1. 测试静态文件服务
```bash
curl http://localhost:8086/chat.html
```

### 2. 测试健康检查
```bash
curl http://localhost:8086/health
```

### 3. 测试广播 API
```bash
curl -X POST http://localhost:8086/broadcast \
  -H "Content-Type: application/json" \
  -d '{"room_id":"room-001","message":"Hello from API!"}'
```

### 4. 测试完整聊天流程
1. 打开两个浏览器窗口
2. 访问 http://localhost:8086/chat.html
3. 使用不同用户登录同一房间
4. 发送消息，验证双方都能收到

## 迁移指南

如果之前使用 Biz-Server（端口 8082），需要：

### 1. 更新客户端配置
```javascript
// 之前
API_URL: 'http://localhost:8082'

// 现在
API_URL: 'http://localhost:8086'
```

### 2. 更新防火墙规则
- ❌ 移除端口 8082
- ✅ 确保端口 8086 可访问

### 3. 更新负载均衡器配置
如果使用了负载均衡器：
- 移除 `biz-server:8082` 后端
- Web-Server 现在处理所有 HTTP 请求

## 总结

这次更新简化了架构，将 Web-Server 和 Biz-Server 合并为一个服务，减少了：
- ✅ 1 个服务（从 2 个减少到 1 个）
- ✅ 1 个端口（只需 8086）
- ✅ 1 个 Dockerfile（不需要 Dockerfile.biz-server）
- ✅ 配置复杂度降低

同时保持了所有功能：
- ✅ Web 聊天界面
- ✅ HTTP 广播 API
- ✅ WebSocket 实时通信
- ✅ 房间消息过滤

🎉 架构更简单，功能完整！


