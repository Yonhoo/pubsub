#!/bin/bash
# 检查广播消息是否发送到客户端

echo "=========================================="
echo "📊 广播消息检查工具"
echo "=========================================="
echo ""

# 1. 检查客户端统计日志（最新的 recv 值）
echo "1️⃣  检查客户端统计日志 (stat.log):"
if [ -f "stat.log" ]; then
    echo "   最新的统计记录:"
    tail -5 stat.log | grep "\[stat\]" | tail -3
    echo ""
    echo "   是否有收到消息 (recv > 0)?"
    if tail -30 stat.log | grep "\[stat\]" | grep -q "recv=[1-9]"; then
        echo "   ✅ 是！客户端已收到消息"
        tail -30 stat.log | grep "\[stat\]" | grep "recv=[1-9]" | tail -3
    else
        echo "   ❌ 否，recv 仍为 0"
    fi
else
    echo "   ⚠️  stat.log 文件不存在"
fi
echo ""

# 2. 检查客户端详细日志
echo "2️⃣  检查客户端详细日志 (client.log):"
if [ -f "client.log" ]; then
    echo "   最近收到的广播消息:"
    tail -100 client.log | grep -E "received broadcast|收到.*消息" | tail -5
    if [ $? -eq 0 ]; then
        echo "   ✅ 客户端日志显示有收到消息"
    else
        echo "   ⚠️  客户端日志中没有找到收到消息的记录"
    fi
else
    echo "   ⚠️  client.log 文件不存在（可能没有指定 -log 参数）"
fi
echo ""

# 3. 检查 Web-Server 日志（如果知道日志位置）
echo "3️⃣  检查 Web-Server 日志:"
echo "   请检查运行 web/main.go 的终端，应该看到:"
echo "   📡 收到广播请求: room=room-001, message=..."
echo "   ✅ 广播成功"
echo ""

# 4. 检查 Connect-Node 日志
echo "4️⃣  检查 Connect-Node 日志:"
echo "   请检查运行 connect-node 的终端，应该看到:"
echo "   📡 [ConnectNodeServer] 收到 Broadcast gRPC 请求"
echo "   📢 [Bucket] Broadcast 被调用"
echo "   ✅ [Bucket] Broadcast 完成: 成功=X"
echo "   📤 [ProtoHandler] 收到服务端推送消息"
echo "   ✅ [ProtoHandler] 服务端推送消息已发送给客户端"
echo ""

# 5. 实时监控（如果客户端正在运行）
echo "5️⃣  实时监控建议:"
echo "   在另一个终端运行以下命令实时查看统计:"
echo "   watch -n 1 'tail -1 benchmark/stat.log'"
echo ""

echo "=========================================="
echo "💡 提示："
echo "   - 如果 recv=0，检查服务端日志确认消息是否被推送"
echo "   - 确认客户端已成功加入房间（查看客户端日志）"
echo "   - 确认客户端已订阅 op=2（查看 connect-node 日志）"
echo "=========================================="


