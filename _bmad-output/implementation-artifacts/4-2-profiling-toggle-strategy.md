# Story 4.2: Profiling 开关策略落地

Status: review

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

- [x] Task 1: 收敛 profiling 开关（AC: #1, #2, #3）
  - [x] Subtask 1.1: 提供统一 settings loader
  - [x] Subtask 1.2: 让 main 按 settings 应用 runtime/pprof 开关

- [x] Task 2: 增加验证（AC: #5）
  - [x] Subtask 2.1: 为 settings 默认值与 ENV 覆盖增加测试
  - [x] Subtask 2.2: 运行 `go test` 与 `go test -race`

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

- 2026-03-13: 执行 `/bmad-bmm-dev-story`，新增统一 profiling settings loader，默认关闭 mutex/block/pprof，支持 ENV 显式打开并输出当前采样口径。

## Dev Agent Record

### Agent Model Used
Amelia-context / dev-story

### Debug Log References
- `go test ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.266s`
- `go test -race ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.297s`

### Completion Notes
- 默认 profiling 低开销：mutex/block 采样率默认 `0`，pprof 服务默认不启动。
- 通过 `CONNECT_NODE_PPROF_ENABLED`、`CONNECT_NODE_PPROF_PORT`、`CONNECT_NODE_MUTEX_PROFILE_FRACTION`、`CONNECT_NODE_BLOCK_PROFILE_RATE`、`CONNECT_NODE_WRITE_TRACE_LOG` 可显式覆盖。
- 启动日志输出当前 profiling 生效值，便于确认采样口径。

### File List
- `/mnt/pubsub/connect-node/main.go`
- `/mnt/pubsub/connect-node/profiling.go`
- `/mnt/pubsub/connect-node/profiling_test.go`
- `/mnt/pubsub/_bmad-output/implementation-artifacts/4-2-profiling-toggle-strategy.md`
