# Push-Manager Review

适用范围：[push-manager/](.) 下所有非测试 Go 文件（[main.go](main.go) + [server.go](server.go)，约 650 行）。

按严重级组织：🔴 P0 正确性 / 🟠 P1 稳定性 & 性能 / 🟡 P2 可维护性。

---

## 🔴 P0：必须立即修复

### P0-1. 每个 BroadcastClient 启动 10 个 worker 并发调用同一个 gRPC client stub

- 位置：[server.go:233-247](server.go#L233-L247)
- 现象：
  ```go
  client := push.NewCometClient(conn)       // 共享的 gRPC stub
  routineSize := uint64(10)
  broadcastClient := &BroadcastClient{
      client:        client,  // 所有 worker 共享
      broadcastChan: make(chan *push.BroadcastReq, 1000),
      ...
  }
  for i := uint64(0); i < routineSize; i++ {
      go broadcastClient.runWorker(i)  // 10 个 worker
  }
  ```
  每个 worker 从同一个 chan 读消息，调用同一个 `bc.client.Broadcast(ctx, req, ...)`。
- 问题：
  - gRPC client stub **虽然并发安全**，但内部已有 HTTP/2 连接池和多路复用。
  - **10 个 worker 完全没必要**——单个 worker 就能充分利用 gRPC 的并发能力（HTTP/2 stream multiplexing）。
  - 多个 worker 反而增加：goroutine 数量（100 个节点 = 1000 个 worker）、调度开销、context 分配。
- 建议：**改成 1 个 worker per client**
  ```go
  routineSize := uint64(1)  // 或直接删掉这个字段
  go broadcastClient.runWorker(0)
  ```
  gRPC 内部会自动并发处理多个 RPC 调用（HTTP/2 多路复用）。

### P0-2. shutdown 顺序不完整，watch goroutine 退出后主进程不等待

- 位置：[main.go:194-206](main.go#L194-L206)；[server.go:110-138](server.go#L110-L138)
- 现象：
  ```go
  grpcServer.GracefulStop()  // ① 等所有 RPC handler 返回
  metricsServer.Shutdown()
  cancel()                    // ② 触发 watch goroutine 的 ctx.Done
  // ③ 缺失：没有等 watch goroutine 退出
  log.Println("✅ Push-Manager 已关闭")
  ```
- 影响：
  - `cleanupAllClients()` 还在遍历 map、关闭连接时，进程已经退出。
  - gRPC 连接不优雅关闭，in-flight RPC 被强制中断。
- 修复：**已在代码中实现**（添加 `watchWG.Wait()`）
  ```go
  cancel()
  pushManager.watchWG.Wait()  // 等 cleanupAllClients 完成
  ```
- 关键不变量：`grpcServer.GracefulStop()` 必须先于 `cancel()`，否则关 client 时还有 RPC 在写 chan。

### P0-3. Broadcast / BroadcastToRoom RPC 永远返回 OK，调用方无感知丢弃

- 位置：[server.go:331-344](server.go#L331-L344)
- 现象：
  ```go
  func (s *PushManagerServer) Broadcast(...) (*BroadCastReply, error) {
      s.EnqueueBroadcastMsg(req)
      return &BroadCastReply{Code: "0", Msg: "OK", ...}, nil
  }
  ```
  - `enqueueToAll` 内部 chan 满时走 default 丢弃，只累加 `queueFullDropCount`。
  - RPC 调用方**完全无感知**——即使所有下游都满、消息全丢，仍返回 "OK"。
- 影响：上游业务系统以为消息已成功推送，实际已丢。**对账 / SLA 失效**。
- 建议：
  ```go
  return &BroadCastReply{
      Code: "0",
      Msg:  "OK",
      EnqueuedNodes: successCount,  // 成功 enqueue 节点数
      DroppedNodes:  failedCount,   // 失败节点数
  }
  ```
  调用方根据 `DroppedNodes > 0` 判断是否需要 retry 或告警。

---

## 🟠 P1：稳定性 & 性能

### P1-1. monitor goroutine 30s 后退出，是死代码

- 位置：[server.go:203-224](server.go#L203-L224)
- 现象：每次创建 client 时启动一个 monitor goroutine，循环检查 `conn.GetState()`，READY 后 break，30s 超时也 break。**break 之后什么也不做**。
- 影响：goroutine 短暂泄漏（30s），且 30s 后连接失联也不会感知。
- 建议：删除，或者改成持续监控 + 重连/告警。当前实现纯日志噪音。

### P1-2. ETCD 服务发现事件处理是单 goroutine 串行

- 位置：[server.go:110-138](server.go#L110-L138)
- 现象：每来一个 ETCD event 就 `GetEndpoints()` + `createBroadcastClient(endpoints)`。如果 ETCD 抖动短时间内吐 100 个事件，会串行处理 100 次。
- 影响：ETCD 抖动期间 CPU / 连接数飙升。
- 建议：debounce / coalescing —— 多个 event 在 100ms 窗口内只触发一次 reconcile。

### P1-3. grpc.DialContext 不带 keepalive 配置

- 位置：[server.go:180-198](server.go#L180-L198)
- 现象：只设了 `MaxCallRecvMsgSize`，没有 `keepalive.ClientParameters`。
- 影响：中间网络设备 NAT 表过期（AWS NLB 默认 350s）会导致连接 silently broken，RPC 第一次失败才重连。
- 建议：
  ```go
  grpc.WithKeepaliveParams(keepalive.ClientParameters{
      Time:                30 * time.Second,
      Timeout:             10 * time.Second,
      PermitWithoutStream: true,
  }),
  grpc.WithDefaultServiceConfig(`{"retryPolicy": {...}}`)
  ```

### P1-4. clientsMu RWMutex 完全没必要，应改成 atomic.Pointer

- 位置：当前代码里已经没有 `clientsMu` 了（被之前某次修改删了），但如果有的话：
- RLock 保护的只是"拷贝 map 指针到切片"（约 100ns），出锁后访问指针完全不受保护。
- 建议：改成 `atomic.Pointer[[]*BroadcastClient]` + COW，零锁开销。

### P1-5. BroadcastClient.Close() 不等 worker 退出就关 conn

- 位置：[server.go:317-325](server.go#L317-L325)
- 现象：`Close()` 只调 `cancel()` + `conn.Close()`，不等 worker 退出。
- 影响：正在 RPC 的 worker 遇到 `connection closed` 错误。
- 建议（如果保留多 worker 的话）：
  ```go
  func (bc *BroadcastClient) Close() {
      bc.cancel()          // ① 通知 worker 停
      bc.workerWG.Wait()   // ② 等 worker 退出
      if bc.conn != nil {
          bc.conn.Close()  // ③ 最后关 conn
      }
  }
  ```
  但如果改成 1 个 worker（P0-1），这个问题影响变小。

---

## 🟡 P2：可维护性

### P2-1. routineSize / chan 容量 / worker timeout 全部写死

- 位置：[server.go:227](server.go#L227)、[server.go:232](server.go#L232)、[server.go:278](server.go#L278)
- 建议：从配置读，至少给 ENV 兜底。

### P2-2. queueFullDropCount 只累加不重置

- 位置：[main.go:164-172](main.go#L164-L172)
- 现象：每 10s 打印当前总数，连差值都不算。
- 建议：与 connect-node 对齐，打 5s 增量或接入 Prometheus counter。

### P2-3. pprof 端口写死 6061，且日志前缀写错

- 位置：[main.go:130](main.go#L130)、[main.go:134](main.go#L134)
- 日志前缀是 `[Controller-Manager]`（复制粘贴错误）。
- 建议：从配置读；修正日志前缀。

### P2-4. getEnvAsInt 用 fmt.Sscanf 吃掉错误

- 位置：[main.go:244-252](main.go#L244-L252)
- 解析失败返回默认值，不打日志。
- 建议：解析失败时 log warn。

### P2-5. 没有 auth / mTLS / rate limit

- gRPC 服务全 plaintext，谁连上谁能广播。
- 入口无限流，恶意客户端或失控上游可以瞬间打满所有下游 chan。
- 建议：内部 mTLS 或 token-based auth；入口加 rate limiter。

---

## 验证 / 落地建议

1. **P0-1 修复验证**：改成 1 个 worker 后，用 pprof 看 goroutine 数量应该从 `N*10` 降到 `N`（N = connect-node 数量）。
2. **P0-2 修复验证**：`goleak.VerifyNone(t)` 包住 shutdown 路径，应该不报 goroutine leak。
3. **P0-3 修复验证**：模拟下游 chan 满（所有 connect-node 不可达），观察 reply 中 `DroppedNodes` 字段。
4. **chaos 压测**：50% connect-node 网络断开 + 持续广播，验证 keepalive 能自动恢复（P1-3）、丢弃指标准确（P0-3）。

---

## 总览：优先级

| 优先级 | 编号 | 一句话 | 改动量 |
|---|---|---|---|
| 🔴 P0 | P0-1 | 10 个 worker 改成 1 个 | 1 行 |
| 🔴 P0 | P0-2 | shutdown 加 watchWG.Wait() | 已完成 ✅ |
| 🔴 P0 | P0-3 | Broadcast reply 带丢弃数 | proto + 10 行 |
| 🟠 P1 | P1-1 | 删除 monitor 死代码 | -22 行 |
| 🟠 P1 | P1-3 | grpc 加 keepalive | +5 行 |
| 🟠 P1 | P1-5 | Close 等 worker 退出 | +3 行 |
| 🟡 P2 | 其余 | 配置化 / 日志 / auth | 若干 |
