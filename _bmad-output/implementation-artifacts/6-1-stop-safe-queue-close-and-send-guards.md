# Story 6.1: 停机发送安全与队列关闭保护

Status: done

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
- 2026-03-15: Senior Developer Review (AI) 完成；发现阻塞问题，状态由 `review` 回退为 `in-progress`。
- 2026-03-15: 修复 review 阻塞项：`enqueueLeaveTask` 去重分支在 stopping/closed 下返回稳定错误；补充 `recordEnqueueFailure` 的 metrics 上报断言测试，状态更新回 `review`。
- 2026-03-15: Senior Developer Re-Review (AI) 完成；阻塞项关闭，状态由 `review` 更新为 `done`。

## Dev Agent Record

### Agent Model Used

Codex (GPT-5) / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.314s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.372s`

### Completion Notes List

- 为 leave/broadcast worker 增加 `running -> stopping -> closed` 显式状态机，`Stop()` 与发送路径通过 `RWMutex` 形成 send-safe 互斥，不再依赖“close channel 即状态”的隐式语义。
- 新增稳定错误类型 `ErrWorkerQueueStopping` / `ErrWorkerQueueClosed`（同时实现 `GRPCStatus`），`EnqueueLeave` 与 `BroadcastRoom` 在停机窗口返回可判定失败语义。
- 增加统一拒绝分类函数 `enqueueRejectReason`，区分 `queue_full`、`stopping`、`closed`；并通过 `recordCriticalEnqueueFailure` 上报 reason。
- 为停机拒绝日志增加 1s 节流窗口，避免停机窗口日志放大。
- 新增停机竞态回归测试：并发 `Stop()+EnqueueLeave`、并发 `Stop()+BroadcastRoom`、拒绝原因分类断言。
- 修复 `enqueueLeaveTask` 的 pending-key 去重分支：命中去重时若队列已 `stopping/closed`，返回 `ErrWorkerQueueStopping/Closed`，不再 `nil` 吞掉拒绝语义。
- 新增 `recordEnqueueFailure` 的 metrics 上报断言测试，验证 `queue_full/stopping/closed` 都会通过 recorder 上报对应 reason。
- 保持 Story 3.2/3.3 语义：本地解绑先行、Leave 异步队列与有限重试流程未回退。

### File List

- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/6-1-stop-safe-queue-close-and-send-guards.md
- /mnt/pubsub/_bmad-output/implementation-artifacts/sprint-status.yaml

## Senior Developer Review (AI)

### Reviewer

- Yonhoo
- Date: 2026-03-15
- Outcome: Changes Requested (Blocked from done)

### Findings

1. **HIGH**: `EnqueueLeave` 在 stopping/closed 窗口仍可能返回 `nil`，违反 AC2“关闭态必须明确失败语义”  
   - 证据：去重检查先于队列状态检查，命中 pending key 直接 `return nil`，不会返回 `ErrWorkerQueueStopping/Closed`。  
   - 代码位置：`/mnt/pubsub/connect-node/server.go:320-330`（先返回），`/mnt/pubsub/connect-node/server.go:346-359`（状态检查在后）。  
   - 影响：停机窗口下调用方可能把请求误判为成功，造成“静默吞掉停机拒绝”。

2. **MEDIUM**: Task 3.3 标记完成但“metrics 断言”未落地，任务完成声明与实现不一致  
   - 证据：Story 子任务写明“覆盖 stopping 态错误分类与 metrics 断言”，但新增测试仅覆盖 reason 分类函数返回值，没有断言 `recordCriticalEnqueueFailure`/metrics collector 的实际上报。  
   - 工件位置：`/mnt/pubsub/_bmad-output/implementation-artifacts/6-1-stop-safe-queue-close-and-send-guards.md:33`  
   - 代码位置：`/mnt/pubsub/connect-node/server_websocket_test.go:596-606`。  
   - 影响：可观测性 AC 的回归保护不足，后续变更可能悄然破坏 reason 上报而测试不报警。

3. **MEDIUM**: 工作区存在未纳入本 Story 的未提交变更，降低本次 review 可追溯性  
   - 证据：`git status --porcelain` 显示 `_bmad-output/planning-artifacts/epics.md`、`logs/*`、`connect-node.test`、`mcp-config.json`、`package-lock.json` 等非本 Story 产物变更/新增。  
   - 影响：在后续合并或复审时会混淆 Story 6.1 边界，不利于审计与回滚定位。

### AC Validation

- AC1: **Implemented**（`Stop` 与 enqueue 路径通过 `queueState + RWMutex` 协作，新增并发回归测试覆盖）。  
- AC2: **Partial**（存在 pending-key 提前返回 `nil` 的反例）。  
- AC3: **Partial**（实现有 reason 分类与节流日志，但缺少 metrics 上报断言测试）。  
- AC4: **Implemented**（未见对 Story 3.2/3.3 语义的回退证据）。  
- AC5: **Implemented**（`go test` 与 `go test -race` 已通过，且新增停机竞态测试）。

### Task Audit

- Task 1.2: **Partial**（存在 stopping/closed 下 `nil` 返回路径）。  
- Task 3.3: **Not fully done**（缺 metrics 断言）。  
- 其余已勾选任务：当前证据下可接受。

## Senior Developer Re-Review (AI)

### Reviewer

- Yonhoo
- Date: 2026-03-15
- Outcome: Approved (Ready for done)

### Focused Blocker Verification

1. 已关闭：`enqueueLeaveTask` 去重命中在 `stopping/closed` 下返回 `nil` 的问题  
   - 证据：`leavePending` 命中后新增 `queueState` + `leaveQueue` 检查，在非 running 或队列为空时返回 `ErrWorkerQueueStopping/ErrWorkerQueueClosed` 并记录拒绝原因。  
   - 代码位置：`/mnt/pubsub/connect-node/server.go:327-337`。  
   - 回归测试：`TestEnqueueLeaveTaskPendingDedupReturnsStopErrorWhenQueueStopping`。  
   - 测试位置：`/mnt/pubsub/connect-node/server_websocket_test.go:608-633`。

2. 已关闭：缺少 enqueue-failure metrics reason 断言测试  
   - 证据：新增 `criticalEnqueueFailureRecorder` 可替换 recorder，并在测试中断言 `queue_full/stopping/closed` 三种 reason 的实际上报。  
   - 代码位置：`/mnt/pubsub/connect-node/server.go:645-653`。  
   - 回归测试：`TestRecordEnqueueFailureReportsMetricsReason`。  
   - 测试位置：`/mnt/pubsub/connect-node/server_websocket_test.go:635-680`。

### Findings

1. **LOW（非阻塞）**: 工作区仍有与本 Story 无关的未提交变更（`_bmad-output/planning-artifacts/epics.md`、`logs/*`、`connect-node.test`、`mcp-config.json`、`package-lock.json`），建议在后续提交中分离，提升审计可追溯性。

### AC Validation (Re-Review)

- AC1: **Implemented**（`Stop` 与 enqueue 路径互斥保护，竞态回归测试覆盖）。  
- AC2: **Implemented**（`EnqueueLeave` 在 stopping/closed 下返回稳定错误，不再静默 `nil`）。  
- AC3: **Implemented**（`queue_full/stopping/closed` 原因分类 + metrics 上报断言 + 节流日志）。  
- AC4: **Implemented**（未见对 Story 3.2/3.3 语义回退）。  
- AC5: **Implemented**（`go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过，停机竞态测试存在）。

### Task Audit (Re-Review)

- Task 1.2: **Done**。  
- Task 3.3: **Done**。  
- 其余已勾选任务：**Done**。
