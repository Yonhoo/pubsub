---
stepsCompleted:
  - step-01-validate-prerequisites
  - step-02-design-epics
  - step-03-create-stories
  - step-04-final-validation
inputDocuments:
  - /mnt/pubsub/_bmad-output/planning-artifacts/prd.md
  - /mnt/pubsub/_bmad-output/planning-artifacts/architecture.md
  - /mnt/pubsub/optmise.md
  - /mnt/pubsub/connect-node/channel.go
  - /mnt/pubsub/connect-node/bucket.go
  - /mnt/pubsub/connect-node/server_websocket.go
  - /mnt/pubsub/connect-node/shard_writer.go
  - /mnt/pubsub/connect-node/server.go
  - /mnt/pubsub/connect-node/main.go
  - /mnt/pubsub/pkg/config/config.go
workflowType: 'epics-and-stories'
project_name: 'pubsub'
user_name: 'Yonhoo'
date: '2026-03-09'
status: 'complete'
lastStep: 4
completedAt: '2026-03-09'
---

# pubsub - Epic Breakdown

## Overview

本文档将 `prd.md` 与 `architecture.md`（含 A3 回灌校准）分解为可实施的 Epic 与 Story，面向 connect-node 后端并发优化场景。分解原则：

- 以用户价值与可交付结果组织，而非技术分层
- 每个 Story 可由单开发代理独立完成
- Story 仅依赖同 Epic 之前的 Story，不依赖未来 Story
- FR/NFR 全量可追溯

## Requirements Inventory

### Functional Requirements

FR1: 系统必须使用共享写入管理器（shard 事件循环）进行会话写入批处理。  
FR2: 系统必须支持按条数、按字节、按超时三种批量 flush 触发条件。  
FR3: 系统必须在会话注册/注销时正确维护共享写入状态，避免泄漏。  
FR4: 系统必须在共享队列满时提供明确失败返回与可观测统计。  
FR5: `Signal` 必须采用非阻塞通知语义，避免调用链阻塞。  
FR6: `Close` 必须具备幂等语义，重复关闭不得导致阻塞或 panic。  
FR7: 系统必须区分“唤醒信号语义”和“业务消息语义”，不以信号计数保证业务可靠性。  
FR8: 系统必须记录信号丢弃计数用于容量评估。  
FR9: 广播流程必须支持在锁内快照、锁外推送，减少全局锁持有时间。  
FR10: `DelRoom` 删除操作必须在写锁保护下执行。  
FR11: 广播必须支持按操作码订阅过滤与 room 过滤。  
FR12: 广播失败必须具备节流日志与错误计数。  
FR13: JoinRoom 必须保持同步确认语义，确保入房结果可立即反馈。  
FR14: LeaveRoom 必须支持异步队列化处理（含去重键）。  
FR15: Leave 失败必须支持有限重试与失败计数。  
FR16: 关闭路径必须先完成本地资源解绑，再触发控制面清理流程。  
FR17: 队列容量参数必须支持 YAML 与环境变量双入口配置。  
FR18: profiling 采样参数必须支持环境变量开关，默认低开销。  
FR19: 系统必须暴露关键指标：enqueue 失败、signal 丢弃、broadcast 锁竞争、close 时延。  
FR20: 系统必须输出可用于阶段 Gate 判定的性能与稳定性统计。  
FR21: 每个 Phase 改造必须支持独立灰度发布。  
FR22: 每个 Phase 必须定义回滚触发条件与回滚路径。  
FR23: 系统必须支持按阶段进行验收并记录通过/失败结论。  
FR24: 如果架构决策发生变化，必须先回写 architecture 文档后再继续实施。

### NonFunctional Requirements

NFR1: 同负载下广播路径 p95 延迟下降 >= 20%。  
NFR2: 同负载下 goroutine 峰值下降 >= 25%。  
NFR3: 同负载下 `bucket.cLock` 阻塞样本下降 >= 40%。  
NFR4: 目标负载下消息丢弃率 < 0.5%。  
NFR5: 连接风暴场景 OnClose p95 降低 >= 50%。  
NFR6: Leave 异步重试成功率 >= 99%。  
NFR7: `go test -race ./connect-node/...` 新增 race = 0。  
NFR8: 不引入协议不兼容行为。  
NFR9: map 写操作全部使用写锁语义。  
NFR10: 暴露 drop/enqueue-fail/lock-block/close-latency 指标。  
NFR11: profiling 默认关闭，开启后 1 分钟内可采样。  
NFR12: 日志具备节流机制。  
NFR13: 参数调整路径可控且可追踪。  
NFR14: 阶段验收结果可追溯至 FR/NFR。

### Additional Requirements

- 本轮无 UI/UX 范围，跳过 UX 文档依赖。
- 后续 Story 必须引用 A3 校准后的 ADR（ADR-001~ADR-007）。
- 广播、写入、关闭路径优化必须保持 Join/Leave/心跳协议语义兼容。
- 每阶段必须提供灰度、回滚和验证脚本/清单。

### FR Coverage Map

FR1-FR4 -> Epic 2（共享写入与批处理性能）  
FR5-FR8 -> Epic 1（信号与关闭并发正确性）  
FR9-FR12 -> Epic 2（广播路径与锁竞争优化）  
FR13, FR16 -> Epic 3（控制面与会话清理）  
FR14-FR15 -> Epic 3（Leave 异步化与重试）  
FR17-FR20 -> Epic 4（配置与可观测能力）  
FR21-FR24 -> Epic 5（灰度回滚与阶段验收治理）

## Epic List

### Epic 1: 并发正确性基线修复
建立不会阻塞、不会竞态、可稳定关闭的连接与房间基础行为，为后续性能优化提供安全底座。  
**FRs covered:** FR5, FR6, FR7, FR8, FR10

### Epic 2: 写入与广播性能收敛
完成共享写入路径统一、批处理触发条件落地和广播锁范围优化，降低调度与锁竞争开销。  
**FRs covered:** FR1, FR2, FR3, FR4, FR9, FR11, FR12

### Epic 3: 控制面稳态与关闭路径治理
保持 Join 同步语义，落地 Leave 异步队列化、去重与重试，降低断连风暴影响。  
**FRs covered:** FR13, FR14, FR15, FR16

### Epic 4: 配置参数化与可观测闭环
将容量与采样策略参数化，并建立关键指标、日志节流与验收对账能力。  
**FRs covered:** FR17, FR18, FR19, FR20

### Epic 5: 发布治理与架构一致性
建立分阶段灰度/回滚与验收门禁，确保改动始终与架构决策一致。  
**FRs covered:** FR21, FR22, FR23, FR24

## Epic 1: 并发正确性基线修复

建立不会阻塞、不会竞态、可稳定关闭的连接与房间基础行为。

### Story 1.1: 修复 DelRoom 锁语义
As a 平台工程师,  
I want `DelRoom` 使用写锁执行 map 删除,  
So that 我可以消除并发未定义行为与潜在竞态。

**Covers:** FR10, NFR7, NFR9

**Acceptance Criteria:**

**Given** `bucket.go` 的房间删除逻辑  
**When** 执行并发压测与 race 检查  
**Then** 不再出现读锁写 map 的路径  
**And** `go test -race` 不新增该路径 race

### Story 1.2: Signal 非阻塞化与丢弃计数
As a 服务稳定性负责人,  
I want `Signal` 改为非阻塞唤醒并记录丢弃计数,  
So that 高并发时不会因信号阻塞拖垮处理链路。

**Covers:** FR5, FR7, FR8, NFR4

**Acceptance Criteria:**

**Given** ClientReqQueue 已有待处理请求  
**When** 信号通道达到上限  
**Then** 业务消息仍由队列语义保证消费  
**And** 信号丢弃计数可被指标读取

### Story 1.3: Close 幂等与关闭保护
As a 连接层开发者,  
I want `Close` 具备幂等与非阻塞保护,  
So that 重复关闭不会导致阻塞或 panic。

**Covers:** FR6, NFR5, NFR8

**Acceptance Criteria:**

**Given** 同一 Channel 被多路径触发关闭  
**When** 并发调用关闭逻辑  
**Then** 只执行一次有效关闭动作  
**And** 不产生 panic/长时间阻塞

### Story 1.4: 并发基线回归包
As a QA/平台协作角色,  
I want 建立并发正确性回归检查清单,  
So that Epic 1 的修复可持续防回归。

**Covers:** FR23, NFR7, NFR14

**Acceptance Criteria:**

**Given** Epic 1 代码完成  
**When** 执行 race + 压测基线脚本  
**Then** 输出可追溯的通过/失败记录  
**And** 结果可映射至对应 FR/NFR

## Epic 2: 写入与广播性能收敛

完成共享写入路径统一、批处理触发条件落地和广播锁范围优化。

### Story 2.1: 共享写路径唯一化
As a 后端开发者,  
I want 会话写入统一走 shared writer manager,  
So that 我可以消除旧路径分叉和重复调度。

**Covers:** FR1, FR3, NFR1, NFR2

**Acceptance Criteria:**

**Given** 新会话建立与注销流程  
**When** 会话触发写入和释放  
**Then** 注册/注销与 pending 状态一致  
**And** 无旧 per-session 常驻 ticker 路径残留

### Story 2.2: 批处理三触发条件验证
As a 性能工程师,  
I want 批处理支持条数/字节/超时三触发,  
So that 吞吐与尾延迟可在不同流量形态下稳定。

**Covers:** FR2, NFR1, NFR2

**Acceptance Criteria:**

**Given** 不同消息大小与速率的压测场景  
**When** 写入负载变化  
**Then** flush 可被三类条件正确触发  
**And** 统计可区分不同触发路径

### Story 2.2a: steady-state flush 长跑基准
As a 性能工程师,  
I want 为 shared writer 建立复用已注册 session 的 steady-state flush benchmark,  
So that 我可以把真实 steady-state flush 成本与 Story 2.2 的 regression sentinel 区分开来。

**Covers:** FR2, FR20, NFR1, NFR2, NFR14

**Acceptance Criteria:**

**Given** 已完成 Story 2.2 的三触发正确性验证  
**When** 执行复用已注册 session 的长跑 benchmark  
**Then** 可以观测 steady-state 下 `count` / `bytes` / `timeout` 三类 flush 的真实热点  
**And** 输出能明确说明其与 Story 2.2 regression sentinel 的差异

**Notes:**

- 本 Story 不重复验证 trigger 正确性，而是补足 steady-state 性能画像。  
- benchmark 应复用预热后的 shard/session 基线对象，避免一次性初始化成本淹没 flush 周期本体。

### Story 2.3: 广播快照后解锁推送
As a 系统架构师,  
I want 广播在锁内快照、锁外推送,  
So that 我可以降低全局锁竞争并提升广播稳定性。

**Covers:** FR9, FR11, FR12, NFR3

**Acceptance Criteria:**

**Given** bucket 内大量并发连接与广播  
**When** 执行全量广播与 room 过滤  
**Then** 推送路径不在全局锁内执行  
**And** `bucket.cLock` 阻塞样本下降满足阈值

### Story 2.3a: broadcast snapshot 分配成本优化/验证
As a 性能工程师,  
I want 针对超高并发广播下 `broadcastSnapshot` 的分配/拷贝成本进行优化或验证,  
So that 我可以在维持低锁竞争的同时，控制 snapshot 带来的 memory/copy 开销转移。

**Covers:** FR11, FR20, NFR1, NFR3, NFR14

**Acceptance Criteria:**

**Given** Story 2.3 已完成“锁内快照、锁外推送”  
**When** 执行超高并发广播 benchmark 与 profiling  
**Then** 可以量化 `broadcastSnapshot` 的 allocation/copy 成本占比  
**And** 输出是否需要进一步优化以及不回退锁范围语义的结论

**Notes:**

- 本 Story 关注 Story 2.3 之后显现的 memory/copy trade-off，而不是回到锁内 push。  
- 它是 2.3 的后续 profiling/优化 story，不要求与 2.3 同轮实现。

### Story 2.4: 队列满场景失败语义与可观测
As a 运维工程师,  
I want 队列满时有明确失败语义和限流日志,  
So that 我可以快速判断是容量问题还是功能故障。

**Covers:** FR4, FR12, NFR4, NFR10, NFR12

**Acceptance Criteria:**

**Given** shared queue 人为打满场景  
**When** 新消息入队失败  
**Then** 返回可识别失败类型并计数  
**And** 日志按节流策略输出不爆量

## Epic 3: 控制面稳态与关闭路径治理

保持 Join 同步语义，落地 Leave 异步队列化、去重与重试。

### Story 3.1: Join 同步语义守护
As a 产品负责人,  
I want 保持 JoinRoom 同步确认语义,  
So that 业务侧仍可即时判定入房成功/失败。

**Covers:** FR13, NFR8

**Acceptance Criteria:**

**Given** 用户发起 Join 请求  
**When** 控制面返回成功或失败  
**Then** 客户端立即得到一致反馈  
**And** 不因后续异步改造改变 Join 语义

### Story 3.2: Leave 异步队列与去重
As a 平台工程师,  
I want Leave 改为异步队列并按 `room:user` 去重,  
So that 连接风暴时关闭路径不会被控制面调用拖慢。

**Covers:** FR14, FR16, NFR5

**Acceptance Criteria:**

**Given** 高频断连与重复触发 Leave  
**When** 关闭路径执行清理  
**Then** Leave 请求进入异步队列且重复键被去重  
**And** OnClose p95 达成目标阈值

### Story 3.3: Leave 失败重试与告警
As a 运维值班人员,  
I want Leave 失败后可有限重试并可告警,  
So that 控制面一致性问题可恢复且可观测。

**Covers:** FR15, NFR6, NFR10

**Acceptance Criteria:**

**Given** 控制面短时不可用  
**When** Leave 首次调用失败  
**Then** 系统执行有限次数重试  
**And** 重试成功率与失败计数可被监控

## Epic 4: 配置参数化与可观测闭环

参数化容量与采样策略，形成可稳定运维的观测体系。

### Story 4.1: 容量参数双通道配置
As a 运维工程师,  
I want `svr_proto/writeBatchQueueSize` 等参数支持 YAML+ENV,  
So that 我可以按环境快速调优而不改代码。

**Covers:** FR17, NFR13

**Acceptance Criteria:**

**Given** 不同环境配置文件与环境变量  
**When** 服务启动  
**Then** 参数解析优先级符合约定  
**And** 生效值可在日志或指标中确认

### Story 4.2: Profiling 开关策略落地
As a 性能工程师,  
I want mutex/block/pprof 采样默认关闭并可开关,  
So that 常态性能不被采样噪音污染。

**Covers:** FR18, NFR11

**Acceptance Criteria:**

**Given** 默认配置启动服务  
**When** 未显式开启采样  
**Then** profiling 不产生额外常态负担  
**And** 开关打开后 1 分钟内可采到目标 profile

### Story 4.3: 关键指标统一出口
As a 监控平台开发者,  
I want drop/enqueue-fail/lock-block/close-latency 指标统一输出,  
So that 阶段 Gate 可自动化判定。

**Covers:** FR19, FR20, NFR10, NFR14

**Acceptance Criteria:**

**Given** 完整压测与线上流量场景  
**When** 采集监控指标  
**Then** 关键指标齐全且命名统一  
**And** 可生成阶段对账报表

## Epic 5: 发布治理与架构一致性

建立阶段化灰度、回滚与架构一致性守门流程。

### Story 5.1: 阶段灰度策略模板化
As a 发布经理,  
I want 每个 Phase 有标准灰度策略模板,  
So that 发布风险可控且执行一致。

**Covers:** FR21

**Acceptance Criteria:**

**Given** 任一 Phase 准备上线  
**When** 执行灰度流程  
**Then** 有明确流量比例与观测窗口  
**And** 异常触发时可立即切换回退

### Story 5.2: 回滚触发条件与路径固化
As a 值班工程师,  
I want 每个 Phase 都有可执行回滚条件与步骤,  
So that 事故处置可以标准化执行。

**Covers:** FR22

**Acceptance Criteria:**

**Given** 指标触发失败阈值  
**When** 发布被判定异常  
**Then** 按预设条件自动或半自动回滚  
**And** 回滚后关键指标恢复至基线区间

### Story 5.3: 阶段验收记录与门禁
As a 技术负责人,  
I want 每个阶段都有验收记录并作为进入下一阶段门禁,  
So that 团队能避免在未知质量状态下推进。

**Covers:** FR23, NFR14

**Acceptance Criteria:**

**Given** 阶段改造完成  
**When** 执行验收流程  
**Then** 产出结构化通过/失败记录  
**And** 未通过不得进入下一阶段

### Story 5.4: ADR 漂移检测与回写流程
As a 架构治理负责人,  
I want Story 实施若改变架构决策必须先回写 architecture,  
So that 文档与实现长期一致。

**Covers:** FR24

**Acceptance Criteria:**

**Given** Story 实施出现 ADR 偏离  
**When** 发起后续 Story 或发布  
**Then** 必须先更新 architecture 文档并完成评审  
**And** 未回写时门禁阻止继续推进
