#!/bin/bash

# Controller-Manager 健康检查脚本
# 用法: ./check-controller-health.sh

echo "🏥 Controller-Manager 健康检查"
echo "========================================"
echo ""

# 1. 检查 Docker 容器状态
echo "1️⃣  检查 Docker 容器状态..."
if docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep -q "pubsub-controller-1"; then
    echo "✅ 容器正在运行:"
    docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep "pubsub-controller-1"
    CONTAINER_RUNNING=true
else
    echo "❌ 容器未运行"
    echo ""
    echo "检查已停止的容器:"
    docker ps -a --format "table {{.Names}}\t{{.Status}}" | grep "pubsub-controller-1" || echo "   找不到容器"
    CONTAINER_RUNNING=false
fi
echo ""

# 2. 检查最新日志（不管容器是否运行）
echo "2️⃣  检查最新日志（最后 10 行）..."
if [ -f "logs/controller-1/controller.log" ]; then
    echo "---"
    tail -10 logs/controller-1/controller.log
    echo "---"
    
    # 检查日志中是否有成功启动的标志
    if grep -q "✅ Controller Manager 运行中" logs/controller-1/controller.log; then
        echo "✅ 日志显示服务曾经启动成功"
    else
        echo "❌ 日志中没有找到启动成功标志"
    fi
    
    # 检查是否有关闭信息
    if tail -20 logs/controller-1/controller.log | grep -q "🛑 正在关闭服务"; then
        echo "⚠️  日志显示服务已被关闭"
    fi
else
    echo "❌ 日志文件不存在: logs/controller-1/controller.log"
fi
echo ""

# 3. 检查 gRPC 端口（仅当容器运行时）
if [ "$CONTAINER_RUNNING" = true ]; then
    echo "3️⃣  检查 gRPC 端口 (50051)..."
    if nc -z localhost 50051 2>/dev/null; then
        echo "✅ 端口 50051 可访问"
    else
        echo "❌ 端口 50051 不可访问"
    fi
    echo ""

    # 4. 测试 gRPC 服务（需要 grpcurl）
    echo "4️⃣  测试 gRPC 服务..."
    if command -v grpcurl &> /dev/null; then
        echo "尝试列出 gRPC 服务..."
        if grpcurl -plaintext localhost:50051 list 2>/dev/null; then
            echo "✅ gRPC 服务正常响应"
        else
            echo "❌ gRPC 服务无响应"
        fi
    else
        echo "⚠️  grpcurl 未安装，跳过 gRPC 测试"
        echo "   安装: brew install grpcurl"
    fi
    echo ""

    # 5. 检查 Docker 日志
    echo "5️⃣  检查 Docker 容器日志（最后 10 行）..."
    echo "---"
    docker logs --tail 10 pubsub-controller-1 2>&1
    echo "---"
else
    echo "⏭️  容器未运行，跳过端口和服务检查"
fi
echo ""

# 6. 总结
echo "========================================"
echo "📊 健康检查总结"
echo "========================================"

if [ "$CONTAINER_RUNNING" = true ]; then
    if nc -z localhost 50051 2>/dev/null; then
        echo "✅ Controller-Manager 运行正常"
        echo ""
        echo "💡 测试命令:"
        echo "   grpcurl -plaintext localhost:50051 list"
        echo "   grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats"
    else
        echo "⚠️  容器运行中，但端口不可访问"
        echo ""
        echo "🔍 可能的问题:"
        echo "   1. 服务正在启动中（等待几秒后重试）"
        echo "   2. gRPC Server 启动失败（查看日志）"
        echo "   3. 端口映射配置错误（检查 docker-compose.yml）"
    fi
else
    echo "❌ Controller-Manager 未运行"
    echo ""
    echo "🔍 检查失败原因:"
    echo "   docker logs pubsub-controller-1"
    echo ""
    echo "🚀 启动服务:"
    echo "   docker-compose up -d controller"
    echo "   或运行: ./rebuild-all.sh"
fi
echo ""

