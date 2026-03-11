# Story 1.2: Signal 非阻塞化与丢弃计数

Status: done

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

- [x] Task 1: 改造 `Signal()` 为非阻塞通知（AC: #1, #4）
  - [x] Subtask 1.1: 使用 `select { case ...: default: }` 消除阻塞发送
  - [x] Subtask 1.2: 保持 dispatch 侧“取空 ring 才休眠”的现有语义不变

- [x] Task 2: 增加 signal drop 计数（AC: #2）
  - [x] Subtask 2.1: 提供全局/按 channel 可读取的 signal drop 计数
  - [x] Subtask 2.2: 不再混用业务 push 丢弃与 signal 丢弃统计

- [x] Task 3: 增加语义回归测试（AC: #3)
  - [x] Subtask 3.1: 验证 signal 满时不会阻塞
  - [x] Subtask 3.2: 验证丢失重复 ready 信号后，ring 中请求仍可被完整消费

- [x] Task 4: 执行验证并更新状态
  - [x] Subtask 4.1: 运行 `go test ./connect-node/...`
  - [x] Subtask 4.2: 运行 `go test -race ./connect-node/...`

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

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.014s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.041s`

### Completion Notes List

- `Signal()` 已改为非阻塞 ready 通知；signal 满时直接返回，不再阻塞调用链。
- signal drop 计数已与业务 `Push` 丢弃计数拆分，新增 per-channel / global 可读接口。
- 新增 `channel_test.go`，覆盖“signal 满不阻塞”与“ready 合并不丢 ring 中请求”两个关键语义。
- 使用指定 Go 工具链执行 `go test` 与 `go test -race` 均通过。
- 已修复 code review 阻塞项：ready 现在与广播/finish 使用独立通道，不再把“任意 signal 消息占用”误判为可合并 ready。
- 新增回归测试覆盖“广播已占用 signal 时，ready 仍可送达且队列请求不会滞留”。

### File List

- /mnt/pubsub/connect-node/channel.go
- /mnt/pubsub/connect-node/channel_test.go

## Change Log

- 2026-03-10: 创建 Story 1.2 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-10: 完成 `Signal` 非阻塞化、signal drop 计数拆分与回归测试，状态更新为 `review`。
- 2026-03-10: 执行 `/bmad-bmm-code-review`，发现阻塞级问题，状态回退为 `in-progress`。
- 2026-03-10: 修复 ready 与广播复用通道导致的唯一唤醒丢失问题，状态推进回 `review`。
- 2026-03-10: 复审通过，Story 状态更新为 `done`。

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-10

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Blocking Issues

- 无。

### Recommended Fix Direction

- 将 `Signal` 的非阻塞语义与“是否已有 ready 待处理”解耦，避免在通道仅被广播消息占用时丢失唯一 ready。
- 可选方向：
  - 为 ready 建立独立的待处理标记/单独通道；
  - 或在 dispatch 处理完非-ready 消息后补检查 `ClientReqQueue`，确保不会因 ready 丢弃而饿死队列。

### Resolution

- 已采用“独立 ready 通道”方案，`Signal()` 仅向 `ready` 通道做非阻塞发送，广播/finish 继续走 `signal` 通道。
- `Ready()` 统一从两个通道选择事件，因此只有“已有待消费 ready”时才会发生 ready 合并，不再受广播消息占用影响。

### Residual Risks / Testing Gaps

- `Close()` 仍是阻塞发送，这属于 Story 1.3 的既有范围，不是本 Story 新引入的问题。
- 当前测试已覆盖 reviewer 指出的广播占位场景；更高强度的关闭并发语义验证仍应在 Story 1.3 补齐。
