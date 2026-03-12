# Story 2.3b: churn 路径画像与优化验证

Status: backlog

<!-- Note: This is a backlog story created from ongoing Epic 2 performance analysis. -->

## Story

As a 性能工程师,  
I want 针对 `Put` / `Del` / `ChangeRoom` 高频 churn 路径建立内存/锁/阻塞画像与优化验证,  
so that 我可以补齐连接与房间变更路径的性能基线，而不只停留在广播路径。

## Problem Statement

- Epic 2 目前已经对 shared writer 和 broadcast 路径建立了较完整的 benchmark / `pprof` 画像。
- 但大量 channel 新增/删除、room 变更的 churn 路径仍缺少专门的 benchmark / `pprof`，无法量化其内存分配、锁竞争和阻塞成本。
- 这使得 Epic 2 的性能验证仍偏向“写入/广播”侧，对连接生命周期和 room 变更频繁场景的热点认识不完整。

## Why This Belongs To Epic 2

- Epic 2 的目标是“写入与广播性能收敛”，其本质是把热点路径的锁范围、内存成本和吞吐瓶颈逐步量化并收敛。
- `Put` / `Del` / `ChangeRoom` 虽然不是广播逻辑本身，但它们直接影响 bucket/channel/room 的高频结构变更成本，是同一性能收敛链路中的后续验证 story。
- 因此它属于 Epic 2 的后续性能验证 story，而不是新的功能 Epic。

## Acceptance Criteria

1. 建立专门覆盖高 churn 场景的 benchmark，至少覆盖 `Put`、`Del`、`ChangeRoom` 三类路径。  
2. 输出 CPU / memory / mutex / block profiling 结果，能够定位 churn 路径中的主要内存、锁和阻塞热点。  
3. 如实施优化，需证明不回退现有并发正确性语义。  
4. 给出量化 trade-off 结论：哪些成本值得优化，哪些属于可接受基线。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 建立 churn benchmark 基线（AC: #1, #2）
  - [ ] Subtask 1.1: 为 `Put` / `Del` / `ChangeRoom` 设计高 churn benchmark
  - [ ] Subtask 1.2: 采集 CPU / memory / mutex / block profile

- [ ] Task 2: 评估或实施优化（AC: #3, #4）
  - [ ] Subtask 2.1: 判断热点是否集中在 room 变更、bucket map 维护或 channel 生命周期管理
  - [ ] Subtask 2.2: 若实施优化，验证不回退并发正确性

- [ ] Task 3: 执行验证（AC: #5）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦 churn 路径的性能画像与验证，不扩展到新协议行为。
- 优先级是补齐基线和确定热点，再决定是否进入进一步优化。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/bucket.go`
  - 相关：`/mnt/pubsub/connect-node/room.go`
  - 测试/基准：`/mnt/pubsub/connect-node/bucket_test.go`

### References

- 来源：当前对 Put/Del/ChangeRoom 高频 churn 成本的分析缺口
- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`

## Dev Agent Record

### Agent Model Used

Amelia-context / backlog-story

### Debug Log References

- 待后续开发阶段补充

### Completion Notes List

- 2026-03-12: 新增 Epic 2 backlog story，用于补齐 churn 路径的 benchmark / pprof 基线。

### File List

- /mnt/pubsub/connect-node/bucket.go
- /mnt/pubsub/connect-node/room.go
- /mnt/pubsub/connect-node/bucket_test.go

## Change Log

- 2026-03-12: 新增 Epic 2 backlog story，补齐高 churn 连接/房间路径的性能画像与优化验证空间。
