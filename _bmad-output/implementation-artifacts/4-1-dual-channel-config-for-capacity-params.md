# Story 4.1: 容量参数双通道配置

Status: ready-for-dev

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
