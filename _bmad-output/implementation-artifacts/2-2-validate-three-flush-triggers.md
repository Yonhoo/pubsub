# Story 2.2: 批处理三触发条件验证

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 性能工程师,  
I want 批处理支持条数/字节/超时三触发,  
so that 吞吐与尾延迟可在不同流量形态下稳定。

## Acceptance Criteria

1. shared writer 在不同消息大小与速率下，可分别由按条数、按字节、按超时三种条件触发 flush。  
2. 验证输出能够区分三种触发路径，不把 unregister/stop 等非目标路径混入统计。  
3. 不改变 shared writer 的现有注册/注销和写入协议行为。  
4. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 均通过。

## Tasks / Subtasks

- [x] Task 1: 为 flush 路径增加可区分触发原因统计（AC: #1, #2）
  - [x] Subtask 1.1: 在 shared writer 中区分 count / bytes / timeout 三类 flush 原因
  - [x] Subtask 1.2: 将非目标路径（如 unregister / stop）从三触发统计中隔离

- [x] Task 2: 补充三触发验证测试（AC: #1, #2, #3)
  - [x] Subtask 2.1: 增加按条数触发 flush 的回归测试
  - [x] Subtask 2.2: 增加按字节触发 flush 的回归测试
  - [x] Subtask 2.3: 增加按超时触发 flush 的回归测试

- [x] Task 3: 执行验证（AC: #4）
  - [x] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [x] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦 shared writer 的三类 flush 触发条件验证，不扩展到广播锁优化或队列满失败语义。
- 统计口径必须能区分 `count` / `bytes` / `timeout`，且不把 `unregister` / `stop` / `drain` 等非目标 flush 计入三触发指标。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/shard_writer.go`
  - 测试：`/mnt/pubsub/connect-node/shard_writer_test.go`
  - 参考：`/mnt/pubsub/connect-node/server_websocket.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2 / Story 2.2）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR2, NFR1, NFR2）

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.031s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.060s`

### Completion Notes List

- 2026-03-11: 创建 Story 2.2 开发上下文，状态设为 `ready-for-dev`。
- 已为 shared writer 的 flush 统计增加 `count` / `bytes` / `timeout` 三类原因计数。
- 已将 `unregister` / `stop` 触发的 drain flush 从三触发统计中隔离，避免污染验证口径。
- 已新增 `shard_writer_test.go`，覆盖按条数、按字节、按超时，以及 unregister drain 不污染三触发计数。
- 使用指定 Go 环境执行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 均通过。

### File List

- /mnt/pubsub/connect-node/shard_writer.go
- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/shard_writer_test.go

## Change Log

- 2026-03-11: 创建 Story 2.2 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-11: 完成三触发路径统计与回归测试，状态更新为 `review`。
