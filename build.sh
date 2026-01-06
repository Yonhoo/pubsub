#!/bin/bash

set -e

echo "======================================"
echo "   PubSub 系统 Docker 构建脚本"
echo "======================================"
echo ""

# 启用 Docker BuildKit（支持缓存挂载）
export DOCKER_BUILDKIT=1

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 构建函数
build_image() {
    local service=$1
    local dockerfile=$2
    local tag=$3
    
    echo -e "${YELLOW}构建 ${service}...${NC}"
    docker build -f ${dockerfile} -t ${tag} .
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ ${service} 构建成功${NC}"
    else
        echo -e "${RED}❌ ${service} 构建失败${NC}"
        exit 1
    fi
    echo ""
}



echo "🔍 检测 Docker 环境..."
echo "Docker 版本: $(docker --version)"
echo "Docker Compose 版本: $(docker-compose --version)"
echo ""

# 构建所有镜像
echo "🏗️  开始构建镜像..."
echo ""

build_image "Controller-Manager" "Dockerfile.controller" "pubsub-controller:latest"
build_image "Connect-Node" "Dockerfile.connect-node" "pubsub-connect-node:latest"
build_image "Push-Manager" "Dockerfile.push-manager" "pubsub-push-manager:latest"

echo -e "${GREEN}======================================"
echo "   ✅ 所有镜像构建完成！"
echo "======================================${NC}"
echo ""
echo "📋 已构建的镜像:"
docker images | grep pubsub

echo ""
echo "🚀 下一步操作:"
echo "  1. 启动服务: make start 或 docker-compose up -d"
echo "  2. 查看状态: make ps 或 docker-compose ps"
echo "  3. 查看日志: make logs 或 docker-compose logs -f"
echo "  4. 健康检查: make health"
echo ""


