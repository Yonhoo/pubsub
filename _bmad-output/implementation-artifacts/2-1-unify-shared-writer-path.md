# Story 2.1: 共享写路径唯一化

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 后端开发者,  
I want 会话写入统一走 shared writer manager,  
so that 我可以消除旧路径分叉和重复调度。

## Acceptance Criteria

1. 会话写入统一通过 shared writer manager 执行，不再保留旧的 `session.WritePkg` 直写兜底分叉。  
2. 服务端推送与响应写入共享同一写入路径。  
3. 注册/注销与写入状态保持一致，不引入新的竞态或 panic。  
4. 不改变现有外部协议行为，仅收敛内部写路径。

## Tasks / Subtasks

- [x] Task 1: 清理旧写路径分叉（AC: #1, #2）
  - [x] Subtask 1.1: 移除 `writeProto()` 中的旧直写 fallback
  - [x] Subtask 1.2: 清理不再使用的旧写入辅助函数

- [x] Task 2: 补充回归验证（AC: #1, #3）
  - [x] Subtask 2.1: 增加单测锁住“无 shared writer 时返回错误而非直写”的行为
  - [x] Subtask 2.2: 运行 `go test ./connect-node/...`
  - [x] Subtask 2.3: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦“写路径唯一化”，不扩展到批处理三触发条件或广播锁范围优化。
- shared writer manager 仍由 `server.go` 初始化并在 `OnOpen` / `OnClose` 时注册注销。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/server_websocket.go`
  - 测试：`/mnt/pubsub/connect-node/server_websocket_test.go`
  - 参考：`/mnt/pubsub/connect-node/shard_writer.go`、`/mnt/pubsub/connect-node/server.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2 / Story 2.1）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR1, FR3, NFR1, NFR2）

## Dev Agent Record

### Agent Model Used

Amelia-context / create-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.030s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.052s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -run=^$ -bench WriteProtoSharedWriter -benchmem ./connect-node/...`
- 结果：
  - `BenchmarkWriteProtoSharedWriterParallel-16 5227021 251.2 ns/op 112 B/op 1 allocs/op`
  - `BenchmarkWriteProtoSharedWriterManySessions-16 13658607 434.1 ns/op 112 B/op 1 allocs/op`

### Completion Notes List

- 已移除 `writeProto()` 的 `session.WritePkg` 直写兜底，响应/广播写入统一要求通过 shared writer manager。
- 已删除不再使用的旧写入辅助函数，减少分叉路径残留。
- 已新增 `server_websocket_test.go`，锁住“无 shared writer 时返回错误而非回退直写”的行为。
- 使用指定 Go 工具链执行 `go test` 与 `go test -race` 均通过。
- 已补充高并发测试，覆盖多 goroutine 并发走 `writeProto/shared writer` 路径，以及 shared writer 缺失时错误语义稳定不回退直写。
- 已补充超高并发 benchmark 与 `pprof` 产物，用于观察 shared writer 路径在高压下的 CPU/内存/锁热点。

### File List

- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/server_websocket_test.go

## Change Log

- 2026-03-11: 创建 Story 2.1 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-11: 完成共享写路径唯一化改造并补充回归测试，状态更新为 `review`。
- 2026-03-11: 补充高并发 shared writer 路径测试，Story 状态保持 `review`。
- 2026-03-11: 补充 shared writer 超高并发 benchmark 与 `pprof` 验证，Story 状态保持 `review`。
- 2026-03-11: 执行 `/bmad-bmm-code-review`，结论 Approve，Story 状态更新为 `done`。

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-11

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- `writeProto()` 已不再保留 `session.WritePkg` 直写回退，响应与广播写入统一要求进入 shared writer manager。
- 高并发测试已覆盖多 goroutine 并发入队与 shared writer 缺失时的稳定错误语义，足以支撑“路径唯一化已完成”的结论。
- `pprof` 热点集中在 `writeProto -> sharedWriteManager.Enqueue` 及运行时同步成本，结论与本 Story 的目标边界一致。

### Residual Risks / Testing Gaps

- 当前新增 benchmark 主要测量 shared writer 的 enqueue 路径，不等同于 flush 触发/批处理行为验证；该部分属于 Story 2.2 的后续范围。
