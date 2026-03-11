# Story 1.4: 并发基线回归包

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a QA/平台协作角色,  
I want 建立并发正确性回归检查清单,  
so that Epic 1 的修复可持续防回归。

## Acceptance Criteria

1. 提供可复用的并发回归入口，至少覆盖 `go test`、`go test -race`、benchmark 与 `pprof` 采集。  
2. 产出可追溯的验证工件路径与脚本/清单说明。  
3. 验证结果必须可映射至对应 FR/NFR。  
4. 不改变现有业务协议行为，仅补充回归包与验证资产。

## Tasks / Subtasks

- [ ] Task 1: 建立并发基线脚本入口（AC: #1, #2）
  - [ ] Subtask 1.1: 提供 race / bench / pprof 的统一执行入口
  - [ ] Subtask 1.2: 约定输出目录与产物命名

- [ ] Task 2: 建立 FR/NFR 对账清单（AC: #2, #3）
  - [ ] Subtask 2.1: 将 Epic 1 已完成 Story 的验证映射到 FR/NFR
  - [ ] Subtask 2.2: 记录通过/失败判定标准

- [ ] Task 3: 执行最小验证（AC: #2）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 的重点是“可复用回归包”，不是新增并发修复逻辑。
- 优先复用 Story 1.1 ~ 1.3 已沉淀的 benchmark / pprof 资产。
- 涉及文件：
  - 主要：`/mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/`
  - 参考：`/mnt/pubsub/connect-node/channel_test.go`、`/mnt/pubsub/connect-node/bucket_test.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 1 / Story 1.4）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR23, NFR7, NFR14）

## Dev Agent Record

### Agent Model Used

Amelia-context / create-story

### Debug Log References

- 待开发阶段填充

### Completion Notes List

- 待开发阶段填充

### File List

- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/run-concurrency-baseline.sh
- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/fr-nfr-traceability.md

## Change Log

- 2026-03-11: 创建 Story 1.4 开发上下文，状态设为 `ready-for-dev`。
