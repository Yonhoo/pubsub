# Story 6.2: 优雅停机 drain/wait 语义收口

Status: review

## Story

As a 平台工程师,  
I want connect-node 在停机时显式 drain/wait leave 与 room worker,  
so that 本地解绑后续的异步清理不会在关停窗口被静默丢失。

## Acceptance Criteria

1. 停机流程必须先拒绝新任务，再对在途 leave/room 任务执行 drain 或 wait，并有明确超时边界。  
2. 停机完成前，必须能区分“已完成”“超时放弃”“未开始处理”的任务结果。  
3. leave queue 与 room worker 的关闭路径必须有统一日志和指标，可支持灰度/回滚判定。  
4. 不破坏现有“本地解绑先行，控制面最终一致”的语义。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过，并补停机 drain/wait 集成或并发回归测试。

## Tasks / Subtasks

- [x] Task 1: 设计并实现停机时序（AC: #1, #2, #4）
  - [x] Subtask 1.1: 定义 stopping -> draining -> stopped 的生命周期
  - [x] Subtask 1.2: 为 leave/room worker 增加 wait group 或同等可等待机制
  - [x] Subtask 1.3: 明确超时后 residual task 的处理策略

- [x] Task 2: 收口 observability（AC: #2, #3）
  - [x] Subtask 2.1: 增加停机 drain outcome metrics
  - [x] Subtask 2.2: 统一 shutdown summary 日志，便于 phase gate 记录

- [x] Task 3: 增加回归验证（AC: #1, #4, #5）
  - [x] Subtask 3.1: 覆盖 leave 正在重试时停机
  - [x] Subtask 3.2: 覆盖 room broadcast worker 仍有排队任务时停机
  - [x] Subtask 3.3: 覆盖超时边界与结果分类

- [x] Task 4: 验证与记录（AC: #5）
  - [x] Subtask 4.1: 使用用户级 Go 工具链运行 `go test ./connect-node/...`
  - [x] Subtask 4.2: 使用用户级 Go 工具链运行 `go test -race ./connect-node/...`

- [x] Review Follow-ups (AI)
  - [x] [AI-Review][High] 延后 `sharedWriter.Stop()` 到 worker drain 完成之后，避免 drain 期间 room push 被提前打断并静默丢弃（`connect-node/server.go`）
  - [x] [AI-Review][High] 调整停机顺序与兜底策略，确保 WebSocket close 并发触发的 `cleanupUser -> EnqueueLeave` 在关停窗口内仍可被处理或补偿（`connect-node/main.go`, `connect-node/server_websocket.go`, `connect-node/server.go`）
  - [x] [AI-Review][Medium] 补充覆盖“WebSocket 关闭风暴 + Stop 并发”集成测试，验证不会出现 `leave_enqueue_failed` 导致的控制面遗漏（`connect-node/server_websocket_test.go`）

## Dev Notes

- 本 Story 来自 connect-node 总体复审的 High finding，优先级仅次于 Story 6.1。  
- 与 Story 6.1 的边界：
  - 6.1 先解决 panic/关闭态发送安全
  - 6.2 再解决优雅停机的 drain/wait 与结果分类
- 直接相关文件：
  - `/mnt/pubsub/connect-node/server.go`
  - `/mnt/pubsub/connect-node/main.go`
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`

## References

- 评审来源：connect-node BMAD-style overall review（High finding #2）
- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 6 / Story 6.2）
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR16, FR22, NFR5, NFR10）
- 上游依赖：`/mnt/pubsub/_bmad-output/implementation-artifacts/3-2-async-leave-queue-and-dedup.md`、`/mnt/pubsub/_bmad-output/implementation-artifacts/3-3-leave-retry-and-alerting.md`

## Change Log

- 2026-03-13: 基于 connect-node 总体复审结果创建 backlog story，状态设为 `ready-for-dev`。
- 2026-03-15: 完成 graceful stop 的 `stopping -> draining -> stopped` 生命周期、leave/room worker drain-wait 超时分类、shutdown summary metrics/logging 与并发回归测试，状态更新为 `review`。
- 2026-03-15: 执行 BMAD `/bmad-bmm-code-review` 复审，发现 2 个 High + 1 个 Medium 问题；状态从 `review` 回退为 `in-progress`，并补充 Review Follow-ups。
- 2026-03-15: 完成 code review follow-ups：调整 shared writer 停机顺序、补 `cleanupUser` 停机窗口补偿路径、增加 close-storm 并发回归测试；状态恢复为 `review`。

## Dev Agent Record

### Agent Model Used

Codex (GPT-5) / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.634s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
  - 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.671s`

### Completion Notes List

- `Stop()` 新增显式生命周期：`running -> stopping -> draining -> stopped`，先拒绝新任务再关闭队列并等待 worker drain。
- leave queue 与 room worker 增加 wait-group 协调和超时等待边界（默认 5s，可在 server 实例上覆写），保证停机窗口有确定的 wait/drain 收口行为。
- 新增停机结果分类摘要：`completed`、`timeout_abandoned`、`not_started`，并在停机完成时统一输出 shutdown summary 日志。
- 新增指标 `pubsub.critical.shutdown_drain.total{component,outcome}`，统一记录 leave/room 在停机 drain 阶段的结果分类。
- 保持 3.2/3.3 语义一致：本地解绑仍先行，Leave 仍为异步有限重试；停机仅收紧 worker drain/wait 收口，不回退既有语义。
- 新增回归测试覆盖：
  - leave retry delay 窗口停机时 pending key 清理与摘要结果；
  - room worker 有排队任务时停机的 timeout/not-started 分类；
  - leave worker 执行中停机的 timeout-abandoned 分类。
- code review follow-ups 已闭环：
  - `Stop()` 改为先 wait leave/room worker drain，再 `sharedWriter.Stop()`，避免 drain 窗口 room push 提前失效；
  - `cleanupUser` 改为 `EnqueueLeaveWithShutdownFallback`，在 `stopping/closed` 时异步补偿直连 `LeaveRoom`（有限重试）；
  - 新增“shared writer 停机时序”“queue closed 补偿”“WebSocket close storm + Stop 并发”回归测试。
- 验证命令（follow-up）：
  - `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.674s`
  - `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.717s`

### File List

- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/connect-node/critical_metrics.go
- /mnt/pubsub/connect-node/critical_metrics_test.go
- /mnt/pubsub/pkg/metrics/metrics.go
- /mnt/pubsub/pkg/metrics/metrics_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/6-2-graceful-stop-drain-and-wait-semantics.md
- /mnt/pubsub/_bmad-output/implementation-artifacts/sprint-status.yaml

## Senior Developer Review (AI)

### Review Date

2026-03-15

### Reviewer

Yonhoo (AI Senior Code Review)

### Scope

- Story 工件与 AC/Tasks 完整核对：`6-2-graceful-stop-drain-and-wait-semantics.md`
- 代码审查：`connect-node/server.go`、`connect-node/main.go`、`connect-node/server_websocket.go`、`connect-node/server_websocket_test.go`、`connect-node/critical_metrics.go`、`pkg/metrics/metrics.go`
- 提交核对：`94d0866`、`e085bb2`、`5b20c09`、`cf9a7ed`
- 验证命令：
  - `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`（通过）
  - `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`（通过）

### Findings

1. [High] Stop 在 worker drain 前提前停止 shared writer，导致 room drain 任务可被静默丢弃，违背 AC #1/#4 的“drain 后再停”目标。  
   证据：`Stop()` 中先 `close(roomWorkers)` 后立即 `sharedWriter.Stop()`，此时 worker 仍可能执行 `room.PushMsg`。`Channel.Push` 在 shared writer 不可用时按丢弃处理，`Room.PushMsg` 忽略该错误。  
   参考：`connect-node/server.go:274-276`、`connect-node/server.go:542-555`、`connect-node/channel.go:126-147`、`connect-node/room.go:68-72`。

2. [High] 主进程停机顺序存在竞态窗口：先关闭 WebSocket server，再 Stop leave 队列，可能让 `cleanupUser` 的 Leave 入队失败而不补偿，破坏“本地解绑先行但最终一致”语义（AC #4）。  
   证据：`main.go` 先执行 `server.Close()` 再 `connectNodeServer.Stop()`；`OnClose -> cleanupUser` 仍会调用 `EnqueueLeave`，但队列进入 stopping/draining 后会直接返回错误且仅记录日志。  
   参考：`connect-node/main.go:223-227`、`connect-node/server_websocket.go:531-585`、`connect-node/server.go:419-431`。

3. [Medium] 缺少“WebSocket close 风暴与 Stop 并发”回归测试，当前新增测试仅覆盖队列/worker 层，不覆盖 main 关停顺序引入的语义风险。  
   证据：`server_websocket_test.go` 覆盖了 leave retry、room worker timeout/not-started，但没有覆盖 `serverList.Close()` 与 `Stop()` 并发触发 `cleanupUser` 的路径。  
   参考：`connect-node/server_websocket_test.go`（现有新增用例集中在 queue/worker）。

### AC Validation

- AC1: Partial（生命周期与 wait/timeout 已实现，但 shared writer 提前停止导致 drain 期间可丢消息）
- AC2: Partial（分类计数存在，但未覆盖/统计 stop 窗口内 enqueue reject 的会话清理遗漏）
- AC3: Implemented（summary 日志与 `pubsub.critical.shutdown_drain.total` 已落地）
- AC4: Not Fully Implemented（存在 stop 并发窗口导致 leave 可能未入队且无补偿）
- AC5: Implemented（本次复核再次执行 `go test` 与 `go test -race` 均通过）

### Outcome

- Decision: Changes Requested
- Story Status Recommendation: `in-progress`
- Sprint Sync Recommendation: `6-2-graceful-stop-drain-and-wait-semantics -> in-progress`

## Follow-up Implementation Record (AI)

### Date

2026-03-15

### Commits

- `bdad142` fix(connect-node): stop shared writer after worker drain wait
- `831730e` fix(connect-node): compensate leave on shutdown enqueue rejects
- `8ddfe82` test(connect-node): add stop and websocket close storm regressions

### Verification

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...` -> pass
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...` -> pass

### Status Recommendation

- Story: `review`
- Sprint sync: `6-2-graceful-stop-drain-and-wait-semantics -> review`
