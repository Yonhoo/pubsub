# Story 2.4: 队列满场景失败语义与可观测

Status: review

## Story

As a 运维工程师,  
I want shared writer 队列满时具备明确失败语义、节流日志与可观测计数,  
so that 我可以快速判断这是容量瓶颈而不是功能故障。

## Acceptance Criteria

1. shared writer 队列满时，调用方收到可识别失败类型，而不是无限阻塞或静默吞掉。  
2. 队列满失败具备节流日志，避免高压场景日志放大。  
3. 队列满失败具备可观测计数，可按来源区分至少 `response` / `broadcast` / `heartbeat`。  
4. 广播侧在 shared writer 满载时保持非阻塞语义。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 明确 shared writer queue full 失败语义（AC: #1, #4）
  - [ ] Subtask 1.1: 统一 `writeProto` / server push enqueue 失败路径
  - [ ] Subtask 1.2: 为 queue full 定义稳定错误返回

- [ ] Task 2: 补齐日志与指标（AC: #2, #3）
  - [ ] Subtask 2.1: 增加 queue full 节流日志
  - [ ] Subtask 2.2: 增加 enqueue success/failure 观测计数

- [ ] Task 3: 执行验证（AC: #5）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦 shared writer 队列满时的失败语义与观测闭环，不扩展到新的调度策略。
- 目标是最小化行为面变化：广播仍保持非阻塞，response/heartbeat 等路径在 queue full 时返回稳定错误。
- 涉及文件：
  - `/mnt/pubsub/connect-node/shard_writer.go`
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/pkg/metrics/metrics.go`

## Change Log

- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-12: shared writer enqueue 路径统一为显式 `queue full` 失败语义，并补齐节流日志与 metrics 计数。
- 2026-03-12: 执行 `/bmad-bmm-dev-story`，状态更新为 `review`。

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.031s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.054s`

### Completion Notes List

- `writeProto` 与 server push enqueue 统一改为 `TryEnqueue`，shared writer 满载时返回稳定 `shared writer queue full` 错误。
- 新增 enqueue success/failure/queue_full 计数，并接入 `pubsub.shared_writer.enqueue.total` metrics。
- 新增 queue-full 节流日志，避免 response/broadcast/heartbeat 高压场景下错误日志爆量。
- 广播侧保持非阻塞：`Channel.Push` 在 shared writer 满载时仍快速返回，并继续累计 drop 计数。

### File List

- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/pkg/metrics/metrics.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/2-4-queue-full-failure-semantics-and-observability.md
