#!/bin/bash
# Web-Server 启动脚本
# 确保使用 localhost 而不是服务名

cd "$(dirname "$0")"

# 设置环境变量，使用 localhost
export PUSH_MANAGER_ADDR=localhost:50053
export WEB_PORT=8086

# 如果 web-server 不存在，先编译
if [ ! -f "./web-server" ]; then
    echo "🔨 编译 web-server..."
    go build -o web-server main.go
    if [ $? -ne 0 ]; then
        echo "❌ 编译失败"
        exit 1
    fi
fi

echo "🚀 启动 Web-Server..."
echo "   Push-Manager: $PUSH_MANAGER_ADDR"
echo "   Port: $WEB_PORT"
echo ""

# 启动 web-server
./web-server


