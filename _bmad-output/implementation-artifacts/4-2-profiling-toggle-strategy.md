# Story 4.2: Profiling 开关策略落地

Status: ready-for-dev

## Story

As a 性能工程师,  
I want mutex/block/pprof 采样默认关闭并可开关,  
so that 常态性能不被采样噪音污染。

## Acceptance Criteria

1. 默认启动时 profiling 保持低开销。  
2. 通过环境变量可显式打开 mutex/block/pprof 采样。  
3. 启动日志清楚打印当前 profiling 口径。  
4. 打开后 1 分钟内可采到目标 profile。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 收敛 profiling 开关（AC: #1, #2, #3）
  - [ ] Subtask 1.1: 提供统一 settings loader
  - [ ] Subtask 1.2: 让 main 按 settings 应用 runtime/pprof 开关

- [ ] Task 2: 增加验证（AC: #5）
  - [ ] Subtask 2.1: 为 settings 默认值与 ENV 覆盖增加测试
  - [ ] Subtask 2.2: 运行 `go test` 与 `go test -race`

## Dev Notes

- 本 Story 聚焦 profiling 开关策略，不改变业务处理逻辑。
- 默认应关闭 mutex/block profile 与 pprof 服务，避免常态额外开销。
- 涉及文件：
  - `/mnt/pubsub/connect-node/main.go`
  - `/mnt/pubsub/connect-node/profiling.go`
  - `/mnt/pubsub/connect-node/profiling_test.go`
  - `/mnt/pubsub/_bmad-output/implementation-artifacts/4-2-profiling-toggle-strategy.md`

## Change Log

- 2026-03-13: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
