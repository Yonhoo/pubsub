# Story 5.1: 阶段灰度策略模板化

Status: ready-for-dev

## Story

As a 发布经理,
I want 每个 Phase 有标准灰度策略模板,
so that 发布风险可控且执行一致。

## Acceptance Criteria

1. 为任一 Phase 提供统一可复用的灰度模板，明确流量比例与观测窗口。  
2. 模板必须包含异常触发与快速回退所需的最小字段（触发条件、回退动作、回退验证）。  
3. 模板支持在不同 Phase 复用，且不影响现有服务逻辑。  
4. 提供可执行校验（lint/validate）以避免缺字段配置进入发布流程。  

## Tasks / Subtasks

- [ ] Task 1: 设计并落地 Phase 灰度模板（AC: #1, #2, #3）
  - [ ] Subtask 1.1: 新增可复用 YAML 模板与示例
  - [ ] Subtask 1.2: 补充模板使用说明（流量比例、观测窗口、回退动作）

- [ ] Task 2: 模板校验与验证（AC: #4）
  - [ ] Subtask 2.1: 增加模板结构校验逻辑
  - [ ] Subtask 2.2: 增加单测覆盖关键校验分支
  - [ ] Subtask 2.3: 运行相关 `go test`

## Dev Notes

- 本 Story 聚焦发布治理模板化，不改动 connect-node 业务路径与协议语义。
- 需保持与 PRD/Architecture 中 FR21 对齐；不提前实现 FR22/FR23/FR24 的完整门禁流程。
- 预期产物：
  - 阶段灰度模板（可复用）
  - 模板说明文档
  - 模板校验工具与测试

## Change Log

- 2026-03-13: 执行 `/bmad-bmm-create-story`，新增 Story 5.1 artifact，状态设为 `ready-for-dev`。

## Dev Agent Record

### Agent Model Used
Amelia-context / dev-story

### Debug Log References
- Pending

### Completion Notes
- Pending

### File List
- `/mnt/pubsub/_bmad-output/implementation-artifacts/5-1-phase-canary-template.md`
