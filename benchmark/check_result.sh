#!/bin/bash
# 检查广播消息是否发送到客户端

echo "=========================================="
echo "📊 广播消息检查报告"
echo "=========================================="
echo "检查时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# 1. 检查统计日志
echo "1️⃣  客户端统计日志 (stat.log):"
if [ -f "stat.log" ]; then
    last_stat=$(tail -1 stat.log)
    last_time=$(stat -c "%y" stat.log | cut -d' ' -f2 | cut -d'.' -f1)
    echo "   最后更新时间: $last_time"
    echo "   最后一条记录: $last_stat"
    echo ""
    
    # 检查是否有收到消息
    recv_count=$(grep -o "recv=[0-9]*" stat.log | grep -v "recv=0" | wc -l)
    if [ $recv_count -gt 0 ]; then
        echo "   ✅ 发现收到消息的记录！"
        grep "recv=[1-9]" stat.log | tail -5
    else
        echo "   ❌ 未发现收到消息的记录（recv 全部为 0）"
    fi
else
    echo "   ⚠️  stat.log 文件不存在"
fi
echo ""

# 2. 检查客户端进程
echo "2️⃣  客户端进程状态:"
if pgrep -f "client.go" > /dev/null; then
    echo "   ✅ 客户端进程正在运行"
    ps aux | grep "client.go" | grep -v grep | head -1
else
    echo "   ❌ 客户端进程未运行"
fi
echo ""

# 3. 检查服务端日志（connect-node）
echo "3️⃣  服务端推送日志检查:"
echo "   请检查运行 connect-node 的终端，查找以下日志:"
echo "   - 📡 [ConnectNodeServer] 收到 Broadcast gRPC 请求"
echo "   - ✅ [Bucket] Broadcast 完成: 成功=X"
echo "   - 📤 [ProtoHandler] 收到服务端推送消息"
echo ""

# 4. 检查 Web-Server 日志
echo "4️⃣  Web-Server 日志检查:"
echo "   请检查运行 web/main.go 的终端，应该看到:"
echo "   - 📡 收到广播请求: room=room-001"
echo "   - ✅ 广播成功"
echo ""

# 5. 建议
echo "5️⃣  诊断建议:"
if [ -f "stat.log" ]; then
    last_time_str=$(stat -c "%y" stat.log | cut -d' ' -f1-2)
    last_time_epoch=$(date -d "$last_time_str" +%s)
    now_epoch=$(date +%s)
    diff=$((now_epoch - last_time_epoch))
    
    if [ $diff -gt 60 ]; then
        echo "   ⚠️  统计日志已停止更新超过 $diff 秒"
        echo "   💡 建议：重新启动客户端程序"
        echo "   💡 命令：go run client.go -room=room-001 -users=1000 -ws=ws://localhost:8083/connect -samples=30 -log=client.log -stat=stat.log"
    fi
fi

echo ""
echo "=========================================="
echo "💡 快速测试方法:"
echo "   1. 手动发送一条测试消息:"
echo "      curl -X POST http://localhost:8086/broadcast \\"
echo "        -H 'Content-Type: application/json' \\"
echo "        -d '{\"room_id\": \"room-001\", \"message\": \"test\"}'"
echo ""
echo "   2. 立即查看 stat.log:"
echo "      tail -1 stat.log"
echo ""
echo "   3. 如果 recv 增加，说明消息推送成功！"
echo "=========================================="


