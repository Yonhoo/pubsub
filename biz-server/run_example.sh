#!/bin/bash

# PubSub 客户端示例脚本

set -e

echo "======================================"
echo "   PubSub 客户端示例"
echo "======================================"
echo ""

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 编译客户端
echo -e "${YELLOW}📦 编译客户端...${NC}"
go build -o biz-client .
echo -e "${GREEN}✅ 编译成功${NC}"
echo ""

# 显示菜单
echo "请选择运行模式:"
echo ""
echo "  1) WebSocket 客户端 - 连接并监听消息"
echo "  2) gRPC 客户端 - 发送广播消息"
echo "  3) 完整示例 - WebSocket + gRPC (推荐)"
echo "  4) 多用户测试 - 启动3个WebSocket客户端"
echo ""
read -p "请输入选项 (1-4): " choice
echo ""

case $choice in
  1)
    echo -e "${BLUE}🚀 启动 Getty WebSocket 客户端${NC}"
    echo ""
    ./biz-client -mode=ws \
      -connect-node="localhost:8083" \
      -user-id="user-001" \
      -user-name="测试用户" \
      -room-id="room-001"
    ;;
    
  2)
    echo -e "${BLUE}🚀 启动 gRPC 客户端${NC}"
    echo ""
    ./biz-client -mode=grpc \
      -push-manager="localhost:50053" \
      -room-id="room-001" \
      -user-id="user-001" \
      -message="Hello from gRPC!"
    ;;
    
  3)
    echo -e "${BLUE}🚀 启动完整示例${NC}"
    echo ""
    ./biz-client -mode=both \
      -connect-node="localhost:8083" \
      -push-manager="localhost:50053" \
      -user-id="user-001" \
      -user-name="测试用户" \
      -room-id="room-001" \
      -message="测试广播消息"
    ;;
    
  4)
    echo -e "${BLUE}🚀 启动多用户测试${NC}"
    echo ""
    echo "启动 3 个 WebSocket 客户端..."
    echo ""
    
    # 启动 3 个客户端
    ./biz-client -mode=ws \
      -connect-node="localhost:8083" \
      -user-id="alice" \
      -user-name="Alice" \
      -room-id="chat-room" &
    PID1=$!
    
    sleep 1
    
    ./biz-client -mode=ws \
      -connect-node="localhost:8081" \
      -user-id="bob" \
      -user-name="Bob" \
      -room-id="chat-room" &
    PID2=$!
    
    sleep 1
    
    ./biz-client -mode=ws \
      -connect-node="localhost:8082" \
      -user-id="charlie" \
      -user-name="Charlie" \
      -room-id="chat-room" &
    PID3=$!
    
    echo ""
    echo -e "${GREEN}✅ 3 个客户端已启动${NC}"
    echo ""
    echo "等待 5 秒后发送广播消息..."
    sleep 5
    
    echo ""
    echo -e "${YELLOW}📢 发送广播消息...${NC}"
    ./biz-client -mode=grpc \
      -push-manager="localhost:50053" \
      -room-id="chat-room" \
      -message="大家好！这是一条广播消息。"
    
    echo ""
    echo "按 Ctrl+C 停止所有客户端"
    
    # 等待用户中断
    trap "kill $PID1 $PID2 $PID3 2>/dev/null; exit" INT TERM
    wait
    ;;
    
  *)
    echo -e "${YELLOW}⚠️  无效选项${NC}"
    exit 1
    ;;
esac

