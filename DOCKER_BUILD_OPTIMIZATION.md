# Docker 构建优化说明

## 优化内容

### 1. 启用 BuildKit 缓存挂载

所有 Dockerfile 现在使用 `--mount=type=cache` 来缓存：
- `/go/pkg/mod` - Go modules 缓存
- `/root/.cache/go-build` - Go 编译缓存

这意味着：
- ✅ **首次构建**：正常下载依赖
- ✅ **后续构建**：直接使用缓存，几乎不需要重新下载

### 2. 配置 Go 代理加速

使用国内镜像加速依赖下载：
```dockerfile
ENV GOPROXY=https://goproxy.cn,direct
```

如果你在国外，可以改为：
```dockerfile
ENV GOPROXY=https://proxy.golang.org,direct
```

### 3. 优化构建上下文

创建了 `.dockerignore` 文件，排除不必要的文件：
- Git 文件
- 文档文件
- IDE 配置
- 测试文件
- 临时文件

这减少了发送到 Docker daemon 的数据量。

### 4. 层缓存策略

Dockerfile 已经使用了正确的层缓存策略：
```dockerfile
# 1. 先复制 go.mod 和 go.sum（很少变化）
COPY go.mod go.sum ./
RUN go mod download

# 2. 再复制源代码（经常变化）
COPY . .
RUN go build ...
```

这样当源代码变化时，`go mod download` 层不会失效。

## 使用方法

### 直接运行（推荐）

```bash
./build.sh
```

脚本已自动启用 `DOCKER_BUILDKIT=1`。

### 手动构建单个服务

```bash
export DOCKER_BUILDKIT=1
docker build -f Dockerfile.controller -t pubsub-controller:latest .
```

### 清理缓存（如果需要）

如果遇到缓存问题，可以清理：

```bash
# 清理构建缓存
docker builder prune

# 清理所有缓存（包括 Go modules）
docker builder prune -a
```

## 性能对比

### 优化前
- **首次构建**: ~5-10 分钟
- **再次构建**: ~5-10 分钟（每次都重新下载）

### 优化后
- **首次构建**: ~3-5 分钟（使用国内镜像加速）
- **再次构建**: ~30 秒 - 1 分钟（使用缓存）
- **仅修改源代码**: ~20-30 秒（跳过 go mod download）

## 注意事项

1. **BuildKit 要求**：需要 Docker 18.09+ 版本
2. **缓存位置**：缓存存储在 Docker 的 BuildKit 缓存中，不占用镜像空间
3. **CI/CD**：在 CI/CD 环境中也能使用，但需要配置缓存持久化

## 故障排查

### 如果构建还是很慢

1. **检查 BuildKit 是否启用**：
   ```bash
   echo $DOCKER_BUILDKIT
   # 应该输出: 1
   ```

2. **检查网络连接**：
   ```bash
   curl -I https://goproxy.cn
   ```

3. **查看构建日志**：
   ```bash
   docker build --progress=plain -f Dockerfile.controller -t test .
   ```

4. **清理并重建**：
   ```bash
   docker builder prune -a
   ./build.sh
   ```

### 如果遇到 "unknown flag: --mount"

说明 BuildKit 未启用，确保：
```bash
export DOCKER_BUILDKIT=1
```

或在 `/etc/docker/daemon.json` 中永久启用：
```json
{
  "features": {
    "buildkit": true
  }
}
```

## 更多优化建议

### 使用 Docker Compose 构建

```bash
docker-compose build --parallel
```

这会并行构建所有服务，进一步加快速度。

### 使用本地 Go module 代理

如果团队内部有多人开发，可以搭建本地 Go module 代理（如 Athens）：
```dockerfile
ENV GOPROXY=http://your-athens-server:3000,https://goproxy.cn,direct
```







