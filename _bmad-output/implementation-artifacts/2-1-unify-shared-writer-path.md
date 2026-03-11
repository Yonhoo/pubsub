# Story 2.1: 共享写路径唯一化

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 后端开发者,  
I want 会话写入统一走 shared writer manager,  
so that 我可以消除旧路径分叉和重复调度。

## Acceptance Criteria

1. 会话写入统一通过 shared writer manager 执行，不再保留旧的 `session.WritePkg` 直写兜底分叉。  
2. 服务端推送与响应写入共享同一写入路径。  
3. 注册/注销与写入状态保持一致，不引入新的竞态或 panic。  
4. 不改变现有外部协议行为，仅收敛内部写路径。

## Tasks / Subtasks

- [ ] Task 1: 清理旧写路径分叉（AC: #1, #2）
  - [ ] Subtask 1.1: 移除 `writeProto()` 中的旧直写 fallback
  - [ ] Subtask 1.2: 清理不再使用的旧写入辅助函数

- [ ] Task 2: 补充回归验证（AC: #1, #3）
  - [ ] Subtask 2.1: 增加单测锁住“无 shared writer 时返回错误而非直写”的行为
  - [ ] Subtask 2.2: 运行 `go test ./connect-node/...`
  - [ ] Subtask 2.3: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 聚焦“写路径唯一化”，不扩展到批处理三触发条件或广播锁范围优化。
- shared writer manager 仍由 `server.go` 初始化并在 `OnOpen` / `OnClose` 时注册注销。
- 涉及文件：
  - 主要：`/mnt/pubsub/connect-node/server_websocket.go`
  - 测试：`/mnt/pubsub/connect-node/server_websocket_test.go`
  - 参考：`/mnt/pubsub/connect-node/shard_writer.go`、`/mnt/pubsub/connect-node/server.go`

### References

- Epic 来源：`/mnt/pubsub/_bmad-output/planning-artifacts/epics.md`（Epic 2 / Story 2.1）
- 架构约束：`/mnt/pubsub/_bmad-output/planning-artifacts/architecture.md`
- 需求来源：`/mnt/pubsub/_bmad-output/planning-artifacts/prd.md`（FR1, FR3, NFR1, NFR2）

## Dev Agent Record

### Agent Model Used

Amelia-context / create-story

### Debug Log References

- 待开发阶段填充

### Completion Notes List

- 待开发阶段填充

### File List

- /mnt/pubsub/connect-node/server_websocket.go
- /mnt/pubsub/connect-node/server_websocket_test.go

## Change Log

- 2026-03-11: 创建 Story 2.1 开发上下文，状态设为 `ready-for-dev`。
