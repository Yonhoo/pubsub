# Phase Canary 模板使用说明

本模板用于 Epic 5 / Story 5.1（FR21）阶段化灰度发布，目标是确保每个 Phase 都具备统一的流量放量、观测窗口和异常快速回退策略。

## 使用步骤

1. 复制 `phase-canary-template.yaml` 为具体阶段文件（例如 `phase-2-canary.yaml`）。
2. 按阶段填写 `phase`、`owner`、`version`、`traffic_steps`、`rollback`。
3. 执行校验：

```bash
go run ./tools/release/validate_phase_canary --file docs/release/phase-2-canary.yaml
```

4. 仅当校验通过时，允许进入发布执行。

## 字段约束

- `traffic_steps`: 必填，至少 1 步。
- `traffic_steps[].traffic_percent`: 必填，范围 `1..100`，并且必须单调不下降。
- `traffic_steps[].observe_seconds`: 必填，必须大于 0。
- `traffic_steps[].success_criteria`: 必填，至少 1 条。
- `traffic_steps[].abort_criteria`: 必填，至少 1 条。
- `rollback.trigger/switch/action/verify`: 全部必填。
- `traffic_steps` 最后一阶段必须是 `100%`，确保有完整放量定义。

## 异常处理原则

- 任一步命中 `abort_criteria` 立即停止后续放量。
- 立刻执行 `rollback.action` 与 `rollback.switch`，并记录回退时间点。
- `rollback.verify` 通过后才允许关闭故障响应。
