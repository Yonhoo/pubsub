# Story 1.3: Close 幂等与关闭保护

Status: review

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

- [x] Task 1: 为 `Close()` 增加幂等关闭保护（AC: #1, #4）
  - [x] Subtask 1.1: 引入关闭态标记，避免重复关闭
  - [x] Subtask 1.2: 保持现有调用方无需改签名

- [x] Task 2: 消除关闭路径阻塞（AC: #2, #3）
  - [x] Subtask 2.1: 将关闭通知与广播/ready 解耦
  - [x] Subtask 2.2: 确保 `Ready()` 在关闭态下优先返回 finish

- [x] Task 3: 补充回归测试（AC: #1, #2, #3）
  - [x] Subtask 3.1: 验证 signal 满时 `Close()` 不阻塞
  - [x] Subtask 3.2: 验证并发多次 `Close()` 不 panic 且只产生一次有效 finish

- [x] Task 4: 执行验证并更新状态
  - [x] Subtask 4.1: 运行 `go test ./connect-node/...`
  - [x] Subtask 4.2: 运行 `go test -race ./connect-node/...`

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

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.013s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.059s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -run=^$ -bench Close -benchmem ./connect-node/...`
- 结果：
  - `BenchmarkCloseConcurrentContention-16 150807 8347 ns/op 1312 B/op 27 allocs/op`
  - `BenchmarkCloseAlreadyClosed-16 130086837 9.273 ns/op 0 B/op 0 allocs/op`

### Completion Notes List

- `Close()` 已改为基于 `done` 通道的一次性关闭信号；重复 `Close()` 调用不再阻塞或重复执行关闭动作。
- `Ready()` 在关闭态下优先返回 `ProtoFinish`，不再受广播消息占用 `signal` 通道影响。
- 新增 `channel_test.go` 回归测试，覆盖“signal 被占用时 Close 不阻塞”和“并发多次 Close 幂等”场景。
- 使用指定 Go 工具链执行 `go test` 与 `go test -race` 均通过。
- 已补充高并发关闭测试，覆盖“多 goroutine 并发 Close”“ready/broadcast 与 Close 混合”“关闭后 Signal/重复 Close 不阻塞”场景。
- 已补充 Close 高并发 benchmark 与 `pprof` 采样产物，用于后续对比关闭路径 CPU/内存/锁热点。

### File List

- /mnt/pubsub/connect-node/channel.go
- /mnt/pubsub/connect-node/channel_test.go

## Change Log

- 2026-03-10: 创建 Story 1.3 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-10: 完成 `Close()` 幂等与非阻塞保护实现，状态更新为 `review`。
- 2026-03-10: 补充高并发关闭测试覆盖，Story 状态保持 `review`。
- 2026-03-11: 补充 Close benchmark 与 `pprof` 验证，Story 状态保持 `review`。
