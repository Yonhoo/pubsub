# Docker 快速启动指南

## 一键启动

### 方式 1: 使用构建脚本（推荐）

```bash
# 1. 构建镜像
./build.sh

# 2. 启动服务
make start

# 3. 健康检查
make health
```

### 方式 2: 使用 Makefile

```bash
# 构建并启动
make rebuild

# 或分步操作
make build-images
make start
```

### 方式 3: 直接使用 Docker Compose

```bash
# 构建镜像
docker-compose build

# 启动服务
docker-compose up -d

# 查看状态
docker-compose ps
```

## 验证服务

### 1. 查看所有服务状态

```bash
make ps
```

期望输出：
```
NAME                      STATUS              PORTS
pubsub-controller-1       Up 30 seconds       0.0.0.0:50051->50051/tcp, 0.0.0.0:9090->9090/tcp
pubsub-connect-node-1     Up 30 seconds       0.0.0.0:8080->8080/tcp, 0.0.0.0:50052->50052/tcp
pubsub-connect-node-2     Up 30 seconds       0.0.0.0:8081->8080/tcp, 0.0.0.0:50055->50052/tcp
pubsub-connect-node-3     Up 30 seconds       0.0.0.0:8082->8080/tcp, 0.0.0.0:50056->50052/tcp
pubsub-push-manager-1     Up 30 seconds       0.0.0.0:50053->50053/tcp
pubsub-mysql              Up 1 minute         0.0.0.0:3306->3306/tcp
pubsub-redis              Up 1 minute         0.0.0.0:6379->6379/tcp
pubsub-etcd               Up 1 minute         0.0.0.0:2379-2380->2379-2380/tcp
```

### 2. 健康检查

```bash
make health
```

### 3. 测试 WebSocket 连接

```bash
# 查看 Connect-Node 状态
curl http://localhost:8080/stats

# 输出示例:
# {"node_id":"connect-node-1","node_address":"connect-node-1:50052","connections":0,"rooms":0}
```

### 4. 测试 gRPC 服务

```bash
# 安装 grpcurl (如果没有)
# macOS: brew install grpcurl
# Linux: 从 GitHub 下载

# 查看 Controller 服务
grpcurl -plaintext localhost:50051 list

# 查看房间统计
grpcurl -plaintext localhost:50051 pubsub.ControllerService/GetRoomStats
```

## 测试完整流程

### 1. 启动客户端（使用浏览器）

创建一个简单的 HTML 文件 `test-client.html`:

```html
<!DOCTYPE html>
<html>
<head>
    <title>PubSub 测试客户端</title>
</head>
<body>
    <h1>PubSub 测试客户端</h1>
    <div>
        <input type="text" id="userId" placeholder="用户ID" value="user-001">
        <input type="text" id="userName" placeholder="用户名" value="Alice">
        <input type="text" id="roomId" placeholder="房间ID" value="room-001">
        <button onclick="connect()">连接</button>
        <button onclick="disconnect()">断开</button>
    </div>
    <div id="status">未连接</div>
    <div id="messages" style="border:1px solid #ccc; height:300px; overflow-y:auto; margin-top:10px;"></div>

    <script>
        let ws = null;

        function connect() {
            const userId = document.getElementById('userId').value;
            const userName = document.getElementById('userName').value;
            const roomId = document.getElementById('roomId').value;
            
            const url = `ws://localhost:8080/ws?user_id=${userId}&user_name=${userName}&room_id=${roomId}`;
            
            ws = new WebSocket(url);
            
            ws.onopen = () => {
                document.getElementById('status').innerText = '✅ 已连接';
                addMessage('系统', '连接成功');
            };
            
            ws.onmessage = (event) => {
                const data = JSON.parse(event.data);
                addMessage('收到消息', JSON.stringify(data, null, 2));
            };
            
            ws.onclose = () => {
                document.getElementById('status').innerText = '❌ 已断开';
                addMessage('系统', '连接关闭');
            };
            
            ws.onerror = (error) => {
                addMessage('错误', error.toString());
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
                ws = null;
            }
        }

        function addMessage(type, msg) {
            const div = document.getElementById('messages');
            const time = new Date().toLocaleTimeString();
            div.innerHTML += `<div><strong>[${time}] ${type}:</strong> ${msg}</div>`;
            div.scrollTop = div.scrollHeight;
        }
    </script>
</body>
</html>
```

在浏览器中打开这个文件，点击"连接"按钮。

### 2. 推送消息

```bash
# 使用 biz-server 示例推送消息到房间
cd biz-server

go run example_client.go \
  --push-manager localhost:50053 \
  --action push-to-room \
  --room room-001 \
  --message "Hello Docker!"
```

### 3. 观察日志

```bash
# 查看所有服务日志
make logs

# 或查看特定服务
make logs-controller
make logs-connect
make logs-push
```

## 监控和调试

### 1. 访问 Jaeger UI（链路追踪）

```bash
open http://localhost:16686
```

在 Jaeger UI 中可以看到：
- 服务调用链路
- 请求延迟
- 错误追踪

### 2. 查看 Metrics

```bash
# Controller Metrics
curl http://localhost:9090/metrics

# Connect-Node-1 Metrics
curl http://localhost:9091/metrics

# Push-Manager Metrics
curl http://localhost:9095/metrics
```

### 3. 进入容器调试

```bash
# 进入 Controller 容器
docker exec -it pubsub-controller-1 sh

# 进入 MySQL 容器
docker exec -it pubsub-mysql mysql -u pubsub -ppubsub123 pubsub

# 查询房间信息
# mysql> SELECT * FROM rooms;
# mysql> SELECT * FROM room_users WHERE left_at IS NULL;
```

## 常见问题

### 1. 端口已被占用

如果端口冲突，修改 `docker-compose.yml` 中的端口映射：

```yaml
ports:
  - "13306:3306"  # 改为其他端口
```

### 2. 服务启动失败

查看日志：
```bash
docker-compose logs controller
```

常见原因：
- 数据库连接失败 → 检查 MySQL 是否启动
- ETCD 连接失败 → 检查 ETCD 是否健康

### 3. 无法连接 WebSocket

检查 Connect-Node 是否启动：
```bash
curl http://localhost:8080/health
```

检查节点是否注册到 ETCD：
```bash
docker exec pubsub-etcd etcdctl get /services/connect-node/ --prefix
```

### 4. 镜像构建失败

清理并重新构建：
```bash
make clean
docker system prune -a
make build-images
```

## 停止和清理

### 停止服务

```bash
make stop
# 或
docker-compose stop
```

### 完全清理（包括数据）

```bash
make clean
# 或
docker-compose down -v
```

### 只清理容器，保留数据

```bash
docker-compose down
```

## 下一步

- [完整系统文档](./README.md)
- [Docker 部署详细指南](./README_DOCKER.md)
- [系统架构说明](./ARCHITECTURE_SIMPLIFIED.md)
- [生产环境部署建议](./PRODUCTION_DEPLOYMENT.md)

## 技术支持

如有问题，请检查：
1. `make logs` - 查看所有日志
2. `make health` - 健康检查
3. `docker-compose ps` - 服务状态


