.PHONY: help build-images start stop restart logs clean rebuild

# 默认目标
help:
	@echo "可用命令:"
	@echo "  make build-images  - 构建所有 Docker 镜像"
	@echo "  make start        - 启动所有服务"
	@echo "  make stop         - 停止所有服务"
	@echo "  make restart      - 重启所有服务"
	@echo "  make logs         - 查看所有服务日志"
	@echo "  make logs-controller - 查看 Controller 日志"
	@echo "  make logs-connect - 查看 Connect-Node 日志"
	@echo "  make logs-push    - 查看 Push-Manager 日志"
	@echo "  make clean        - 清理所有容器和数据卷"
	@echo "  make rebuild      - 重新构建并启动"
	@echo "  make ps           - 查看服务状态"

# 构建所有镜像
build-images:
	@echo "构建 Controller-Manager 镜像..."
	docker build -f Dockerfile.controller -t pubsub-controller:latest .
	@echo "构建 Connect-Node 镜像..."
	docker build -f Dockerfile.connect-node -t pubsub-connect-node:latest .
	@echo "构建 Push-Manager 镜像..."
	docker build -f Dockerfile.push-manager -t pubsub-push-manager:latest .
	@echo "所有镜像构建完成！"

# 启动所有服务
start:
	@echo "启动所有服务..."
	docker-compose up -d
	@echo "等待服务启动..."
	sleep 5
	@echo "服务状态:"
	docker-compose ps

# 停止所有服务
stop:
	@echo "停止所有服务..."
	docker-compose stop

# 重启所有服务
restart:
	@echo "重启所有服务..."
	docker-compose restart

# 查看所有日志
logs:
	docker-compose logs -f

# 查看 Controller 日志
logs-controller:
	docker-compose logs -f controller

# 查看 Connect-Node 日志
logs-connect:
	docker-compose logs -f connect-node-1 connect-node-2 connect-node-3

# 查看 Push-Manager 日志
logs-push:
	docker-compose logs -f push-manager

# 查看服务状态
ps:
	docker-compose ps

# 清理所有容器和数据卷
clean:
	@echo "停止并删除所有容器..."
	docker-compose down -v
	@echo "清理完成！"

# 重新构建并启动
rebuild: clean build-images start

# 只构建业务服务镜像（不包括基础服务）
build-services:
	docker-compose build controller connect-node-1 connect-node-2 connect-node-3 push-manager

# 扩展 Connect-Node
scale-connect:
	docker-compose up -d --scale connect-node-1=3

# 健康检查
health:
	@echo "检查服务健康状态..."
	@echo "\n=== MySQL ==="
	@docker exec pubsub-mysql mysqladmin ping -h localhost -u root -proot123 2>/dev/null && echo "✅ MySQL OK" || echo "❌ MySQL Failed"
	@echo "\n=== Redis ==="
	@docker exec pubsub-redis redis-cli ping 2>/dev/null && echo "✅ Redis OK" || echo "❌ Redis Failed"
	@echo "\n=== ETCD ==="
	@docker exec pubsub-etcd etcdctl endpoint health 2>/dev/null && echo "✅ ETCD OK" || echo "❌ ETCD Failed"
	@echo "\n=== Controller ==="
	@curl -s http://localhost:9090/metrics > /dev/null && echo "✅ Controller OK" || echo "❌ Controller Failed"
	@echo "\n=== Connect-Node-1 ==="
	@curl -s http://localhost:8080/health > /dev/null && echo "✅ Connect-Node-1 OK" || echo "❌ Connect-Node-1 Failed"
	@echo "\n=== Push-Manager ==="
	@curl -s http://localhost:9095/metrics > /dev/null && echo "✅ Push-Manager OK" || echo "❌ Push-Manager Failed"


