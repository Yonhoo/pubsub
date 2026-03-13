# Story 4.3: Unified Critical Metrics Export

Status: ready-for-dev

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

- [ ] Task 1: 统一关键指标出口（AC: #1, #2）
  - [ ] Subtask 1.1: 在 `pkg/metrics` 中补齐统一 critical metrics 定义
  - [ ] Subtask 1.2: 在 drop/enqueue-fail/lock-block/close 路径接入记录

- [ ] Task 2: 验证（AC: #3, #4）
  - [ ] Subtask 2.1: 增加最小 recorder / hook 测试
  - [ ] Subtask 2.2: 运行 `go test` 与 `go test -race`

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
