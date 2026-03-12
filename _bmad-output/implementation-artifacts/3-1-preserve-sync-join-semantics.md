# Story 3.1: Join 同步语义守护

Status: done

## Story

As a 产品负责人,  
I want 保持 JoinRoom 同步确认语义,  
so that 业务侧仍可即时判定入房成功/失败。

## Acceptance Criteria

1. `JoinRoom` 成功或失败时，客户端都能立即得到一致回复。  
2. `JoinRoom` 回复不能被后续 Leave 异步化或关闭清理路径改造悄悄推迟。  
3. 成功 Join 时，room/watch 状态更新与成功响应保持同一同步确认链路。  
4. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 固化 Join 同步确认语义（AC: #1, #2, #3）
  - [ ] Subtask 1.1: 审视 `processClientRequest` 的 Join 路径
  - [ ] Subtask 1.2: 增加同步语义回归测试

- [ ] Task 2: 执行验证（AC: #4）
  - [ ] Subtask 2.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 2.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 的重点是“守住语义”，不是提前引入 Leave 异步队列。
- 如果当前实现已经满足要求，优先通过测试与注释固化约束，而不是制造不必要的行为改动。
- 涉及文件：
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/_bmad-output/implementation-artifacts/3-1-preserve-sync-join-semantics.md`

## Change Log

- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-12: 新增 Join 同步语义回归测试与代码注释，状态更新为 `review`。
- 2026-03-12: 执行 `/bmad-bmm-code-review`，结论 Approve，状态更新为 `done`。

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.032s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.056s`

### Completion Notes List

- 为 Join 路径补充“控制面未返回前不得提前 ack”的同步语义测试。
- 为 Join 失败路径补充错误 ack 与本地 room/watch 不被污染的回归测试。
- 在 Join 分支增加同步确认链路注释，明确后续 Leave 异步化改造不得影响 Join 的立即确认语义。
- 本 Story 未改 Join 协议行为，只通过测试和注释锁定现有正确语义。

### File List

- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/3-1-preserve-sync-join-semantics.md

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-12

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- Join 成功 ack 仍然严格依赖控制面返回。
  - 测试已经证明 `processClientRequest` 在 `JoinRoom` 返回前不会提前 enqueue 成功 ack。
  - 成功链路中，本地 `room` 更新、`Watch(2)` 注册与成功响应保持同一同步确认链路。
- 失败路径没有污染本地 `room/watch` 状态。
  - `JoinRoom` 返回失败时，仍会立即发送错误 ack
  - 但不会提前写入 `channel.Room`，也不会注册 `Watch(2)`
- 回归测试足以覆盖 Story 3.1 的目标边界。
  - 成功场景验证了“控制面返回前不 ack”
  - 失败场景验证了“错误 ack + 无本地状态污染”
- 未发现行为回归。
  - 本 Story 没有改变 Join 的产品语义，只是把同步确认约束通过测试和注释显式化

### Residual Risks / Testing Gaps

- 当前测试聚焦 `processClientRequest`，不是完整 `OnMessage -> authWebsocket -> processClientRequest` 端到端链路。
- 对 Story 3.1 而言这已足够，因为本 Story 要守住的是 Join 确认语义，而不是重新覆盖整个 websocket 生命周期。
