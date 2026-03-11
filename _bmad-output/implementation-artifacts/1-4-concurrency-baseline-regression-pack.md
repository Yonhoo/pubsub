# Story 1.4: 并发基线回归包

Status: done

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

- [x] Task 1: 建立并发基线脚本入口（AC: #1, #2）
  - [x] Subtask 1.1: 提供 race / bench / pprof 的统一执行入口
  - [x] Subtask 1.2: 约定输出目录与产物命名

- [x] Task 2: 建立 FR/NFR 对账清单（AC: #2, #3）
  - [x] Subtask 2.1: 将 Epic 1 已完成 Story 的验证映射到 FR/NFR
  - [x] Subtask 2.2: 记录通过/失败判定标准

- [x] Task 3: 执行最小验证（AC: #2）
  - [x] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [x] Subtask 3.2: 运行 `go test -race ./connect-node/...`

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

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.017s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.042s`
- `run-concurrency-baseline.sh`
- 结果：
  - 统一产出 `go-test` / `go-test-race` / `go-bench` / `pprof` 资产到 `validation/story-1-4/`
  - `BenchmarkDelRoom-16 1355 ns/op 2208 B/op 12 allocs/op`
  - `BenchmarkCloseConcurrentContention-16 8052 ns/op 1312 B/op 27 allocs/op`

### Completion Notes List

- 已提供统一回归入口：`run-concurrency-baseline.sh`，覆盖 `go test`、`go test -race`、benchmark 与 CPU/memory/mutex/block `pprof` 采集。
- 已提供 `fr-nfr-traceability.md`，将 Epic 1 的 Story 1.1 ~ 1.4 验证资产映射到 FR/NFR。
- 已执行脚本并生成可追溯产物到 `validation/story-1-4/`。
- 使用指定 Go 工具链执行 `go test` 与 `go test -race` 均通过。

### File List

- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/run-concurrency-baseline.sh
- /mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/fr-nfr-traceability.md

## Change Log

- 2026-03-11: 创建 Story 1.4 开发上下文，状态设为 `ready-for-dev`。
- 2026-03-11: 完成并发基线回归包（脚本入口 + FR/NFR 映射 + 基线产物），状态更新为 `review`。
- 2026-03-11: 执行 `/bmad-bmm-code-review`，结论 Approve，Story 状态更新为 `done`。

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-11

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- `run-concurrency-baseline.sh` 已能作为统一复用入口，覆盖 `go test`、`go test -race`、benchmark 与 CPU/memory/mutex/block `pprof` 采集。
- `fr-nfr-traceability.md` 已将 Epic 1 Story 1.1 ~ 1.4 的验证资产映射到对应 FR/NFR，满足 Story 1.4 的可追溯目标。
- 已生成完整基线产物，足以支撑 Epic 1 并发正确性回归；后续只需复跑脚本并对比产物即可。

### Residual Risks / Testing Gaps

- 基线脚本默认依赖当前 Go 环境变量，后续若执行环境变更，需要在调用方显式传入 `GO_BIN` 或保持 Go 工具链可用。
