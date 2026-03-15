# connect-node integration suite (Story 6.3)

## 运行入口

- 默认（不含 integration）
  - `go test ./connect-node/...`
  - `go test -race ./connect-node/...`
- 含 integration critical 套件
  - `go test -tags=integration ./connect-node/...`
  - `go test -race -tags=integration ./connect-node/...`
- 含 integration extended 套件（额外并发竞态长跑）
  - `CONNECT_NODE_IT_SUITE=extended go test -tags=integration ./connect-node/...`
  - `CONNECT_NODE_IT_SUITE=extended go test -race -tags=integration ./connect-node/...`

## FR/NFR 对账矩阵

- `TestIntegrationJoinSyncConfirmationWithBufconnController`
  - FR13（Join 同步确认链）
  - NFR8（协议语义兼容）
- `TestIntegrationLeaveAsyncUnbindAndRetry`
  - FR14（Leave 异步队列化）
  - FR16（本地解绑先行，再控制面清理）
  - NFR8（语义不回退）
- `TestIntegrationSharedWriterQueueFullFailureSemantic`
  - FR19（关键 enqueue failure 可观测语义）
  - NFR10（关键路径失败语义可统计）
- `TestIntegrationBroadcastRoomOpFiltering`
  - FR11（broadcast room/op 过滤）
  - FR23（可验证验收条目）
- `TestIntegrationExtendedStopEnqueueRaceStableErrors`
  - FR16（停机窗口语义稳定）
  - FR23（阶段验收可追溯）
  - NFR7（并发/race 回归入口）

## 与 review finding 的对应

- 复审关注的关键风险（Join/Leave 语义回退、停机竞态、queue full、广播过滤）在本套件均有对应用例。
- 本 Story 仅新增轻量进程内 fake controller（bufconn gRPC）与 mock Getty session，不引入重型外部依赖。
