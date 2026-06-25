# PubSub 系统优化路线图

## 📊 当前目标

**目标**: 在不扩展 Connect-Node 的前提下，通过架构优化支持更多并发用户

**约束**: Connect-Node 保持 3 个节点

---

## 🔄 优化历史

### v1: Push-Manager 队列和并发优化 ✅

**日期**: 2026-06-23  
**提交**: 7d6b981

**问题**: 1000 用户压测推送成功率仅 1.4%

**优化内容**:
- 队列大小: 1000 → 10000
- Worker 数量: 1 → 10
- 超时时间: 10秒 → 30秒

**效果**:
- ✅ 1000 用户推送成功率: 1.4% → 100%
- 瓶颈转移到 Connect-Node

---

### v2: Connect-Node 并发推送优化 ✅

**日期**: 2026-06-23  
**提交**: 92e426e

**问题**: Connect-Node 串行推送成为瓶颈

**优化内容**:
- 将 `Room.PushMsg()` 从串行改为并发
- 使用 `sync.WaitGroup` + goroutine 并发推送
- 修复 `push_room.go` HTTP 超时配置

**理论效果**:
- 单次广播耗时: 10ms → 1-2ms (5-10倍提升)
- 单节点吞吐: ~400条/秒 → ~2000条/秒

**实际测试**:
- ❌ 2000 用户推送成功率: 13.81%
- 发现新瓶颈: ETCD 服务发现

---

### v3: ETCD 扩展为 3 节点集群 🔄 测试中

**日期**: 2026-06-24  
**提交**: b9fb981

**问题**: ETCD 单节点在高并发下成为瓶颈
- Lease 超时
- Connect-Node 频繁上下线
- Push-Manager 找不到可用节点

**优化内容**:
- ETCD: 单节点 → 3节点集群
- 所有服务连接到集群: `etcd-1:2379,etcd-2:2379,etcd-3:2379`
- 修复端口冲突

**预期效果**:
- ETCD 处理能力提升 2-3 倍
- 支持 2000-3000 用户

**测试状态**: 🔄 进行中
- 测试阶段: 2000, 3000, 4000 用户
- 目标: 找到 ETCD 集群优化后的最大容量

---

## 📈 性能指标对比

| 优化版本 | 1000 用户 | 2000 用户 | 3000 用户 | 4000 用户 | 瓶颈 |
|---------|-----------|-----------|-----------|-----------|------|
| v0 (原始) | 1.4% | - | - | - | Push-Manager |
| v1 | 100% | - | - | - | Connect-Node |
| v2 | 100% | 13.81% | - | - | ETCD |
| v3 | 待测试 | 测试中 | 测试中 | 测试中 | ? |

---

## 🎯 下一步优化方案

### 方案 A: 服务发现缓存 (推荐)

**问题**: 每次推送都查询 ETCD

**方案**:
```go
// Push-Manager 改进
type ServiceCache struct {
    nodes    []string
    lastSync time.Time
    mu       sync.RWMutex
}

func (c *ServiceCache) GetNodes() []string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.nodes  // 返回缓存
}

// 后台定期刷新
func (c *ServiceCache) RefreshLoop() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        nodes := etcd.Query("connect-node")
        c.mu.Lock()
        c.nodes = nodes
        c.mu.Unlock()
    }
}
```

**预期效果**:
- 减少 ETCD 查询: 2000次/秒 → 0.1次/秒
- 支持 3000-5000 用户

---

### 方案 B: 静态服务发现

**问题**: 依赖 ETCD 动态发现

**方案**: 使用配置文件
```yaml
connect_nodes:
  - connect-node-1:50052
  - connect-node-2:50052
  - connect-node-3:50052
```

**优点**: 完全消除 ETCD 瓶颈  
**缺点**: 失去动态扩缩容

---

### 方案 C: gRPC 负载均衡

**问题**: 手动管理服务发现

**方案**:
```
Push-Manager → gRPC LB → Connect-Nodes
```

**优点**: 
- 利用 gRPC 内置负载均衡
- 不依赖 ETCD

---

### 方案 D: 消息队列架构 (长期)

**问题**: 推送和连接耦合

**方案**:
```
Web-Server → Redis Pub/Sub → Connect-Nodes
```

**优点**:
- 推送和连接完全解耦
- 无需服务发现
- 支持 10000+ 用户

**工作量**: 1-3 个月

---

## 🔍 瓶颈分析方法

每次压测后的分析步骤：

1. **检查推送成功率**
   ```bash
   grep "推送成功率" logs_*/push-*.log
   ```

2. **检查 ETCD 日志**
   ```bash
   docker logs pubsub-etcd-1 | grep -E "timeout|error"
   ```

3. **检查 Push-Manager 服务发现**
   ```bash
   docker logs pubsub-push-manager-1 | grep -E "节点上线|节点下线|未找到"
   ```

4. **检查 Connect-Node 性能**
   ```bash
   docker logs pubsub-connect-node-1 | grep -E "error|failed"
   ```

5. **分析系统资源**
   ```bash
   docker stats --no-stream
   ```

---

## ✅ 已修复的问题

1. **Push-Manager 队列溢出** (v1) ✅
2. **Connect-Node 串行推送** (v2) ✅
3. **ETCD 单点瓶颈** (v3) 🔄
4. **HTTP 超时配置不匹配** (v2) ✅
5. **端口冲突** (v3) ✅

---

## 📝 测试日志位置

- v1: `logs/` (1000 用户)
- v2: `logs_optimized/` (2000 用户)
- v3: `logs_etcd_cluster/` (2000-4000 用户)

---

**最后更新**: 2026-06-24 08:15  
**当前状态**: v3 (ETCD 集群) 测试中

---

### v4: 修复 ETCD Lease TTL 配置 🔄 测试中

**日期**: 2026-06-24  
**提交**: f341303

**问题根因分析**:
经过深入追踪，发现 v3 (ETCD 集群) 并未解决问题，真正的根因是：

1. **ETCD 在 Docker/WSL2 环境下 I/O 性能差**
   - KeepAlive 请求处理延迟: 100-380ms
   - 原配置 TTL=10s, KeepAlive 间隔=5s 太短
   
2. **Lease 续约超时导致服务频繁下线**
   ```
   Connect-Node 发送 KeepAlive (5s 间隔)
       ↓
   ETCD 处理太慢 (>100ms)
       ↓
   连续 2-3 次延迟，10s TTL 过期
       ↓
   Connect-Node 被标记下线
       ↓
   重新注册，创建新 lease
       ↓
   Push-Manager 找不到节点 → 推送失败
   ```

3. **推送成功率低的真正原因**
   - 不是 ETCD 单点瓶颈（集群也一样慢）
   - 不是并发能力不足
   - 而是 Lease TTL 配置不合理

**优化内容**:
- Lease TTL: 10秒 → 30秒 (3倍余量)
- KeepAlive 间隔: 5秒 → 10秒 (减少 ETCD 压力)

**预期效果**:
- 消除 Connect-Node 频繁上下线
- 推送成功率恢复正常（>95%）

**测试状态**: 🔄 重新构建并测试中

---

## 🔍 深度分析：为什么 ETCD 集群没有帮助？

### 问题本质

ETCD 集群（3节点）并**没有提升单个请求的处理速度**，只是提升了：
1. 整体吞吐量（更多节点分担负载）
2. 高可用性（一个节点故障不影响服务）

但对于**单个 KeepAlive 请求的延迟**，集群和单节点一样受限于：
- Docker 虚拟化开销
- WSL2 文件系统性能
- ETCD 自身的 Raft 一致性协议开销

### 正确的解决方向

**不是**：增加 ETCD 节点数  
**而是**：
1. 调整 Lease TTL 适应环境延迟 ✅
2. 减少 KeepAlive 频率 ✅
3. 长期：优化服务发现架构（缓存/静态配置）

---

## 📊 性能指标对比（更新）

| 优化版本 | 1000 用户 | 2000 用户 | 瓶颈 | 根因 |
|---------|-----------|-----------|------|------|
| v0 (原始) | 1.4% | - | Push-Manager | 队列小，并发低 |
| v1 | 100% | - | Connect-Node | 串行推送 |
| v2 | 100% | 13.81% | ETCD | ~~单点性能~~ |
| v3 (集群) | 32.6% | - | ETCD | **Lease TTL 太短** |
| v4 (TTL 修复) | 待测试 | 待测试 | ? | - |

---

**最后更新**: 2026-06-24 08:35  
**当前状态**: v4 (Lease TTL 修复) 构建中

---

## 🐛 v4 关键 Bug 修复

**发现**: v4 部署后，connect-node 仍每 11 秒续约失败一次

**根因**:
- `DefaultTTL = 30` 常量定义了，但 `registry.go:303` 仍硬编码 `ttl = 10`
- 续约间隔 10s = TTL 10s，lease 总在续约前过期
- 每次都走 `lease not found` 分支重新注册

**修复**: 
- 让代码真正使用 `DefaultTTL` 常量
- 现在: TTL 30s + 续约 10s = 3 次续约机会

**提交**: [待填写]


---

## ✅ 最终验证结果（2026-06-25）

经过完整的根因追查与修复，在**单节点 ETCD + 3 connect-node**（节点数未扩）下：

| 规模 | 连接成功率 | 推送成功率 | 消息送达率 | 节点重启 |
|------|-----------|-----------|-----------|---------|
| 1000用户/100房 | 100% | **100%** (57600/57600) | 183.7% | 0 0 0 |
| 2000用户/200房 | 100% | **100%** (115500/115500) | 194.7% | 0 0 0 |
| 3000用户/100房(30/房) | 100% | **100%** (58300/58300) | 545.4% (317960/58300) | 0 0 0 |

服务端内存占用：每 connect-node 仅 ~290MB。

### 完整根因链（已全部修复并提交）
1. **ETCD lease TTL bug**（核心）: `registry.go` 硬编码 ttl=10，续约间隔=TTL → lease 必过期 → connect-node 疯狂重注册 → push-manager 丢节点 → 推送失败。修复：ttl=DefaultTTL(30s)，续约10s=3次机会。
2. **Push-Manager 容量**: 队列1000→10000, worker 1→10/node, 超时10→30s。
3. **Connect-Node 串行推送**: 房间广播改并发。
4. **超时不对齐**: web/main.go gRPC 5→30s, benchmark HTTP 5→35s。
5. **ETCD 3节点集群方向错误**: WSL2 慢磁盘下 3 节点抢盘更糟 → 回退单节点。
6. **构建效率**: `go build -a` 使缓存失效、每次全量编译 → 移除，缓存生效。
7. **测试环境**: 主机内存耗尽(其他项目2.7GB java)、孤儿docker-proxy占端口、go run编译风暴+僵尸client耗尽临时端口 → 释放内存/换端口/预编译二进制。

### 当前瓶颈
服务端在 3000 用户下毫无压力（0重启, ~290MB/node）。进一步扩展的瓶颈是**压测机本身**（bench_client 进程被主机 OOM），非服务端。

---

## ✅ 广播吞吐维度的完整结论（2026-06-25）

继续压测发现并专项修复了一系列瓶颈（均已 commit）：

| 瓶颈 | 根因 | 修复 | commit |
|------|------|------|--------|
| push-manager 全量扫描 | Broadcast O(总连接数)/消息 | 改 BroadcastRoom O(房间人数) | 2b9aab4 |
| connect-node room-worker 入队溢出 | 队列/超时偏小 | env 调容量(256/8192/2s) | 836a9e6 |
| **单核 CPU 打满(关键)** | room.PushMsg 每消息每连接起 goroutine，而 ch.Push 只是廉价异步入队 | 改回串行遍历 | 0ecc3e3 |
| 构建奇慢(20-30min) | go build -a 使缓存失效 | 移除 -a | ef725d3 |
| 压测端假瓶颈(rate=100崩) | bench HTTP 客户端不复用连接，临时端口耗尽 | MaxIdleConnsPerHost=2000 | 4ac9e2b |

### 真实容量结论（3 connect-node 不扩，单节点 ETCD，WSL2 16核）
- **连接维度**: 3000 用户 100% 推送成功，服务端仅 ~290MB/node，0 重启。
- **广播吞吐**: 持续 ~4800 msg/s @ 100% 成功率（rate=50/房×100房）。此时 connect-node ~50-90% 单核占用——受 CPU-bound 限制，更高速率需扩 node（本目标禁止）。
- **rate=100(10000msg/s) 的"崩溃"是压测端临时端口耗尽的假象**，非服务端瓶颈（修复 HTTP 复用后服务端 CPU 在该速率下仍空闲，瓶颈在 100 个 pusher 进程的 socket churn）。

### 最终验证（2000用户/100房/rate=10，目标场景）
- 连接 2000/2000 (100%)
- 推送成功率 **100%** (57600/57600)
- 消息送达率 368.3%
- 续约失败 0/0/0，节点重启 0/0/0

### 🔍 进一步优化方向（CPU-bound吞吐天花板）

当前持续吞吐 ~4800 msg/s 时 connect-node 单核占用 ~90%（CPU-bound）。

**根因**: 房间广播时，同一 `*proto.Proto` 在 flush 时被每个 session 重复编码一次（`handler.Write` 仅依赖 proto 不依赖 session 状态，编出的 bytes 对所有接收者完全相同）。100 人房间推送一条消息 = 100 次冗余编码。

**优化方案**: 在 `room.PushMsg` 循环前编码一次，通过 writeEvent 传递预编码的 `[]byte` 而非 `*proto.Proto`，flush 时直接追加。

**收益**: 理论上可将 CPU-bound 吞吐天花板提升 ~100x（按房间人数倍数）。

**风险**: 需改 writeEvent 数据契约（增加 `preEncoded []byte` 字段或新 kind），flush 路径需区分"编码此 proto"vs"用预编码数据"。任何失误会破坏所有服务端推送。

**建议**: 当真实业务需求突破 ~4800 msg/s 且无法横向扩 connect-node 时再实施，需独立分支 + 全量回归测试。现阶段目标场景(2000用户/rate=10)已100%达成，不应在会话尾部冒险重构热路径。

---

## 🚀 v6: encode-once-per-broadcast 突破 4800 msg/s 天花板（2026-06-25）

**分支**: `perf/encode-once-per-broadcast`
**提交**: 59dfcb1

### 实施
- `writeEvent` 增加 `data []byte`（与 `msg *Proto` 二选一）
- `sharedWriteManager` 新增 `EnqueuePreEncoded`/`TryEnqueuePreEncoded`，与原路径共享分片调度
- `Channel` 新增 `serverPushBytesWriter` 回调和 `PushBytes`，自动 fallback
- `room.PushMsg` 编码一次后逐 channel 复用字节，编码失败/未装载 bytes-writer 自动回退到原 proto 路径
- 单播/系统消息路径完全不变

### 速率扫描结果（2000用户/100房×20，单进程多房间推送器）

| rate/房 | 目标推送 | **实际吞吐** | **成功率** | connect-node CPU | 重启/队列丢 |
|---------|---------|------------|----------|-----------------|-----------|
| 50 | 5000 msg/s | **4998 msg/s** | **100%** | ~55% 单核 | 0 / 0 |
| 100 | 10000 msg/s | **9561 msg/s** | **100%** | ~95% 单核 | 0 / 0 |
| 200 | 20000 msg/s | **10539 msg/s** | **100%** | ~104% 单核 | 0 / 0 |
| 400 | 40000 msg/s | 10155 msg/s | **100%** | ~94% 单核 | 0 / 0 |
| 800 | 80000 msg/s | 9862 msg/s | **100%** | ~101% 单核 | 0 / 0 |

### 关键观察
1. **吞吐天花板从 ~4800 → ~10500 msg/s**（>2x），完全发挥到接近原计算的 fan-out 100k deliveries/s 量级。
2. **零失败、零节点重启、零队列丢**，即使在压测请求 80000 msg/s 时（系统按物理上限 ~10500 msg/s 节流但所有应答都成功）。
3. **新瓶颈**: connect-node 仍是 ~1 单核饱和（rate≥200 时 ~100%/16 核）。剩余的单核消耗推测在 room.PushMsg 串行遍历 + flushShard 调度（每条广播仍要往所有接收者的 shard 投递入队事件）。
4. **fan-out 维度**: 100 人房间下，每条广播只编码 1 次但仍要 99 次额外入队（逻辑 N），后续若需进一步抬升，可考虑 shard 级批量入队。

### 安全验证
- 编译通过，go vet 无警告
- 现有 shard 单元测试 (`TestFlushTrigger*`) 全部通过
- 单播/系统消息路径完全不变（`Enqueue/TryEnqueue` + `msg *Proto`）
- 任意 channel 未升级或 bytes 入队失败均自动回退到原 proto 路径，无消息丢失风险

