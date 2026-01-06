# OpenTelemetry 和 Metrics 集成指南

## ✅ 已完成的集成

Controller Manager 现已完全集成 **OpenTelemetry** 链路追踪和 **Metrics** 指标收集！

## 🔭 OpenTelemetry 链路追踪

### 功能特性

1. **自动 gRPC 拦截器**
   - 自动为所有 gRPC 请求创建 span
   - 自动传播 trace context
   - 自动记录错误和状态

2. **手动 Span 创建**
   - 每个关键方法都有独立的 span
   - 支持添加自定义属性
   - 支持记录事件

3. **分布式追踪**
   - 跨服务的 trace 传播
   - 完整的调用链路可视化

### 已追踪的方法

| 方法 | Span 名称 | 追踪信息 |
|------|-----------|---------|
| NotifyUserOnline | `Controller.NotifyUserOnline` | user_id, node_id, room_id |
| NotifyUserOffline | `Controller.NotifyUserOffline` | user_id, room_id |
| JoinRoom | `Controller.JoinRoom` | user_id, user_name, room_id, node_id, user_count |
| RegisterNode | `Controller.RegisterNode` | node_id, target |

### Trace 示例

```
Controller.NotifyUserOnline
  ├─ saving_user_to_redis (event)
  └─ Controller.JoinRoom (child span)
      ├─ saving_room_to_redis (event)
      └─ success
```

### 代码示例

```go
// 创建 span
ctx, span := tracing.StartSpan(ctx, "Controller.JoinRoom")
defer span.End()

// 添加属性
tracing.AddSpanAttributes(ctx,
    tracing.AttrUserID.String(req.UserId),
    tracing.AttrRoomID.String(req.RoomId),
)

// 添加事件
tracing.AddSpanEvent(ctx, "saving_room_to_redis")

// 记录错误
if err != nil {
    tracing.RecordError(ctx, err)
    return err
}

// 标记成功
tracing.SetSpanSuccess(ctx)
```

## 📊 Metrics 指标收集

### 支持的指标

#### 1. 房间指标
- **pubsub.rooms.total**: 总房间数（UpDownCounter）
- **pubsub.room.user_count**: 每个房间的用户数（Gauge）

#### 2. 用户指标
- **pubsub.users.total**: 在线用户总数（UpDownCounter）

#### 3. 节点指标
- **pubsub.nodes.total**: 在线节点总数（UpDownCounter）

#### 4. API 指标
- **pubsub.api.requests.total**: API 请求总数（Counter）
  - 标签: `method`, `success`
- **pubsub.api.errors.total**: API 错误总数（Counter）
  - 标签: `method`

### Metrics 更新时机

| 事件 | 更新的指标 |
|------|-----------|
| 用户上线 | `users.total +1`, `api.requests.total +1` |
| 用户下线 | `users.total -1`, `api.requests.total +1` |
| 创建房间 | `rooms.total +1` |
| 删除房间 | `rooms.total -1`, `room.user_count -` |
| 加入房间 | `room.user_count +`, `api.requests.total +1` |
| 离开房间 | `room.user_count -`, `api.requests.total +1` |
| 节点注册 | `nodes.total +1`, `api.requests.total +1` |

### 代码示例

```go
// 增加用户数
s.metrics.IncrementUsers(ctx, 1)

// 减少房间数
s.metrics.DecrementRooms(ctx, 1)

// 设置房间用户数
s.metrics.SetRoomUserCount(roomID, int64(userCount))

// 记录 API 请求
s.metrics.RecordAPIRequest(ctx, "JoinRoom", true)

// 获取当前统计
roomCount := s.metrics.GetCurrentRooms()
userCount := s.metrics.GetCurrentUsers()
nodeCount := s.metrics.GetCurrentNodes()
```

## 🚀 使用示例

### 1. 启动 Controller（已集成 OTEL + Metrics）

```bash
cd controller-manager
go run . controller-1 50051
```

你会看到：

```
🔭 初始化 OpenTelemetry...
✅ OpenTelemetry 初始化成功

📊 创建 Metrics Collector...
✅ Metrics Collector 创建成功

📋 服务信息:
  - OpenTelemetry: enabled
  - Metrics: enabled
```

### 2. 发起请求，查看 Trace

```bash
# 加入房间
grpcurl -plaintext -d '{
  "user_id": "user-1",
  "room_id": "room-001",
  "user_name": "Alice",
  "node_id": "node-1"
}' localhost:50051 pubsub.ControllerService/JoinRoom
```

**Trace 输出**（stdout）：
```json
{
  "Name": "Controller.JoinRoom",
  "SpanContext": {
    "TraceID": "...",
    "SpanID": "..."
  },
  "Attributes": [
    {"Key": "user.id", "Value": "user-1"},
    {"Key": "room.id", "Value": "room-001"},
    {"Key": "user.count", "Value": 1}
  ],
  "Events": [
    {"Name": "saving_room_to_redis"}
  ],
  "Status": {"Code": "Ok"}
}
```

### 3. 查看统计信息

每 30 秒自动打印：

```
============================================================
📊 统计信息
============================================================
🏠 房间: 2 个, 👥 用户: 5 人
📈 Metrics - Rooms: 2, Users: 5, Nodes: 1

房间详情:
  - room-001: 3 人 (创建于 14:30:15)
  - room-002: 2 人 (创建于 14:32:20)

🖥️  在线节点: 1 个
============================================================
```

## 📈 导出 Metrics（可选）

### 切换到 Prometheus Exporter

当前使用 stdout 导出器，生产环境建议使用 Prometheus：

```go
// pkg/tracing/tracing.go
import (
    "go.opentelemetry.io/otel/exporters/prometheus"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// 创建 Prometheus exporter
exporter, err := prometheus.New()
if err != nil {
    return err
}

// 创建 MeterProvider
mp := sdkmetric.NewMeterProvider(
    sdkmetric.WithReader(exporter),
)
otel.SetMeterProvider(mp)
```

### Prometheus 配置

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'pubsub-controller'
    static_configs:
      - targets: ['localhost:9090']
```

## 🔍 查看 Jaeger Traces（可选）

### 1. 启动 Jaeger

```bash
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 14268:14268 \
  jaegertracing/all-in-one:latest
```

### 2. 修改 Tracing 导出器

```go
// pkg/tracing/tracing.go
import (
    "go.opentelemetry.io/otel/exporters/jaeger"
)

// 使用 Jaeger exporter
exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(
    jaeger.WithEndpoint("http://localhost:14268/api/traces"),
))
```

### 3. 访问 Jaeger UI

浏览器打开: http://localhost:16686

可以看到：
- 所有服务的调用链路
- 每个请求的时间线
- 跨服务的依赖关系
- 错误追踪

## 🎯 自定义 Attributes

### 预定义的 Attributes

```go
// pkg/tracing/tracing.go
var (
    AttrRoomID    = attribute.Key("room.id")
    AttrUserID    = attribute.Key("user.id")
    AttrUserName  = attribute.Key("user.name")
    AttrNodeID    = attribute.Key("node.id")
    AttrUserCount = attribute.Key("user.count")
    AttrRoomCount = attribute.Key("room.count")
    AttrOperation = attribute.Key("operation")
    AttrSuccess   = attribute.Key("success")
    AttrSource    = attribute.Key("source")
    AttrTarget    = attribute.Key("target")
)
```

### 使用示例

```go
tracing.AddSpanAttributes(ctx,
    tracing.AttrRoomID.String("room-001"),
    tracing.AttrUserCount.Int(5),
    tracing.AttrSuccess.Bool(true),
)
```

## 🔧 性能优化

### 1. Sampling（采样）

```go
// 100% 采样（开发环境）
sdktrace.AlwaysSample()

// 10% 采样（生产环境）
sdktrace.TraceIDRatioBased(0.1)

// 父级采样
sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))
```

### 2. Batching（批量导出）

```go
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter,
        sdktrace.WithMaxQueueSize(2048),
        sdktrace.WithMaxExportBatchSize(512),
        sdktrace.WithBatchTimeout(5 * time.Second),
    ),
)
```

### 3. Resource 优化

```go
res, err := resource.New(ctx,
    resource.WithAttributes(
        semconv.ServiceName("pubsub-controller"),
        semconv.ServiceVersion("1.0.0"),
        semconv.DeploymentEnvironment("production"),
        attribute.String("instance.id", controllerID),
    ),
)
```

## 📝 最佳实践

### 1. Span 命名

✅ 好的命名：
- `Controller.JoinRoom`
- `Redis.SaveUser`
- `GRPC.SendMessage`

❌ 不好的命名：
- `ProcessRequest`
- `HandleData`
- `DoWork`

### 2. Attributes 选择

只添加有价值的属性：
- ✅ `room.id`, `user.id`, `user.count`
- ❌ 敏感信息（密码、token）
- ❌ 过大的数据（整个请求体）

### 3. Event vs Attribute

- **Attribute**: 静态信息（ID、名称）
- **Event**: 动态事件（保存到 Redis、发送通知）

### 4. 错误处理

```go
if err != nil {
    tracing.RecordError(ctx, err)  // 记录错误
    span.SetStatus(codes.Error, err.Error())
    return err
}

tracing.SetSpanSuccess(ctx)  // 成功时标记
```

## 🎉 总结

Controller Manager 现在完全支持：
- ✅ OpenTelemetry 分布式追踪
- ✅ Metrics 指标收集
- ✅ 自动 gRPC 拦截
- ✅ 自定义 Span 和 Attributes
- ✅ 实时统计打印

下一步可以：
1. 为其他模块（Connect-Node, Push-Manager）添加同样的集成
2. 切换到 Prometheus + Jaeger
3. 创建 Grafana Dashboard 可视化

---

**完整示例代码**: `controller-manager/controller.go` 和 `controller-manager/main.go`


