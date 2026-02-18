# 🚀 PubSub 聊天室 Web 客户端

这是一个基于 WebSocket 和 PubSub 架构的实时聊天室 Web 客户端。

## ✨ 功能特性

- 🎨 现代化 UI 设计
- 💬 实时消息推送
- 🏠 多房间支持
- 👥 多用户聊天
- 💓 自动心跳保活
- 📱 响应式布局

## 🚀 快速开始

### 1. 启动所有后端服务

确保以下服务已启动：
- ETCD
- Controller-Manager (端口 50051)
- Push-Manager (端口 50053)
- Connect-Node (端口 8083)
- Biz-Server (端口 8082)

### 2. 启动 Web 服务器

```bash
cd web
go run main.go
```

Web 服务器将在 `http://localhost:8084` 启动。

### 3. 打开聊天页面

在浏览器中打开：`http://localhost:8084/chat.html`

### 4. 开始聊天

1. **输入用户信息**：
   - 用户 ID：例如 `user-001`、`user-002`
   - 用户名：例如 `Alice`、`Bob`
   - 房间 ID：例如 `room-001`

2. **点击"加入房间"**

3. **开始发送消息**

## 📝 测试多用户聊天

### 方法 1：多个浏览器窗口

1. 打开多个浏览器窗口（或使用隐私模式）
2. 在每个窗口中使用不同的用户 ID 和昵称
3. 加入相同的房间
4. 开始聊天！

### 方法 2：多个浏览器

1. 使用 Chrome、Firefox、Edge 等不同浏览器
2. 在每个浏览器中打开聊天页面
3. 使用不同的用户信息登录
4. 开始聊天！

## 🎯 示例场景

### 场景 1：Alice 和 Bob 在 room-001 聊天

**窗口 1 (Alice)**：
- 用户 ID: `user-001`
- 用户名: `Alice`
- 房间 ID: `room-001`

**窗口 2 (Bob)**：
- 用户 ID: `user-002`
- 用户名: `Bob`
- 房间 ID: `room-001`

### 场景 2：多个用户在同一房间

**窗口 1 (Charlie)**：
- 用户 ID: `user-003`
- 用户名: `Charlie`
- 房间 ID: `room-001`

**窗口 2 (David)**：
- 用户 ID: `user-004`
- 用户名: `David`
- 房间 ID: `room-001`

**窗口 3 (Eve)**：
- 用户 ID: `user-005`
- 用户名: `Eve`
- 房间 ID: `room-001`

## 🏗️ 技术架构

```
浏览器客户端
    ↓ WebSocket (ws://localhost:8083/connect)
Connect-Node
    ↓ Room 过滤 + Op 订阅
Bucket → Channel
    ↓ Push
客户端接收消息
```

## 📡 消息流程

### 发送消息
```
用户输入 → HTTP POST /broadcast (Biz-Server)
    ↓
Push-Manager (广播到所有 Connect-Node)
    ↓
Connect-Node (Room 过滤)
    ↓
WebSocket 推送到房间内的所有客户端
```

### 接收消息
```
Connect-Node 推送
    ↓
WebSocket onmessage
    ↓
Protobuf 解码
    ↓
UI 渲染
```

## 🎨 UI 特性

- **渐变色主题**：紫色渐变设计
- **消息气泡**：左右对齐，区分自己和他人
- **实时状态**：连接状态指示灯
- **动画效果**：消息滑入动画
- **响应式设计**：适配各种屏幕尺寸
- **优雅滚动**：自定义滚动条样式

## 🔧 配置说明

### WebSocket 连接
- 默认地址：`ws://localhost:8083/connect`
- 修改位置：`chat.html` 中的 `wsUrl`

### HTTP API
- 默认地址：`http://localhost:8082/broadcast`
- 修改位置：`chat.html` 中的 `fetch` URL

### Web 服务器端口
- 默认端口：`8084`
- 修改位置：`main.go` 中的 `ListenAndServe`

## 🐛 故障排查

### 连接失败
1. 检查 Connect-Node 是否运行在 8083 端口
2. 检查浏览器控制台错误信息
3. 确认防火墙没有阻止连接

### 消息发送失败
1. 检查 Biz-Server 是否运行在 8082 端口
2. 检查浏览器网络面板的 POST 请求
3. 确认 CORS 配置正确

### 收不到消息
1. 检查是否在同一个房间
2. 检查 Connect-Node 日志的广播记录
3. 确认客户端已订阅 op=2 消息

## 📚 相关文档

- [系统架构文档](../ARCHITECTURE.md)
- [API 文档](../API.md)
- [协议文档](../protocol/README.md)

## 🎉 享受聊天吧！

现在您可以和朋友们一起在 PubSub 聊天室中愉快地聊天了！



