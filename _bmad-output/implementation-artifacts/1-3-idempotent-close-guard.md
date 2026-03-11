# Story 1.3: Close 幂等与关闭保护

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 连接层开发者,  
I want `Close` 具备幂等与非阻塞保护,  
so that 重复关闭不会导致阻塞或 panic。

## Acceptance Criteria

1. `channel.go` 中 `Close()` 必须是幂等的，多次调用只执行一次有效关闭动作。  
2. 当广播消息占用 `signal` 通道时，`Close()` 也不得阻塞调用链。  
3. `dispatchWebsocket` 必须能稳定观察到关闭事件并退出，不因 signal 队列状态卡住。  
4. 不改变现有外部协议行为，仅修复关闭语义与并发保护。

## Tasks / Subtasks

- [ ] Task 1: 为 `Close()` 增加幂等关闭保护（AC: #1, #4）
  - [ ] Subtask 1.1: 引入关闭态标记，避免重复关闭
  - [ ] Subtask 1.2: 保持现有调用方无需改签名

- [ ] Task 2: 消除关闭路径阻塞（AC: #2, #3）
  - [ ] Subtask 2.1: 将关闭通知与广播/ready 解耦
  - [ ] Subtask 2.2: 确保 `Ready()` 在关闭态下优先返回 finish

- [ ] Task 3: 补充回归测试（AC: #1, #2, #3）
  - [ ] Subtask 3.1: 验证 signal 满时 `Close()` 不阻塞
  - [ ] Subtask 3.2: 验证并发多次 `Close()` 不 panic 且只产生一次有效 finish

- [ ] Task 4: 执行验证并更新状态
  - [ ] Subtask 4.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 4.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 只处理 Channel 关闭语义，不扩展到控制面 Leave 异步化。
- 相关架构约束：
  - ADR-002: `Close()` 需具备幂等与不可阻塞语义。
  - FR6/NFR5/NFR8: 重复关闭不得导致 panic 或长时间阻塞。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/channel.go`
  - 测试：`/mnt/pubsub/connect-node/channel_test.go`
  - 参考：`/mnt/pubsub/connect-node/server_websocket.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 1 / Story 1.3）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`（ADR-002）
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR6, NFR5, NFR8）

## Dev Agent Record

### Agent Model Used

Amelia-context / create-story

### Debug Log References

- 待开发阶段填充

### Completion Notes List

- 待开发阶段填充

### File List

- /mnt/pubsub/connect-node/channel.go
- /mnt/pubsub/connect-node/channel_test.go

## Change Log

- 2026-03-10: 创建 Story 1.3 开发上下文，状态设为 `ready-for-dev`。
