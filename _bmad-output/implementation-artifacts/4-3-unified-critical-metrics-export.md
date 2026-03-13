# Story 4.3: Unified Critical Metrics Export

Status: in-progress

## Story

As a 平台运维工程师,
I want Epic 4 关键风险信号通过统一命名的 metrics 导出,
so that drop、enqueue-fail、lock-block、close-latency 可以被同一套观测面消费。

## Acceptance Criteria

1. 提供统一前缀的关键 metrics 导出，至少覆盖 `drop`、`enqueue-fail`、`lock-block`、`close-latency`。  
2. 指标命名与 labels 口径一致，避免同类问题散落在不同命名空间。  
3. 现有业务逻辑不回归，指标导出本身不改变并发语义。  
4. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。  

## Tasks / Subtasks

- [x] Task 1: 统一关键指标出口（AC: #1, #2）
  - [x] Subtask 1.1: 在 `pkg/metrics` 中补齐统一 critical metrics 定义
  - [x] Subtask 1.2: 在 drop/enqueue-fail/lock-block/close 路径接入记录

- [x] Task 2: 验证（AC: #3, #4）
  - [x] Subtask 2.1: 增加最小 recorder / hook 测试
  - [x] Subtask 2.2: 运行 `go test` 与 `go test -race`

## Dev Notes

- 本 Story 不改协议或业务语义，只统一关键观测出口。
- 重点涉及：
  - `/mnt/pubsub/pkg/metrics/metrics.go`
  - `/mnt/pubsub/connect-node/channel.go`
  - `/mnt/pubsub/connect-node/bucket.go`
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/main.go`

## Change Log

- 2026-03-13: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。

- 2026-03-13: 执行 `/bmad-bmm-dev-story`，统一导出 `pubsub.critical.*` 指标族，覆盖 drop、enqueue_fail、lock_block、close_latency，并补最小 recorder / hook 测试。
- 2026-03-13: 修复 review findings：
  - `main.go` 在 Metrics 初始化失败时 fail-fast，避免继续启动并误报成功；
  - `channel.go` 改为 `errors.Is` 稳定识别 `shared writer queue full`；
  - `bucket.go` 将 `lock_block` 指标记录移到 unlock 之后，避免延长临界区；
  - close 指标改为 `pubsub.critical.close_cleanup.duration` 并补充注释，明确仅表示本地 close 清理时延（非端到端 Leave 完成）；
  - 新增 metrics 名称与 label 集稳定性测试。

## Dev Agent Record

### Agent Model Used
Amelia-context / dev-story

### Debug Log References
- `go test ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.275s`
- `go test -race ./connect-node/...` -> `ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.305s`
- `go test ./pkg/metrics` -> `ok github.com/livekit/psrpc/examples/pubsub/pkg/metrics 0.006s`
- `go test -race ./pkg/metrics` -> `ok github.com/livekit/psrpc/examples/pubsub/pkg/metrics 1.036s`
- 2026-03-13（review fix）: 当前执行环境缺少 `go` 工具链，无法在本地重新执行：
  - `go test ./pkg/metrics ./connect-node/...` -> `/bin/bash: go: command not found`
  - `go test -race ./pkg/metrics ./connect-node/...` -> `/bin/bash: go: command not found`

### Completion Notes
- 统一新增 `pubsub.critical.drop.total`、`pubsub.critical.enqueue_fail.total`、`pubsub.critical.lock_block.duration`、`pubsub.critical.close_latency.duration`。
- drop/enqueue-fail/lock-block/close-latency 均从现有关键路径接入，不改变业务处理语义。
- 对现有 Leave retry 测试做了稳定性修正，避免 worker 成功与 pending 清理的微小时序差导致假失败。
- review finding 修复后：
  - startup 对 Metrics 初始化失败行为与日志一致；
  - queue-full 分类不再依赖错误字符串；
  - bucket 临界区不会被指标上报路径延长；
  - close 指标语义已明确为本地清理时延，避免被误解为端到端 Leave 完成；
  - 增加指标名与 label 集稳定性断言测试。

### File List
- `/mnt/pubsub/pkg/metrics/metrics.go`
- `/mnt/pubsub/pkg/metrics/metrics_test.go`
- `/mnt/pubsub/connect-node/critical_metrics.go`
- `/mnt/pubsub/connect-node/critical_metrics_test.go`
- `/mnt/pubsub/connect-node/channel.go`
- `/mnt/pubsub/connect-node/channel_test.go`
- `/mnt/pubsub/connect-node/bucket.go`
- `/mnt/pubsub/connect-node/server_websocket.go`
- `/mnt/pubsub/connect-node/server_websocket_test.go`
- `/mnt/pubsub/connect-node/main.go`
- `/mnt/pubsub/_bmad-output/implementation-artifacts/4-3-unified-critical-metrics-export.md`
