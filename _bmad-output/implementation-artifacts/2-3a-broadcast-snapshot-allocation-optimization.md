# Story 2.3a: broadcast snapshot 分配成本优化/验证

Status: done

<!-- Note: This is a backlog story created from Story 2.3 profiling follow-up. -->

## Story

As a 性能工程师,  
I want 针对超高并发广播下 `broadcastSnapshot` 的分配/拷贝成本进行优化或验证,  
so that 我可以在维持低锁竞争的同时，控制 snapshot 带来的 memory/copy 开销转移。

## Problem Statement

- Story 2.3 已把广播路径收敛为“锁内快照、锁外推送”，有效缩小了 `bucket.cLock` 持有范围。
- 但 Story 2.3 profiling 显示，在超高并发广播场景下，主要内存成本已转移到 `Bucket.broadcastSnapshot` 的切片分配与拷贝。
- 这说明锁竞争问题得到缓解后，新的优化焦点变成了 snapshot 内存行为，而不是继续扩大 2.3 的锁范围变更。

## Why This Is A Follow-up To Story 2.3

- Story 2.3 的目标是先确保“锁内快照、锁外推送”语义成立，优先解决全局锁竞争问题。
- snapshot 分配成本属于 2.3 改造后的 profiling trade-off，需要基于已有正确性和锁范围收敛结果，再单独评估优化空间与验证手段。
- 因此它是 2.3 的后续性能优化/验证 story，而不是 2.3 范围内应立即打包完成的内容。

## Acceptance Criteria

1. 建立一组专门针对超高并发广播下 snapshot 分配成本的 benchmark / profiling 场景。  
2. 明确比较“锁竞争下降”与“snapshot 分配成本上升”之间的 trade-off，并给出是否需要进一步优化的结论。  
3. 如果实施优化，需证明不回退 Story 2.3 的“锁内快照、锁外推送”语义。  
4. 输出 CPU / memory profiling 结果，并能定位 `broadcastSnapshot` 的 allocation/copy 占比变化。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 建立 snapshot allocation profiling 基线（AC: #1, #4）
  - [ ] Subtask 1.1: 设计超高并发 broadcast benchmark
  - [ ] Subtask 1.2: 采集 CPU / memory profile，必要时补 mutex / block profile

- [ ] Task 2: 评估或实施优化（AC: #2, #3）
  - [ ] Subtask 2.1: 判断是否需要引入复用/池化/分层快照等方案
  - [ ] Subtask 2.2: 若实施优化，验证不回退锁范围语义

- [ ] Task 3: 执行验证（AC: #5）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦 Story 2.3 后的 memory/copy trade-off，不回退“锁内快照、锁外推送”的架构方向。
- 优先级是“先验证是否值得优化”，而不是预设必须引入额外复杂度。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/bucket.go`
  - 测试/基准：`/mnt/pubsub/connect-node/bucket_test.go`
  - 参考：`/mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-3/`

### References

- 来源：Story 2.3 profiling trade-off
- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.031s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.053s`
- `GO_BIN=/home/node/.local/go/bin/go /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-3a/run-broadcast-allocation-benchmark.sh`
- 结果：
  - `BenchmarkBroadcastSnapshotOptimizedParallel-16 426126 2803 ns/op 56 B/op 2 allocs/op`
  - `BenchmarkBroadcastSnapshotOptimizedRoomFilteredParallel-16 463515 2594 ns/op 64 B/op 3 allocs/op`

### Completion Notes List

- 2026-03-12: 基于 Story 2.3 profiling 结果新增 backlog story，尚未进入实现阶段。
- 2026-03-12: Story 2.3a 正式进入开发准备，状态更新为 `ready-for-dev`。
- 已将 `broadcastSnapshot` 改为复用切片缓冲，减少超高并发广播下的 snapshot 分配成本。
- 已补充高 fan-out broadcast benchmark 与 CPU / memory / mutex / block `pprof`。
- 当前量化结果表明：在 1024 fan-out 场景下，snapshot 路径维持锁范围语义不变，同时 alloc/op 已降到双位数 B/op 量级。
- 已使用指定 Go 环境执行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...`，结果通过。

### File List

- /mnt/pubsub/connect-node/bucket.go
- /mnt/pubsub/connect-node/bucket_test.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-3a/run-broadcast-allocation-benchmark.sh

## Change Log

- 2026-03-12: 新增 Epic 2 backlog story，补足 broadcast snapshot allocation trade-off 的后续优化/验证空间。
- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-12: 完成 snapshot allocation 优化、benchmark 与 `pprof` 验证，状态更新为 `review`。
- 2026-03-12: 执行 `/bmad-bmm-code-review`，结论 Approve，Story 状态更新为 `done`。

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-12

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- `snapshotPool` 复用边界正确。snapshot 切片在 `Broadcast()` 中获取、使用并通过 `release()` 归还；归还前显式清空元素，避免旧 channel 指针在复用时造成数据污染。
- 并发语义未回退。`broadcastSnapshot()` 仍然只在快照阶段持有 `bucket.cLock`，`NeedPush` / room 过滤 / `Push` 继续在锁外执行，符合 Story 2.3 的架构边界。
- benchmark / `pprof` 足以支撑“allocation/copy 成本下降”的结论。高 fan-out benchmark 的 alloc/op 已降到双位数 `B/op`，memory profile 也显示 `broadcastSnapshot` 仍是主要残余热点，但总 alloc_space 已显著收缩。
- 未发现新的行为回归。本 Story 只优化 snapshot 缓冲复用，并未改变广播过滤和 push 的对外功能语义。

### Residual Risks / Testing Gaps

- `snapshotPool` 可能保留较大 capacity 的切片缓冲，从而在极端峰值 fan-out 后维持较高驻留内存；这属于典型池化 trade-off，目前没有迹象表明会影响正确性，可在后续按需要再决定是否加上容量裁剪策略。
