# Story 3.3: Leave 失败重试与告警

Status: review

## Story

As a 运维值班人员,  
I want Leave 失败后可有限重试并可告警,  
so that 控制面一致性问题可恢复且可观测。

## Acceptance Criteria

1. Leave 失败后执行有限次数重试。  
2. 重试成功率与最终失败计数可被监控。  
3. 最终失败时有明确告警信号。  
4. 不破坏 3.2 的“本地解绑先行、Leave 异步执行”语义。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 在 leave queue 上补有限重试（AC: #1, #4）
  - [ ] Subtask 1.1: 基于现有 `leaveTask` 增加尝试次数控制
  - [ ] Subtask 1.2: 为失败路径增加有限退避重试

- [ ] Task 2: 增加可观测与告警信号（AC: #2, #3）
  - [ ] Subtask 2.1: 补充 leave outcome metrics
  - [ ] Subtask 2.2: 在最终失败时输出明确告警日志

- [ ] Task 3: 执行验证（AC: #5）
  - [ ] Subtask 3.1: 运行 `go test ./connect-node/...`
  - [ ] Subtask 3.2: 运行 `go test -race ./connect-node/...`

## Dev Notes

- 本 Story 只实现有限重试与告警信号，不扩展完整持久化补偿。
- 3.2 的本地解绑先行语义必须保持。
- 涉及文件：
  - `/mnt/pubsub/connect-node/server.go`
  - `/mnt/pubsub/connect-node/server_websocket_test.go`
  - `/mnt/pubsub/pkg/metrics/metrics.go`
  - `/mnt/pubsub/_bmad-output/implementation-artifacts/3-3-leave-retry-and-alerting.md`

## Change Log

- 2026-03-12: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-12: 在 leave queue 上补有限重试、leave outcome metrics 与最终失败告警日志，状态更新为 `review`。

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 0.266s`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./connect-node/...`
- 结果：`ok github.com/livekit/psrpc/examples/pubsub/connect-node 1.300s`

### Completion Notes List

- 在 leave queue 上新增有限次数重试，默认 3 次尝试、固定退避。
- 新增 `pubsub.leave.total` metrics，按 `result/reason` 记录 success / retry_scheduled / final_failure。
- 最终失败时输出 `🚨 [LeaveQueue]` 告警日志，形成值班可见信号。
- 重试成功后会清理 pending key；最终失败后也会释放 dedup key，允许后续重新入队。
- 本地解绑先于异步 Leave 的 3.2 语义保持不变。

### File List

- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/server_websocket_test.go
- /mnt/pubsub/pkg/metrics/metrics.go
- /mnt/pubsub/_bmad-output/implementation-artifacts/3-3-leave-retry-and-alerting.md
