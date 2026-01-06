# 环境设置指南

## ⚠️ 安装 Protocol Buffers 编译器

Controller Manager 需要先生成 gRPC 代码，这需要安装 **protoc** 和相关插件。

### 方法 1: 使用 Homebrew (推荐 macOS)

```bash
brew install protobuf
```

### 方法 2: 手动下载

访问 [Protocol Buffers Releases](https://github.com/protocolbuffers/protobuf/releases)，下载适合你系统的版本：

**macOS (Apple Silicon):**
```bash
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v25.1/protoc-25.1-osx-aarch_64.zip
unzip protoc-25.1-osx-aarch_64.zip -d $HOME/.local
export PATH="$PATH:$HOME/.local/bin"
```

**macOS (Intel):**
```bash
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v25.1/protoc-25.1-osx-x86_64.zip
unzip protoc-25.1-osx-x86_64.zip -d $HOME/.local
export PATH="$PATH:$HOME/.local/bin"
```

**Linux (x86_64):**
```bash
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v25.1/protoc-25.1-linux-x86_64.zip
unzip protoc-25.1-linux-x86_64.zip -d $HOME/.local
export PATH="$PATH:$HOME/.local/bin"
```

### 安装 Go 插件

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

确保 `$GOPATH/bin` 或 `$HOME/go/bin` 在你的 PATH 中：

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 验证安装

```bash
# 检查 protoc
protoc --version
# 应该输出: libprotoc 3.x.x 或更高

# 检查 Go 插件
ls $(go env GOPATH)/bin/ | grep protoc
# 应该看到:
# protoc-gen-go
# protoc-gen-go-grpc
```

## 📦 生成 Proto 代码

```bash
cd /Users/yon/repo/psrpc/examples/pubsub/proto
chmod +x gen.sh
./gen.sh
```

成功后你应该看到：

```
正在生成 gRPC 代码...
  - controller.proto
  - connect_node.proto  
  - push_manager.proto
✅ gRPC 代码生成完成
```

## 🔧 安装其他依赖

### Redis

```bash
# macOS
brew install redis
brew services start redis

# 或使用 Docker
docker run -d --name redis -p 6379:6379 redis:latest
```

### ETCD（可选，用于服务发现）

```bash
# macOS  
brew install etcd
brew services start etcd

# 或使用 Docker
docker run -d --name etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

## 🚀 启动 Controller

```bash
# 安装 Go 依赖
cd /Users/yon/repo/psrpc/examples/pubsub
go mod tidy

# 运行 Controller
cd controller-manager
go run . controller-1 50051
```

## 🧪 测试工具

### grpcurl

```bash
# 安装
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 测试
grpcurl -plaintext localhost:50051 list
```

### redis-cli

```bash
# 安装
brew install redis

# 使用
redis-cli
> KEYS *
> GET room:room-001
```

## 常见问题

### Q: protoc: command not found

**A:** 你需要安装 protoc，参见上面的安装方法。

### Q: protoc-gen-go: program not found or is not executable

**A:** 你需要安装 Go 插件并确保它们在 PATH 中：

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Q: cannot find package "github.com/livekit/psrpc/examples/pubsub/proto"

**A:** 你需要先生成 proto 代码：

```bash
cd proto
./gen.sh
```

### Q: go: module requires go >= 1.23.0

**A:** 这是因为 psrpc 主项目需要 Go 1.23+，但 examples 只需要 Go 1.21+。`go.mod` 的 `replace` 指令会自动处理版本切换。

## ✅ 完整设置流程

```bash
# 1. 安装 protoc
brew install protobuf  # 或手动下载

# 2. 安装 Go 插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 3. 确保 PATH 正确
export PATH="$PATH:$(go env GOPATH)/bin"

# 4. 生成 Proto 代码
cd /Users/yon/repo/psrpc/examples/pubsub/proto
./gen.sh

# 5. 安装依赖
cd ..
go mod tidy

# 6. 启动 Redis
docker run -d -p 6379:6379 redis:latest

# 7. 运行 Controller
cd controller-manager
go run . controller-1 50051
```

搞定！🎉


