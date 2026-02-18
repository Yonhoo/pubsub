# 配置指南

本项目支持统一的 YAML 配置文件，所有服务都使用相同的配置加载机制。

## 配置优先级

配置加载遵循以下优先级（从高到低）：

1. **环境变量** - 最高优先级，可以覆盖任何配置
2. **YAML 配置文件** - 从 `config.yaml` 读取
3. **默认值** - 代码中定义的默认值

## 配置文件位置

每个服务在其自己的目录下查找 `config.yaml`：

```
pubsub/
├── controller-manager/
│   └── config.yaml          # Controller-Manager 配置
├── connect-node/
│   └── config.yaml          # Connect-Node 配置
├── push-manager/
│   └── config.yaml          # Push-Manager 配置
└── config.yaml              # 根配置（通用）
```

## 配置文件语法

### 基本语法

```yaml
# 注释以 # 开头
server:
  id: controller-1
  port: 50051
  addr: 0.0.0.0:50051

database:
  host: localhost
  port: 3306
```

### 环境变量替换

配置文件支持环境变量替换，语法为 `${VAR_NAME:default_value}`：

```yaml
database:
  host: ${DB_HOST:localhost}        # 使用环境变量 DB_HOST，如果未设置则使用 localhost
  port: ${DB_PORT:3306}             # 使用环境变量 DB_PORT，如果未设置则使用 3306
  password: ${DB_PASSWORD:}         # 使用环境变量 DB_PASSWORD，如果未设置则为空字符串
```

### 数组配置

```yaml
etcd:
  endpoints:
    - localhost:2379
    - localhost:2380
    - localhost:2381
```

或使用环境变量（逗号分隔）：

```bash
export ETCD_ENDPOINTS=localhost:2379,localhost:2380,localhost:2381
```

## 配置项说明

### 服务器配置

```yaml
server:
  id: controller-1              # 服务器唯一标识
  port: 50051                   # gRPC 监听端口
  addr: 0.0.0.0:50051          # 完整监听地址
```

环境变量：
- `SERVER_ID`
- `SERVER_PORT`
- `SERVER_ADDR`

### 数据库配置

```yaml
database:
  host: localhost               # MySQL 主机
  port: 3306                    # MySQL 端口
  user: pubsub                  # 数据库用户
  password: pubsub123           # 数据库密码
  database: pubsub              # 数据库名称
  charset: utf8mb4              # 字符集
  max_open_conns: 100           # 最大打开连接数
  max_idle_conns: 20            # 最大空闲连接数
  conn_max_lifetime: 3600s      # 连接最大生命周期
```

环境变量：
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `DB_CHARSET`

### Redis 配置

```yaml
redis:
  addr: localhost:6379          # Redis 地址
  password: ""                  # Redis 密码
  db: 0                         # 数据库编号
  pool_size: 10                 # 连接池大小
  min_idle_conns: 5             # 最小空闲连接数
```

环境变量：
- `REDIS_ADDR`
- `REDIS_PASSWORD`
- `REDIS_DB`

### ETCD 配置

```yaml
etcd:
  endpoints:                    # ETCD 端点列表
    - localhost:2379
  dial_timeout: 5s              # 连接超时
  request_timeout: 10s          # 请求超时
```

环境变量：
- `ETCD_ENDPOINTS` (逗号分隔)

### RPC 配置

```yaml
rpc:
  timeout: 10s                  # RPC 超时时间
  max_retries: 3                # 最大重试次数
  retry_interval: 1s            # 重试间隔
```

环境变量：
- `RPC_TIMEOUT_SECONDS` (支持数字或 duration 格式)

### Getty 配置

```yaml
getty:
  app_name: pubsub-server       # 应用名称
  host: 0.0.0.0                 # 监听地址
  ports:                        # 监听端口列表
    - "8080"
  paths:                        # 路径列表
    - "/connect"
  heartbeat_period: 60s         # 心跳周期
  session_timeout: 60s          # 会话超时
  
  session_param:
    tcp_no_delay: true          # TCP_NODELAY
    tcp_keep_alive: true        # TCP_KEEPALIVE
    tcp_read_buf_size: 262144   # TCP 读缓冲区
    tcp_write_buf_size: 65536   # TCP 写缓冲区
    max_msg_len: 1024000        # 最大消息长度
```

环境变量：
- `GETTY_APP_NAME`
- `GETTY_HOST`
- `GETTY_PORT`
- `GETTY_PATH`
- `GETTY_HEARTBEAT_PERIOD`
- `GETTY_SESSION_TIMEOUT`

## 使用示例

### 示例 1：纯 YAML 配置

创建 `config.yaml`：

```yaml
server:
  id: my-controller
  port: 50051

database:
  host: 192.168.1.100
  port: 3306
  user: pubsub
  password: secret123
  database: pubsub

redis:
  addr: 192.168.1.101:6379

etcd:
  endpoints:
    - 192.168.1.102:2379
```

运行：

```bash
cd controller-manager
go run main.go
```

### 示例 2：YAML + 环境变量

创建 `config.yaml`：

```yaml
database:
  host: ${DB_HOST:localhost}
  port: ${DB_PORT:3306}
  user: pubsub
  password: ${DB_PASSWORD}      # 必须从环境变量读取
```

运行：

```bash
export DB_HOST=192.168.1.100
export DB_PASSWORD=secret123
cd controller-manager
go run main.go
```

### 示例 3：环境变量覆盖 YAML

即使 `config.yaml` 中有配置，环境变量也会覆盖：

```yaml
# config.yaml
database:
  host: localhost
  port: 3306
```

```bash
# 环境变量会覆盖 YAML 中的值
export DB_HOST=192.168.1.100
export DB_PORT=3307
go run main.go
```

实际使用的配置：
- `host`: `192.168.1.100` (来自环境变量)
- `port`: `3307` (来自环境变量)

### 示例 4：Docker Compose

在 `docker-compose.yml` 中使用环境变量：

```yaml
services:
  controller-manager:
    image: pubsub-controller:latest
    environment:
      - SERVER_ID=controller-1
      - DB_HOST=mysql
      - DB_PORT=3306
      - DB_USER=pubsub
      - DB_PASSWORD=pubsub123
      - REDIS_ADDR=redis:6379
      - ETCD_ENDPOINTS=etcd:2379
```

容器内的 `config.yaml` 可以使用这些环境变量：

```yaml
database:
  host: ${DB_HOST:localhost}
  port: ${DB_PORT:3306}
```

## 配置验证

### 检查配置是否正确加载

运行服务时，会看到日志：

```
✅ 已加载 YAML 配置文件: config.yaml
```

如果没有配置文件：

```
⚠️  无法加载 config.yaml: open config.yaml: no such file or directory，使用默认配置和环境变量
```

这是正常的，程序会使用默认值和环境变量。

### 调试配置

如果需要查看实际使用的配置值，可以在代码中添加日志：

```go
cfg := config.LoadConfig()
log.Printf("Database Host: %s", cfg.Database.Host)
log.Printf("Database Port: %d", cfg.Database.Port)
log.Printf("Redis Addr: %s", cfg.Redis.Addr)
```

## 最佳实践

### 1. 敏感信息使用环境变量

不要在 YAML 文件中硬编码密码等敏感信息：

```yaml
# ❌ 不好
database:
  password: my_secret_password

# ✅ 好
database:
  password: ${DB_PASSWORD}
```

### 2. 提供合理的默认值

在 YAML 中提供开发环境的默认值：

```yaml
database:
  host: ${DB_HOST:localhost}        # 开发环境默认 localhost
  port: ${DB_PORT:3306}
  user: ${DB_USER:pubsub}
  password: ${DB_PASSWORD:pubsub123}  # 开发环境默认密码
```

### 3. 使用不同的配置文件

为不同环境创建不同的配置文件：

```
config.yaml              # 开发环境
config.prod.yaml         # 生产环境
config.test.yaml         # 测试环境
```

运行时指定配置文件（需要修改代码支持）：

```bash
CONFIG_FILE=config.prod.yaml go run main.go
```

### 4. 文档化配置项

在配置文件中添加注释说明每个配置项的作用：

```yaml
database:
  # MySQL 主机地址，生产环境应使用内网地址
  host: ${DB_HOST:localhost}
  
  # 连接池大小，根据实际负载调整
  max_open_conns: 100
```

### 5. 版本控制

- ✅ 提交 `config.yaml.example` 到 Git
- ❌ 不要提交包含敏感信息的 `config.yaml`

```bash
# .gitignore
config.yaml
config.*.yaml
!config.yaml.example
```

## 故障排查

### 问题 1：配置未生效

**症状**：修改了 `config.yaml`，但程序仍使用旧值

**可能原因**：
1. 环境变量覆盖了 YAML 配置
2. 配置文件路径不正确
3. YAML 语法错误

**解决方案**：
```bash
# 1. 检查环境变量
env | grep DB_

# 2. 检查配置文件位置
ls -la config.yaml

# 3. 验证 YAML 语法
cat config.yaml | python3 -c 'import yaml, sys; yaml.safe_load(sys.stdin)'
```

### 问题 2：环境变量替换失败

**症状**：配置值显示为 `${VAR_NAME:default}`

**可能原因**：YAML 解析器将其作为普通字符串

**解决方案**：确保不要在值两边加引号：

```yaml
# ❌ 错误
database:
  host: "${DB_HOST:localhost}"

# ✅ 正确
database:
  host: ${DB_HOST:localhost}
```

### 问题 3：数组配置不生效

**症状**：ETCD endpoints 只使用了第一个

**解决方案**：检查 YAML 数组语法：

```yaml
# ✅ 正确
etcd:
  endpoints:
    - localhost:2379
    - localhost:2380

# 或使用环境变量（逗号分隔）
export ETCD_ENDPOINTS=localhost:2379,localhost:2380
```

## 相关文档

- [Controller-Manager 配置](controller-manager/README.md)
- [Connect-Node 配置](connect-node/README.md)
- [Push-Manager 配置](push-manager/README.md)
- [配置源码](pkg/config/config.go)






