# Story 2.4: 队列满场景失败语义与可观测

Status: done

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
- 2026-03-12: 执行 `/bmad-bmm-code-review`，结论 Approve，状态更新为 `done`。

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

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-12

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- `shared writer queue full` 的显式失败语义已经稳定落地：
  - `writeProto` 与 server push enqueue 都统一走 `enqueueSharedWrite -> TryEnqueue`
  - 队列满时稳定返回 `errSharedWriterQueueFull`
  - 不再出现“部分路径阻塞、部分路径丢弃”的不一致
- `writeProto` 与 server push enqueue 的行为口径已经统一：
  - 两条路径共享同一错误分类、计数和 metrics 记录
  - 广播侧继续通过 `Channel.Push` 保持非阻塞，调用方可拿到明确错误并继续累计 drop 计数
- 节流日志与 metrics 口径合理：
  - metrics 使用 `source` / `result` / `reason` 三元标签，足以区分 `response` / `broadcast` / `heartbeat` 与 success/failure/queue_full
  - queue full 失败日志按 session 1 秒节流，避免高压场景日志放大
- 未发现产品行为回归：
  - 本 Story 只把 shared writer 满载时的行为从“可能阻塞/不透明”收敛为“显式失败 + 可观测”
  - 广播 push 的非阻塞语义仍保持

### Residual Risks / Testing Gaps

- `reason=unavailable` 当前同时覆盖 shared writer 缺失与其他非-queue-full enqueue 失败，作为 Story 2.4 的运维观测已够用；如果后续需要更细粒度容量治理，可再拆出 `stopped` / `unregistered` 等子类。
- `776eab7` 的提交信息偏 docs-only，但实际内容已包含代码、测试和 story 状态更新；这是提交记录准确性问题，不构成运行时风险。
