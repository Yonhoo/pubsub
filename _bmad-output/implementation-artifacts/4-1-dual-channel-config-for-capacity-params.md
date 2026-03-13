# Story 4.1: 容量参数双通道配置

Status: done

## Story

As a 运维工程师,  
I want `svr_proto` / shared writer / leave queue 等容量参数支持 YAML + ENV 双入口,  
so that 我可以按环境快速调优而不改代码。

## Acceptance Criteria

1. 关键容量参数支持 YAML 默认值。  
2. 对应环境变量可覆盖 YAML。  
3. 启动时可看到生效值，口径清晰。  
4. 默认值明确，不依赖散落的魔法常量。  
5. 运行 `go test ./connect-node/...` 与 `go test -race ./connect-node/...` 通过。

## Tasks / Subtasks

- [ ] Task 1: 把容量参数收敛到配置模型（AC: #1, #2, #4）
  - [ ] Subtask 1.1: 为 shared writer / leave queue 增加配置结构
  - [ ] Subtask 1.2: 让 server/main 使用统一 effective config

- [ ] Task 2: 增加可追踪性与验证（AC: #3, #5）
  - [ ] Subtask 2.1: 启动日志输出关键生效值
  - [ ] Subtask 2.2: 增加 YAML / ENV 覆盖测试

## Dev Notes

- 本 Story 聚焦参数入口和优先级，不改变现有业务默认语义。
- 目标容量参数至少包括 shared writer 和 leave queue 相关值。
- 涉及文件：
  - `/mnt/pubsub/pkg/config/config.go`
  - `/mnt/pubsub/connect-node/main.go`
  - `/mnt/pubsub/connect-node/server.go`
  - `/mnt/pubsub/connect-node/config.yaml`
  - `/mnt/pubsub/pkg/config/config_test.go`

## Change Log

- 2026-03-13: 执行 `/bmad-bmm-create-story`，状态更新为 `ready-for-dev`。
- 2026-03-13: shared writer / leave queue 容量参数收敛到 YAML + ENV 双入口，并补充配置测试，状态更新为 `review`。
- 2026-03-13: 执行 `/bmad-bmm-code-review`，结论 Approve，状态更新为 `done`。

## Dev Agent Record

### Agent Model Used

Amelia-context / dev-story

### Debug Log References

- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test ./pkg/config ./connect-node/...`
- 结果：
  - `ok github.com/livekit/psrpc/examples/pubsub/pkg/config 0.005s`
  - `ok github.com/livekit/psrpc/examples/pubsub/connect-node (cached)`
- `GOROOT=/home/node/.local/go GOPATH=/home/node/go PATH=... go test -race ./pkg/config ./connect-node/...`
- 结果：
  - `ok github.com/livekit/psrpc/examples/pubsub/pkg/config 1.020s`
  - `ok github.com/livekit/psrpc/examples/pubsub/connect-node (cached)`

### Completion Notes List

- 新增 `SharedWriterConfig` 与 `LeaveQueueConfig`，让 shared writer / leave queue 容量参数走统一配置模型。
- 支持 YAML 默认值 + ENV 覆盖：
  - `SHARED_WRITER_BATCH_SIZE`
  - `SHARED_WRITER_MAX_BATCH_BYTES`
  - `SHARED_WRITER_FLUSH_INTERVAL`
  - `SHARED_WRITER_QUEUE_SIZE`
  - `LEAVE_QUEUE_RETRY_DELAY`
  - `LEAVE_QUEUE_MAX_ATTEMPTS`
- `server.go` 和 `main.go` 改为消费 `cfg.SharedWriter` / `cfg.LeaveQueue`，并在启动日志输出 effective values。
- 修复 `RawYAMLConfig` 嵌套路径读取，确保新增 YAML 节点能被正确解析。

### File List

- /mnt/pubsub/pkg/config/config.go
- /mnt/pubsub/pkg/config/config_test.go
- /mnt/pubsub/connect-node/server.go
- /mnt/pubsub/connect-node/main.go
- /mnt/pubsub/connect-node/config.yaml
- /mnt/pubsub/_bmad-output/implementation-artifacts/4-1-dual-channel-config-for-capacity-params.md

## Senior Developer Review (AI)

### Review Outcome

Approve

### Review Date

2026-03-13

### Findings (Severity Ordered)

- 无 High/Medium/Low 级缺陷。

### Review Notes

- YAML + ENV 双入口优先级正确。
  - `LoadConfigFromFile` 继续保持“ENV 优先于 YAML，YAML 优先于默认值”的口径
  - 新增测试已经覆盖 shared writer / leave queue 的 YAML 生效与 ENV 覆盖
- 新增 `shared_writer` / `leave_queue` 节点能够正确解析并传递到运行时。
  - `RawYAMLConfig` 嵌套路径读取问题已修复
  - `server.go` 读取 `cfg.SharedWriter` / `cfg.LeaveQueue`
  - `main.go` 输出对应 effective values
- 默认值与启动日志口径一致。
  - 配置默认值集中在 `pkg/config`
  - server 侧只在配置缺失时做同口径兜底
  - 启动日志直接打印运行时实际使用值
- 未发现新的配置回归。
  - 现有 `Protocol/Bucket` 路径未受影响
  - 新增配置仅扩展而未替换既有参数入口

### Residual Risks / Testing Gaps

- 本 Story 聚焦 shared writer / leave queue 容量参数，没有把 gRPC buffer 等其他“类容量”常量一并参数化；这不影响 4.1 通过，但如果后续要继续消除散落常量，可在 Epic 4 后续 story 中补齐。
- 提交顺序上曾出现一次 git 锁竞争，导致 docs commit 先于代码 commit；当前仓库内容已对齐，不构成运行时风险。
