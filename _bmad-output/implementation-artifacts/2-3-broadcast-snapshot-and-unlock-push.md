# Story 2.3: 广播快照后解锁推送

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 系统架构师,  
I want 广播在锁内快照、锁外推送,  
so that 我可以降低全局锁竞争并提升广播稳定性。

## Acceptance Criteria

1. `Bucket.Broadcast` 在 `bucket.cLock` 持有期间只完成必要的 channel 快照，不在锁内执行 `NeedPush` / `Push`。  
2. 广播消息的 room 过滤和 op 过滤行为不发生回归。  
3. 广播路径在 push 阻塞或变慢时，不再长时间占用 `bucket.cLock`。  
4. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 收敛广播锁范围（AC: #1, #3）
  - [ ] Subtask 1.1: 为 `Bucket.Broadcast` 增加锁内快照逻辑
  - [ ] Subtask 1.2: 将过滤与推送移到锁外执行

- [ ] Task 2: 补充回归测试（AC: #2, #3）
  - [ ] Subtask 2.1: 验证 push 期间 `bucket.cLock` 已释放
  - [ ] Subtask 2.2: 验证 room / op 过滤行为不回归

- [ ] Task 3: 执行验证（AC: #4）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦 `Bucket.Broadcast` 的全局锁持有范围，不扩展到 room worker 队列或 queue-full 语义。
- 目标是将 `bucket.cLock` 从“遍历+push 全流程”缩小为“channel 快照”，减少 push 变慢时对其他读写路径的阻塞。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/bucket.go`
  - 测试：`/mnt/pubsub/connect-node/bucket_test.go`
  - 参考：`/mnt/pubsub/connect-node/channel.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2 / Story 2.3）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.031s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.054s`
- `GO_BIN=/home/node/.local/go/bin/go /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-3/run-broadcast-benchmark.sh`
- 结果：
  - `BenchmarkBroadcastSnapshotParallel-16 629302 1676 ns/op 2312 B/op 2 allocs/op`
  - `BenchmarkBroadcastSnapshotRoomFilteredParallel-16 677398 1616 ns/op 2304 B/op 1 allocs/op`

### Completion Notes List

- 2026-03-11: 创建 Story 2.3 开发上下文，状态设为 `ready-for-dev`。
- 已将 `Bucket.Broadcast` 调整为锁内只做 channel 快照，`NeedPush` / room 过滤 / `Push` 均在锁外执行。
- 已新增回归测试，验证 push 期间 `bucket.cLock` 已释放，且 room / op 过滤行为不回归。
- 使用指定 Go 环境执行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 均通过。
- 已补充高并发 broadcast benchmark 与 CPU / memory / mutex / block `pprof`，用于观察 snapshot 与锁外 push 路径的热点。

### File List

- /mnt/pubsub/connect-node/bucket.go
- /mnt/pubsub/connect-node/bucket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-3/run-broadcast-benchmark.sh

## Change Log

- 2026-03-11: 创建 Story 2.3 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-11: 完成广播快照后解锁推送改造并补充回归测试，状态更新为 `review`。
- 2026-03-12: 补充 broadcast 高并发 benchmark 与 `pprof` 验证，Story 状态保持 `review`。
