# Biz-Server 客户端使用指南

这个目录包含两个示例客户端，用于演示如何与 PubSub 系统交互。

## 客户端说明

### 1. WebSocket 客户端 (`websocket_client.go`)
- **功能**：连接到 Connect-Node，加入房间，接收实时消息
- **协议**：WebSocket + Protocol Buffers
- **用途**：模拟终端用户连接

### 2. gRPC 客户端 (`grpc_client.go`)
- **功能**：调用 Push-Manager 的 gRPC 接口，发送广播消息
- **协议**：gRPC
- **用途**：模拟业务服务器推送消息

## 编译

```bash
cd /home/yonhoo/pubsub/biz-server
go build -o biz-client .
```

## 使用方法

### 模式 1：只运行 WebSocket 客户端

连接到 Connect-Node，加入房间并监听消息：

```bash
./biz-client -mode=ws \
  -connect-node="ws://localhost:8083/connect" \
  -user-id="user-001" \
  -user-name="张三" \
  -room-id="room-001"
```

**输出示例**：
```
🔌 连接到 Connect-Node: ws://localhost:8083/connect
   用户: 张三 (user-001)
   房间: room-001
✅ WebSocket 连接成功
🚪 加入房间: room-001
✅ 加入房间请求已发送
👂 开始监听服务器消息...
✅ 加入房间成功
📨 收到推送消息: Hello from server!
```

### 模式 2：只运行 gRPC 客户端

调用 Push-Manager 发送广播消息：

```bash
./biz-client -mode=grpc \
  -push-manager="localhost:50053" \
  -room-id="room-001" \
  -user-id="user-001" \
  -message="系统通知：服务器将在5分钟后维护"
```

**输出示例**：
```
🔌 连接到 Push-Manager: localhost:50053
✅ gRPC 连接成功
📢 广播消息到房间: room-001
   内容: 系统通知：服务器将在5分钟后维护
✅ 广播成功
```

### 模式 3：同时运行两个客户端（推荐）

先连接 WebSocket 监听消息，然后通过 gRPC 发送广播，验证消息推送：

```bash
./biz-client -mode=both \
  -connect-node="ws://localhost:8083/connect" \
  -push-manager="localhost:50053" \
  -user-id="user-001" \
  -user-name="张三" \
  -room-id="room-001" \
  -message="测试广播消息"
```

**输出示例**：
```
1️⃣  创建 WebSocket 客户端...
🔌 连接到 Connect-Node: ws://localhost:8083/connect
✅ WebSocket 客户端已连接并监听消息

2️⃣  创建 gRPC 客户端...
🔌 连接到 Push-Manager: localhost:50053
✅ gRPC 连接成功

3️⃣  通过 gRPC 广播消息...
📢 广播消息到房间: room-001
✅ 消息已发送，WebSocket 客户端应该会收到推送

📨 收到推送消息: 测试广播消息
```

## 完整参数列表

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-mode` | `both` | 运行模式：`ws`、`grpc`、`both` |
| `-connect-node` | `ws://localhost:8083/connect` | Connect-Node WebSocket 地址 |
| `-push-manager` | `localhost:50053` | Push-Manager gRPC 地址 |
| `-user-id` | `user-001` | 用户 ID |
| `-user-name` | `测试用户` | 用户名称 |
| `-room-id` | `room-001` | 房间 ID |
| `-message` | `Hello from Biz-Server!` | 要广播的消息 |

## 使用场景示例

### 场景 1：测试单个用户连接

启动一个 WebSocket 客户端：

```bash
./biz-client -mode=ws -user-id="alice" -user-name="Alice" -room-id="chat-room"
```

### 场景 2：测试多用户房间

在不同终端启动多个客户端：

```bash
# 终端 1
./biz-client -mode=ws -user-id="alice" -user-name="Alice" -room-id="chat-room"

# 终端 2
./biz-client -mode=ws -user-id="bob" -user-name="Bob" -room-id="chat-room"

# 终端 3 - 发送广播
./biz-client -mode=grpc -room-id="chat-room" -message="大家好！"
```

所有在 `chat-room` 的用户都会收到消息。

### 场景 3：测试消息推送

```bash
# 1. 启动 WebSocket 客户端监听
./biz-client -mode=ws -user-id="user-123" -room-id="notifications"

# 2. 在另一个终端发送广播
./biz-client -mode=grpc -room-id="notifications" -message="新订单通知"
```

### 场景 4：压力测试

使用脚本启动多个客户端：

```bash
#!/bin/bash
for i in {1..100}; do
  ./biz-client -mode=ws \
    -user-id="user-$i" \
    -user-name="User$i" \
    -room-id="stress-test" &
done

# 等待所有客户端连接
sleep 5

# 发送广播消息
./biz-client -mode=grpc \
  -room-id="stress-test" \
  -message="压力测试消息"
```

## 协议说明

### WebSocket 消息格式

使用 Protocol Buffers，消息包格式：

```
[4字节包长度] + [Proto消息体]
```

**Proto 定义** (`protocol/protocol/protocol.proto`):

```protobuf
message Proto {
  int32 ver = 1;      // 协议版本
  int32 op = 2;       // 操作类型
  uint32 seq = 3;     // 序列号
  string roomid = 4;  // 房间ID
  string userid = 5;  // 用户ID
  bytes body = 6;     // 消息体
}
```

**操作类型 (op)**:
- `1`: 加入房间
- `2`: 加入房间响应
- `3`: 服务器推送消息
- `4`: 广播消息
- `5`: 心跳

### gRPC 接口

使用 Controller Service 的接口（示例）：

```protobuf
service ControllerService {
  rpc GetRoomInfo(GetRoomInfoRequest) returns (GetRoomInfoResponse);
  rpc GetUserNode(GetUserNodeRequest) returns (GetUserNodeResponse);
  rpc GetRoomStats(GetRoomStatsRequest) returns (GetRoomStatsResponse);
}
```

## 故障排查

### 问题 1：WebSocket 连接失败

```
❌ 连接失败: dial tcp [::1]:8083: connect: connection refused
```

**解决方案**：
1. 确保 Connect-Node 正在运行：
   ```bash
   docker-compose ps | grep connect-node
   ```
2. 检查端口映射是否正确（8083 -> 8080）
3. 尝试使用 `127.0.0.1` 而不是 `localhost`

### 问题 2：gRPC 连接失败

```
❌ 连接失败: context deadline exceeded
```

**解决方案**：
1. 确保 Push-Manager 正在运行：
   ```bash
   docker-compose ps | grep push-manager
   ```
2. 检查端口 50053 是否开放
3. 查看 Push-Manager 日志：
   ```bash
   docker-compose logs push-manager
   ```

### 问题 3：消息收不到

**检查清单**：
1. ✅ WebSocket 客户端是否成功加入房间
2. ✅ gRPC 客户端是否发送到正确的房间
3. ✅ Controller 和 ETCD 是否正常运行
4. ✅ 查看各服务日志

## 开发说明

### 添加新功能

1. **添加新的 WebSocket 消息类型**：
   - 在 `protocol/protocol/protocol.proto` 中定义新的 op 类型
   - 在 `websocket_client.go` 的 `handleMessage` 中添加处理逻辑

2. **添加新的 gRPC 接口**：
   - 在 `grpc_client.go` 中添加新方法
   - 调用相应的 gRPC 服务

### 代码结构

```
biz-server/
├── main.go              # 主程序入口
├── websocket_client.go  # WebSocket 客户端实现
├── grpc_client.go       # gRPC 客户端实现
├── example_client.go    # 原始示例（已废弃）
└── CLIENT_USAGE.md      # 本文档
```

## 相关文档

- [系统架构](../README.md)
- [快速开始](../QUICKSTART.md)
- [Protocol Buffers 定义](../protocol/protocol/protocol.proto)
- [Controller API](../protocol/controller/controller.proto)






