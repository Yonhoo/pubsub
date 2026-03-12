# Story 2.3a: broadcast snapshot 分配成本优化/验证

Status: backlog

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

Amelia-context / backlog-story

### Debug Log References

- 待后续开发阶段补充

### Completion Notes List

- 2026-03-12: 基于 Story 2.3 profiling 结果新增 backlog story，尚未进入实现阶段。

### File List

- /mnt/pubsub/connect-node/bucket.go
- /mnt/pubsub/connect-node/bucket_test.go

## Change Log

- 2026-03-12: 新增 Epic 2 backlog story，补足 broadcast snapshot allocation trade-off 的后续优化/验证空间。
