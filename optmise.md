这个问题很关键，我按你现在的代码和这批 pprof 结果，给你一份按收益排序的优化清单。

每连接一个 batcher + ticker 是当前最大可优化点。
文件：server_websocket.go (line 311)、server_websocket.go (line 740)、server_websocket.go (line 760)
结论：每个 session 都有一个 goroutine 和 500ms ticker，会持续制造调度唤醒；这和你看到的 runtime.futex / findRunnable 特征一致。
建议：改成“按需启动定时器（有待刷数据才计时）”或“共享 flush worker”，不要每连接常驻 ticker。

channel.Signal/Close 是阻塞发送，存在卡住/放大 futex 的风险。
文件：channel.go (line 144)、channel.go (line 148)
结论：Signal() 和 Close() 都是直接 c.signal <- ...，通道满时会阻塞。高并发广播或关闭风暴时容易形成阻塞链。
建议：改成非阻塞 select { case ...: default: } + 关闭标记（CAS）保证只发一次 finish。

Bucket.Broadcast 持有全局读锁做全量遍历 + push，临界区过大。
文件：bucket.go (line 181) 到 (line 202)
结论：广播时长时间占着 b.cLock.RLock，会压住 Put/Del/ChangeRoom。
建议：先在锁内快照 channel 列表，解锁后再 NeedPush/Push。

有一个明确的并发正确性问题：RLock 下 delete map。
文件：bucket.go (line 213) 到 (line 216)
结论：DelRoom 用 RLock 后 delete(b.rooms, ...)，这是错误用法，既可能出错也可能导致异常竞争行为。
建议：改成 Lock/Unlock。

当前队列容量配置偏大，会显著放大内存和调度成本。
文件：config.yaml (line 45)、server_websocket.go (line 34)
结论：protocol.svr_proto=10240 + writeBatchQueueSize=1024，每连接内存和排队都很重。
建议：先降到 svr_proto=256~1024、writeBatchQueueSize=64~256，配合丢弃统计观察。

profiling 采样默认常开，会影响压测结论。
文件：main.go (line 49)、main.go (line 50)
结论：SetMutexProfileFraction(5) 和 SetBlockProfileRate(1000000) 一直开着，会增加运行时负担。
建议：改成环境变量开关，只在采样窗口打开。

连接建立/关闭路径里同步 gRPC 调用会放大抖动。
文件：server_websocket.go (line 423)、server_websocket.go (line 553)
结论：Join/Leave 在会话线程里同步 RPC，峰值连接波动时会拖慢会话处理。
建议：至少把 LeaveRoom 异步化到 worker 队列并做去重。

我建议先按这个顺序落地：1 -> 2 -> 4 -> 3，这四项对你现在的 futex 和吞吐最直接。
如果你同意，我下一步就直接提交这四项代码改动。



有，而且你这套代码里还能再做几类“对象池 + batch”优化，按收益排序我建议这样做：

广播预编码 + 引用计数复用（最高收益）
位置：bucket.go、server_websocket.go、codec.go
现状是每个 session 都会 handler.Write() 一次并 make([]byte, packLen)。
优化为：同一条广播消息只编码一次，session 侧直接发 []byte，编码结果用对象池+refcnt 回收。这样能显著减少分配和序列化 CPU。

batch 增加“按字节阈值”触发
位置：server_websocket.go
现在只有 batchSize + 500ms。建议加 maxBatchBytes（比如 32KB/64KB），满足任一条件就 flush：
len(batch)>=N 或 bytes>=maxBatchBytes 或 age>=timeout。
这样吞吐和尾延迟更稳。

batch 容器池化
位置：server_websocket.go
[][]byte 每轮 append/grow 也有分配成本。用 sync.Pool 复用 batch 切片（固定 cap=32/64），减少 GC 压力。

房间广播微批（room fanout 批处理）
位置：bucket.go、room.go
在 roomprc 里对短时间窗口（如 1~5ms）内同房间消息聚合，再一次遍历房间连接下发，减少锁开销和遍历次数。

Join/Leave 异步队列化 + 去重
位置：server_websocket.go
把 LeaveRoom 从连接关闭路径改成异步 worker（带去重 map），降低高并发断联时 gRPC 突刺。

仅在采样时开启 profile
位置：main.go
SetMutexProfileFraction/SetBlockProfileRate 建议走 env 开关，避免常态额外开销。




房间扇出模型升级
从“全局 map/锁遍历”转成“room actor + 一致性哈希分片（room->worker）”。
广播消息预编码一次，扇出复用 bytes（对象池+引用计数回收）。
这类思路在 goim（comet/logic/job 分层）和 Centrifugo（channel/hash/shard）里都能看到。


服务拆分
把 websocket 接入层从业务层剥离（类似 Rocket.Chat 的 ddp-streamer-service），业务通过消息总线交互。
适合你后续多节点扩容和故障隔离。


把“房间聚合”前移到分发阶段，而不是仅连接侧 batch。
位置：bucket.go、server_websocket.go。
你现在的 per-session batch 已验证会受客户端拆包能力影响；房间侧聚合收益更稳定。

压缩只在分发层做一次（按阈值触发），接入层直接复用压缩结果。
位置：消息分发到接入节点的链路（你的 push-manager/connect-node 之间）。
避免每连接重复 CPU 开销。





