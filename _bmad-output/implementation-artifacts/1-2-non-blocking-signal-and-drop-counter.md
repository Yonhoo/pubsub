# Story 1.2: Signal 非阻塞化与丢弃计数

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 服务稳定性负责人,  
I want `Signal` 改为非阻塞唤醒并记录丢弃计数,  
so that 高并发时不会因信号阻塞拖垮处理链路。

## Acceptance Criteria

1. `channel.go` 中 `Signal()` 必须改为非阻塞通知，signal 通道满时立即返回。  
2. 当 signal 通道达到上限时，必须记录可读取的 signal drop 计数。  
3. `ClientReqQueue` 仍作为业务消息真实来源；重复 ready 信号被合并时，不得导致 ring 中已入队请求丢失。  
4. 不改变现有外部协议语义，仅调整内部唤醒机制。

## Tasks / Subtasks

- [ ] Task 1: 改造 `Signal()` 为非阻塞通知（AC: #1, #4）
  - [ ] Subtask 1.1: 使用 `select { case ...: default: }` 消除阻塞发送
  - [ ] Subtask 1.2: 保持 dispatch 侧“取空 ring 才休眠”的现有语义不变

- [ ] Task 2: 增加 signal drop 计数（AC: #2）
  - [ ] Subtask 2.1: 提供全局/按 channel 可读取的 signal drop 计数
  - [ ] Subtask 2.2: 不再混用业务 push 丢弃与 signal 丢弃统计

- [ ] Task 3: 增加语义回归测试（AC: #3)
  - [ ] Subtask 3.1: 验证 signal 满时不会阻塞
  - [ ] Subtask 3.2: 验证丢失重复 ready 信号后，ring 中请求仍可被完整消费

- [ ] Task 4: 执行验证并更新状态
  - [ ] Subtask 4.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 4.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 只处理 `Signal` 的非阻塞唤醒与信号 drop 计数。
- `Close()` 幂等与 finish 非阻塞属于 Story 1.3，本次不提前实现。
- 相关架构约束：
  - ADR-002: `Signal()` 改为非阻塞，重复 ready 信号允许合并。
  - FR5/FR7/FR8: signal 只承担唤醒语义，业务可靠性依赖 ring buffer。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/channel.go`
  - 测试：`/mnt/pubsub/connect-node/channel_test.go`
  - 参考：`/mnt/pubsub/connect-node/server_websocket.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 1 / Story 1.2）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`（ADR-002）
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR5, FR7, FR8）

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

- 2026-03-10: 创建 Story 1.2 开发上下文，状态设为 `ready-for-dev`。
