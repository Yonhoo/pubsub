# Story 3.1: Join 同步语义守护

Status: ready-for-dev

## Story

As a 产品负责人,  
I want 保持 JoinRoom 同步确认语义,  
so that 业务侧仍可即时判定入房成功/失败。

## Acceptance Criteria

1. `JoinRoom` 成功或失败时，客户端都能立即得到一致回复。  
2. `JoinRoom` 回复不能被后续 Leave 异步化或关闭清理路径改造悄悄推迟。  
3. 成功 Join 时，room/watch 状态更新与成功响应保持同一同步确认链路。  
4. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 固化 Join 同步确认语义（AC: #1, #2, #3）
  - [ ] Subtask 1.1: 审视 `processClientRequest` 的 Join 路径
  - [ ] Subtask 1.2: 增加同步语义回归测试

- [ ] Task 2: 执行验证（AC: #4）
  - [ ] Subtask 2.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 2.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 的重点是“守住语义”，不是提前引入 Leave 异步队列。
- 如果当前实现已经满足要求，优先通过测试与注释固化约束，而不是制造不必要的行为改动。
- 涉及文件：
  - `/mnt/pubsub/connect-node/server_websocket.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/_bmad-output/implementation-artifacts/3-1-preserve-sync-join-semantics.md`

## Change Log

- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
