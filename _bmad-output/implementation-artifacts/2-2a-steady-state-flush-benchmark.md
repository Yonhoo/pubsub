# Story 2.2a: steady-state flush 长跑基准

Status: ready-for-dev

<!-- Note: This is a backlog story created from Story 2.2 review follow-up. -->

## Story

As a 性能工程师,  
I want 针对 shared writer steady-state flush 开销建立复用已注册 session 的长跑 benchmark,  
so that 我可以把真实 steady-state 成本与 2.2 中偏初始化成本的 regression sentinel 区分开来。

## Why This Is Different From Story 2.2

- Story 2.2 的 benchmark 主要用于验证三种 flush trigger 路径是否存在异常回归，热点受 shard/session/pool 初始化影响较大，更适合作为 regression sentinel。
- 本 Story 聚焦 steady-state：复用已注册 session、复用已建立的 flush shard，尽量剥离一次性初始化成本，观察真实 flush 周期开销、批处理吞吐和锁竞争画像。

## Acceptance Criteria

1. 提供至少一组复用已注册 session 的长跑 benchmark，覆盖 steady-state 下的 `count` / `bytes` / `timeout` flush 场景。  
2. benchmark 不在每次迭代中重复创建 shard/session/pool，而是复用预热后的 shared writer 基线对象。  
3. 输出 CPU / memory profiling 结果，并能说明 steady-state 热点与 Story 2.2 regression sentinel 的差异。  
4. 产出明确的验证入口或脚本清单，便于后续 Epic 2 调优前后复跑对比。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 设计 steady-state benchmark 基线（AC: #1, #2）
  - [ ] Subtask 1.1: 复用已注册 session 和 flush shard，建立预热流程
  - [ ] Subtask 1.2: 区分 `count` / `bytes` / `timeout` 三类 steady-state 场景

- [ ] Task 2: 补充 profiling 与对比说明（AC: #3, #4）
  - [ ] Subtask 2.1: 采集 CPU / memory profile，必要时补 mutex / block profile
  - [ ] Subtask 2.2: 记录与 Story 2.2 regression sentinel 的差异结论

- [ ] Task 3: 执行验证（AC: #5）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 属于 Epic 2 的性能验证补强，不改变 shared writer 对外协议或 flush 语义。
- 目标是补足 Story 2.2 review 中保留的 steady-state 画像缺口，而不是重复做 trigger 正确性验证。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/shard_writer_test.go`
  - 可选：`/mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-2-2/`
  - 参考：`/mnt/pubsub/connect-node/shard_writer.go`

### References

- 来源：Story 2.2 review 保留项
- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`

## Dev Agent Record

### Agent Model Used

Amelia-context / backlog-story

### Debug Log References

- 待后续开发阶段补充

### Completion Notes List

- 2026-03-11: 基于 Story 2.2 review 保留项新增 backlog story，尚未进入实现阶段。
- 2026-03-11: Story 2.2a 正式进入开发准备，状态更新为 `ready-for-dev`。

### File List

- /mnt/pubsub/connect-node/shard_writer_test.go
- /mnt/pubsub/connect-node/shard_writer.go

## Change Log

- 2026-03-11: 新增 Epic 2 backlog story，补足 steady-state flush 基准缺口。
- 2026-03-11: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
