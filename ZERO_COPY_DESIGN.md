# 零拷贝（Zero-Copy）设计文档

## 概述

本文档描述了基于 goim 优化策略的零拷贝设计，用于高性能 WebSocket 消息处理。

## 核心思想

**零拷贝（Zero-Copy）**：`Proto.Body` 直接引用 `ReadBuffer` 的内存，避免数据拷贝，减少内存分配和 GC 压力。

**代价**：`ReadBuffer` 会被持续复用，消费者必须在数据被覆盖前处理完。

## 架构图

```
┌─────────┐                 ┌──────────────────┐                ┌────────────────┐
│ Client  │──WebSocket──────▶│ OnMessage        │───Set()────────▶│ CliProto       │
└─────────┘     ▲            │ (读事件)         │   SetAdv()     │ (Ring Buffer)  │
                │            └──────────────────┘   Signal()      └────────────────┘
                │                     │                                   │
                │                     │ *Proto.Body 引用 ReadBuffer       │
                │                     ▼                                   │
                │            ┌──────────────────┐                         │
                │            │ ReadBuffer       │◀────────────────────────┘
                │            │ (零拷贝)         │        Get()
                │            └──────────────────┘        GetAdv() (rp++)
                │                                               │
                │            ┌──────────────────┐              │
                └────────────│ dispatchWebsocket│◀─────────────┘
                 WritePkg    │ (写协程)         │
                             └──────────────────┘
                                     │
                                     │ processClientRequest()
                                     ▼
                             ┌──────────────────┐
                             │ 业务处理 Handler │
                             └──────────────────┘
```

## 数据流

### 1. 读取阶段（OnMessage）

```go
// pkg/getty/codec.go - Read()
func (h *ProtoPackageHandler) Read(ss getty.Session, data []byte) (any, int, error) {
    // 1. 使用 session 级别的 ReadBuffer（不释放）
    bufBytes := h.ReadBuffer.Bytes()
    
    // 2. 解析协议头
    packLen = binary.BigEndian.Uint32(bufBytes[0:4])
    // ... 解析 Ver, Op, Seq, RoomId, UserId ...
    
    // 3. 零拷贝：Body 直接引用 ReadBuffer 内存
    pkg.Body = bufBytes[bodyOffset : bodyOffset+bodyLen]  // ⚠️ 零拷贝！
    
    return &pkg, readLen, nil
}
```

```go
// connect-node/server_websocket.go - OnMessage()
func (h *ProtoMessageHandler) OnMessage(session getty.Session, pkg any) {
    p := pkg.(*protocol.Proto)  // p.Body 引用 ReadBuffer
    
    // 1. Set() 获取 Ring Buffer 中 wp 位置的指针
    cliproto, _ := h.channel.ClientReqQueue.Set()
    
    // 2. 浅拷贝（Body 仍然引用 ReadBuffer）
    *cliproto = *p
    
    // 3. SetAdv() 推进写指针
    h.channel.ClientReqQueue.SetAdv()
    
    // 4. Signal() 通知 dispatchWebsocket
    h.channel.Signal()
}
```

### 2. 处理阶段（dispatchWebsocket）

```go
// connect-node/server_websocket.go - dispatchWebsocket()
func (h *ProtoMessageHandler) dispatchWebsocket(session getty.Session) {
    for {
        // 1. 阻塞等待信号
        signal := h.channel.Ready()
        
        if signal == protocol.ProtoFinish {
            goto close  // 退出
        }
        
        // 2. 从 Ring Buffer 取出所有消息
        for {
            p, err := h.channel.ClientReqQueue.Get()  // 获取 rp 位置的指针
            if err != nil {
                break  // Ring Buffer 空了
            }
            
            // 3. 处理消息（此时 p.Body 仍引用 ReadBuffer）
            h.processClientRequest(session, p)
            
            // 4. ⚠️ GetAdv() 推进读指针（此时 ReadBuffer 可以复用了）
            h.channel.ClientReqQueue.GetAdv()
        }
    }
}
```

## 关键设计点

### 1. ReadBuffer 生命周期

```
Session 创建 ──▶ NewProtoPackageHandler()
                   │
                   ├── ReadBuffer = readPool.Get()  // 获取一次
                   │
                   ▼
               持续使用（零拷贝）
                   │
                   ▼
Session 关闭 ──▶ OnClose() / OnError()
                   │
                   ├── channel.Close()  // 通知 dispatchWebsocket 退出
                   ├── protoPackageHandler.Close()  // 归还 ReadBuffer
                   │
                   ▼
               readPool.Put(ReadBuffer)  // 归还到 pool
```

### 2. Ring Buffer（CliProto）流转

```
              ┌──────────────────────────────────────┐
              │  Ring Buffer (默认 5 个 Proto 对象) │
              │                                      │
              │  [0] [1] [2] [3] [4]                │
              │   │               │                  │
              │   rp (读指针)   wp (写指针)        │
              └──────────────────────────────────────┘
                   │               │
                   │               │
          GetAdv() │               │ SetAdv()
          (rp++)   │               │ (wp++)
                   │               │
         ┌─────────▼───┐   ┌───────▼──────┐
         │ dispatch    │   │ OnMessage    │
         │ Websocket   │   │ (读事件)     │
         └─────────────┘   └──────────────┘
```

**规则：**
- `wp - rp < num`：Ring Buffer 未满，可以继续写入
- `rp == wp`：Ring Buffer 空了，等待新消息
- `wp - rp >= num`：Ring Buffer 满了，丢弃或阻塞

### 3. 零拷贝的风险控制

**风险：** ReadBuffer 被覆盖导致数据损坏

**控制措施：**

1. **Ring Buffer 限流**（默认 5）
   - 限制未处理消息数量
   - 防止读协程远超写协程

2. **快速消费**
   - `processClientRequest` 应尽快处理
   - 不要在处理函数中阻塞或等待

3. **明确的生命周期**
   - `GetAdv()` 之前，数据安全
   - `GetAdv()` 之后，数据可能被覆盖

**示例：安全使用**

```go
// ✅ 安全：立即处理
func (h *ProtoMessageHandler) processClientRequest(session getty.Session, p *protocol.Proto) error {
    // 1. 读取数据
    body := p.Body  // 引用 ReadBuffer
    
    // 2. 立即使用或拷贝
    if needToSave(body) {
        savedBody := make([]byte, len(body))
        copy(savedBody, body)  // 拷贝到独立内存
        go asyncProcess(savedBody)  // 异步处理拷贝
    }
    
    // 3. 发送响应
    resp := &protocol.Proto{...}
    return session.WritePkg(resp, 0)
    
    // 4. 返回后，GetAdv() 被调用，ReadBuffer 可复用
}
```

**示例：危险使用**

```go
// ❌ 危险：异步处理原始数据
func (h *ProtoMessageHandler) processClientRequest(session getty.Session, p *protocol.Proto) error {
    // 直接将 p 传递给异步处理
    go func() {
        time.Sleep(1 * time.Second)  // 模拟慢处理
        process(p.Body)  // ⚠️ p.Body 可能已被覆盖！
    }()
    
    return nil
    // GetAdv() 被调用，ReadBuffer 被复用，p.Body 指向的数据被覆盖
}
```

## 性能优势

### 传统方案（数据拷贝）

```
网络数据 ──copy──▶ ReadBuffer ──copy──▶ Proto.Body ──copy──▶ 业务逻辑
                                    │
                                    ▼
                                3 次内存分配
                                3 次数据拷贝
```

### 零拷贝方案

```
网络数据 ──copy──▶ ReadBuffer ────引用────▶ Proto.Body ──引用──▶ 业务逻辑
                                    │
                                    ▼
                                1 次内存分配
                                1 次数据拷贝
```

**收益：**
- 减少 2 次内存分配
- 减少 2 次数据拷贝
- 降低 GC 压力
- 提升吞吐量

## 配置参数

```yaml
protocol:
  cli_proto: 5          # Ring Buffer 大小（默认 5）
  svr_proto: 10         # Server 推送队列大小

tcp_config:
  reader: 32            # Reader Pool 数量
  read_buf: 100         # 每个 Reader Pool 的 Buffer 数量
  read_buf_size: 8192   # 每个 Buffer 大小（8KB）
```

## 适用场景

**适合：**
- 高并发、低延迟场景
- 消息处理逻辑简单快速
- 对内存分配敏感

**不适合：**
- 消息需要长时间保留
- 异步处理耗时较长
- 业务逻辑复杂、阻塞

## 总结

零拷贝优化是一种**用时间（快速处理）换空间（零拷贝）**的激进优化策略。

在 goim 这种高并发、低延迟的场景下，通过：
1. Ring Buffer 限流
2. 快速消费
3. 明确生命周期

可以有效控制风险，同时获得显著的性能提升。

**核心原则：谁受益，谁负责。** 使用零拷贝的业务代码必须保证在 `GetAdv()` 前处理完数据，或者自行拷贝数据到独立内存。




