# Story 6.1: 停机发送安全与队列关闭保护

Status: review

## Story

As a 连接层稳定性负责人,  
I want `Stop()` 与 `EnqueueLeave/BroadcastRoom` 在关闭竞态下具备 send-safe 保护,  
so that 服务停机或灰度切换时不会触发 `send on closed channel` panic。

## Acceptance Criteria

1. `Stop()` 与 `EnqueueLeave`、`BroadcastRoom` 并发发生时，不得出现 `send on closed channel` panic。  
2. worker 队列进入关闭态后，调用方必须收到明确失败语义，不能静默吞掉停机中拒绝。  
3. 关闭中失败必须有统一 metrics/logging 口径，可区分 `queue_full` 与 `stopping/closed`。  
4. 不破坏 Story 3.2/3.3 的“本地解绑先行、Leave 异步执行、有限重试”语义。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过，并新增覆盖停机竞态的回归测试。

## Tasks / Subtasks

- [x] Task 1: 为 leave/broadcast worker 引入显式 stopping/closed 状态（AC: #1, #2, #3）
  - [x] Subtask 1.1: 明确 `Stop()` 前后状态机，禁止“关闭 channel 作为唯一状态信号”
  - [x] Subtask 1.2: 让 `EnqueueLeave` 在 stopping/closed 时返回稳定错误类型
  - [x] Subtask 1.3: 让 `BroadcastRoom` 在 stopping/closed 时返回稳定错误类型

- [x] Task 2: 补齐可观测与错误分类（AC: #2, #3）
  - [x] Subtask 2.1: 为 stopping/closed 拒绝补 metrics 标签或独立 reason
  - [x] Subtask 2.2: 统一日志节流口径，避免停机窗口日志放大

- [x] Task 3: 增加停机竞态回归测试（AC: #1, #4, #5）
  - [x] Subtask 3.1: 覆盖 `Stop()` 与 `EnqueueLeave` 并发
  - [x] Subtask 3.2: 覆盖 `Stop()` 与 `BroadcastRoom` 并发
  - [x] Subtask 3.3: 覆盖 stopping 态错误分类与 metrics 断言

- [x] Task 4: 验证与验收记录（AC: #5）
  - [x] Subtask 4.1: 使用用户级 Go 工具链运行 `go test ./connect-node/...`
  - [x] Subtask 4.2: 使用用户级 Go 工具链运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 来自 connect-node 总体复审的 High finding，优先级最高。  
- 重点不是“避免当前测试失败”，而是把停机路径收敛为可证明安全的并发语义。  
- 直接相关文件：
  - `/mnt/pubsub/connect-node/server.go`
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/connect-node/critical_metrics.go`
- 约束：
  - 保持 Join 同步语义不变
  - 不回退 Leave 异步化与 shared writer 既有架构

## References

- 评审来源：connect-node BMAD-style overall review（High finding #1）
- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 6 / Story 6.1）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`（FR16 对应关闭路径边界，发布灰度约束）
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR16, FR19, FR21, NFR5, NFR7, NFR10）

## Change Log

- 2026-03-13: 基于 connect-node 总体复审结果创建 backlog story，状态设为 `ready-for-dev`。
- 2026-03-15: 完成停机 send-safe 队列状态机、stopping/closed 错误类型与拒绝分类观测，新增停机并发回归测试，状态更新为 `review`。

## Dev Agent Record

### Agent Model Used

Codex (GPT-5) / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.314s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.345s`

### Completion Notes List

- 为 leave/broadcast worker 增加 `running -> stopping -> closed` 显式状态机，`Stop()` 与发送路径通过 `RWMutex` 形成 send-safe 互斥，不再依赖“close channel 即状态”的隐式语义。
- 新增稳定错误类型 `ErrWorkerQueueStopping` / `ErrWorkerQueueClosed`（同时实现 `GRPCStatus`），`EnqueueLeave` 与 `BroadcastRoom` 在停机窗口返回可判定失败语义。
- 增加统一拒绝分类函数 `enqueueRejectReason`，区分 `queue_full`、`stopping`、`closed`；并通过 `recordCriticalEnqueueFailure` 上报 reason。
- 为停机拒绝日志增加 1s 节流窗口，避免停机窗口日志放大。
- 新增停机竞态回归测试：并发 `Stop()+EnqueueLeave`、并发 `Stop()+BroadcastRoom`、拒绝原因分类断言。
- 保持 Story 3.2/3.3 语义：本地解绑先行、Leave 异步队列与有限重试流程未回退。

### File List

- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/6-1-stop-safe-queue-close-and-send-guards.md
- /mnt/pubsub/_bmad-output/implementation-artifacts/sprint-status.yaml
