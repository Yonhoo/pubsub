# Story 3.2: Leave 异步队列与去重

Status: review

## Story

As a 平台工程师,  
I want Leave 改为异步队列并按 `room:user` 去重,  
so that 连接风暴时关闭路径不会被控制面调用拖慢。

## Acceptance Criteria

1. 关闭路径先完成本地解绑，再异步提交 `LeaveRoom`。  
2. `LeaveRoom` 使用异步队列处理，不在 `OnClose` / `cleanupUser` 路径同步阻塞。  
3. 同一 `room:user` 在 pending 期间不会被重复排队。  
4. 为后续重试 story 预留基础结构，但不提前扩展完整重试策略。  
5. 不破坏 Join 同步确认语义。  
6. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 改造 Leave 异步队列（AC: #1, #2, #4）
  - [ ] Subtask 1.1: 在 server 侧增加 leave queue / worker
  - [ ] Subtask 1.2: 将 `cleanupUser` 改为本地解绑后异步入队

- [ ] Task 2: 增加 dedup 保护（AC: #3, #5）
  - [ ] Subtask 2.1: 为 `room:user` 建立 pending 去重键
  - [ ] Subtask 2.2: 补充 Join/Leave 语义不互相污染的回归测试

- [ ] Task 3: 执行验证（AC: #6）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 只做 Leave 异步队列化、去重和稳定性准备，不提前实现完整失败重试。
- Join 仍必须保持同步确认语义，不能被 Leave 改造连带破坏。
- 涉及文件：
  - `/mnt/pubsub/connect-node/server.go`
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/_bmad-output/implementation-artifacts/3-2-async-leave-queue-and-dedup.md`

## Change Log

- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-12: Leave 改为本地解绑后异步入队，补充 `room:user` 去重和回归测试，状态更新为 `review`。

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.133s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.159s`

### Completion Notes List

- 在 `ConnectNodeServer` 中新增 Leave queue / worker，并使用 `room:user` 作为 pending 去重键。
- `cleanupUser` 改为先做本地 bucket/channel 解绑，再异步入队 `LeaveRoom`。
- `leaveTask` 中保留 `attempts` / `enqueuedAt` 等基础字段，为后续 Story 3.3 的重试治理预留结构。
- 新增回归测试覆盖：
  - 本地解绑先于异步 Leave 完成
  - 同一 `room:user` 不会重复排队
  - Join 同步语义测试继续通过，未被 Leave 改造破坏

### File List

- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/3-2-async-leave-queue-and-dedup.md
