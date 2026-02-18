# 📡 如何判断广播消息是否发送到客户端

## 检查步骤

### 1️⃣ 检查客户端统计（最快的方法）

```bash
# 实时监控统计日志
watch -n 1 'tail -1 benchmark/stat.log'

# 或者查看最新统计
tail -5 benchmark/stat.log
```

**判断标准：**
- ✅ `recv > 0`：客户端已收到广播消息
- ❌ `recv = 0`：客户端未收到消息，继续检查下面步骤

### 2️⃣ 检查 Web-Server 日志

查看运行 `web/main.go` 的终端，应该看到：

```
📡 收到广播请求: room=room-001, message={"test":1}
✅ 广播成功
```

**如果没有看到这些日志：**
- 检查 Web-Server 是否在运行
- 检查端口是否正确（默认 8086）
- 检查 push_room.go 的 `-addr` 参数是否正确

### 3️⃣ 检查 Push-Manager 日志

查看运行 `push-manager` 的终端，应该看到：

```
📡 [PushManager] 收到 Broadcast 请求: roomID=room-001
🚀 [PushManager] 开始广播到所有 Connect-Node
✅ [PushManager] Broadcast 完成
```

### 4️⃣ 检查 Connect-Node 日志

查看运行 `connect-node` 的终端，应该看到：

```
📡 [ConnectNodeServer] 收到 Broadcast gRPC 请求: op=2, roomId=room-001
🚀 [ConnectNodeServer] 开始广播到 X 个 buckets
📢 [Bucket] Broadcast 被调用: op=2, roomId=room-001, 总channels=X
✅ [Bucket] Broadcast 完成: 成功=X, 跳过(op不匹配)=Y, 跳过(room不匹配)=Z
📤 [ProtoHandler] 收到服务端推送消息: op=2, seq=1, roomId=room-001, bodyLen=9
✅ [ProtoHandler] 服务端推送消息已发送给客户端
```

**关键指标：**
- `成功=X`：X 应该 > 0，表示成功推送给 X 个客户端
- 如果 `成功=0`，可能原因：
  - 客户端没有订阅 op=2（检查是否有 "已订阅消息推送: op=2"）
  - 客户端不在该房间（检查 roomId 是否匹配）
  - 客户端连接已断开

### 5️⃣ 检查客户端详细日志

如果客户端运行时指定了 `-log` 参数：

```bash
tail -f benchmark/client.log | grep "received broadcast"
```

应该看到：
```
[client user-000001] received broadcast: op=2, seq=1, bodyLen=9
```

**注意：** 客户端只打印前 3 条消息，避免日志过多。

## 🔍 常见问题排查

### 问题1：recv=0，但服务端日志显示成功

**可能原因：**
1. 客户端没有订阅 op=2
   - 检查 connect-node 日志是否有 "已订阅消息推送: op=2"
   - 检查客户端是否成功加入房间

2. 客户端连接的房间 ID 不匹配
   - 确认 push_room.go 的 `-room` 参数与客户端加入的房间一致

3. 客户端连接已断开
   - 检查客户端是否还在运行
   - 检查 connect-node 日志是否有连接错误

### 问题2：Web-Server 没有收到请求

**检查：**
```bash
# 测试 Web-Server 是否可访问
curl http://localhost:8086/health

# 手动发送一条广播测试
curl -X POST http://localhost:8086/broadcast \
  -H "Content-Type: application/json" \
  -d '{"room_id": "room-001", "message": "test"}'
```

### 问题3：Push-Manager 未连接

**检查：**
- Web-Server 启动日志应该显示 "✅ Push-Manager 客户端已连接"
- 如果显示 "⚠️ 连接 Push-Manager 失败"，检查：
  - Push-Manager 是否在运行
  - 端口是否正确（默认 50053）
  - 环境变量 `PUSH_MANAGER_ADDR` 是否正确

## 📊 快速检查命令

```bash
# 1. 检查客户端统计
tail -1 benchmark/stat.log

# 2. 检查是否有客户端收到消息
grep "received broadcast" benchmark/client.log | wc -l

# 3. 检查 Web-Server 健康状态
curl http://localhost:8086/health

# 4. 手动发送测试消息
curl -X POST http://localhost:8086/broadcast \
  -H "Content-Type: application/json" \
  -d '{"room_id": "room-001", "message": "test message"}'
```

## ✅ 成功标志

如果一切正常，你应该看到：

1. **客户端统计：** `recv > 0`（每秒收到的消息数）
2. **客户端日志：** 有 "received broadcast" 记录
3. **Connect-Node 日志：** `成功=X` 其中 X > 0
4. **Web-Server 日志：** "✅ 广播成功"


