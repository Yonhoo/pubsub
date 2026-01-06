// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Server      *ServerConfig
	Database    *DatabaseConfig
	Redis       *RedisConfig
	ETCD        *ETCDConfig
	Room        *RoomConfig
	Bucket      *BucketConfig
	TCPConfig   *TcpConfig
	Protocol    *Protocol
	RpcConfig   *RpcConfig
	GettyConfig *GettyConfig
}

type GettySessionParam struct {
	CompressEncoding bool `default:"false"` // Accept-Encoding: gzip, deflate, sdch
	TcpNoDelay       bool `default:"true"`
	TcpKeepAlive     bool `default:"true"`
	TcpRBufSize      int  `default:"262144"`
	TcpWBufSize      int  `default:"65536"`
	PkgRQSize        int  `default:"1024"`
	PkgWQSize        int  `default:"1024"`
	TcpReadTimeout   time.Duration
	TcpWriteTimeout  time.Duration
	WaitTimeout      time.Duration
	MaxMsgLen        int    `default:"1024"`
	SessionName      string `default:"echo-server"`
}

// Config holds supported types by the multiconfig package
type GettyConfig struct {
	// local address
	AppName     string   `default:"echo-server"`
	Host        string   `default:"127.0.0.1"`
	Ports       []string `default:["10000"]`
	Paths       []string `default:["/echo"]`
	ProfilePort int      `default:"10086"`

	// session
	HeartbeatPeriod time.Duration
	SessionTimeout  time.Duration
	SessionNumber   int `default:"1000"`

	// app
	FailFastTimeout string `default:"5s"`
	failFastTimeout time.Duration

	// session tcp parameters
	GettySessionParam GettySessionParam `required:"true"`
}

type RpcConfig struct {
	TimeOut time.Duration
}

type BucketConfig struct {
	Size          int
	Channel       int
	Room          int
	RoutineAmount uint64
	RoutineSize   int
}

type Protocol struct {
	Timer            int
	TimerSize        int
	SvrProto         int
	CliProto         int
	HandshakeTimeout time.Duration
}

type TcpConfig struct {
	Bind         []string
	Sndbuf       int
	Rcvbuf       int
	KeepAlive    bool
	Reader       int
	ReadBuf      int
	ReadBufSize  int
	Writer       int
	WriteBuf     int
	WriteBufSize int
}

// ServerConfig 服务器配置
type ServerConfig struct {
	ID   string
	Port int
	Addr string
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	Charset  string
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// ETCDConfig ETCD 配置
type ETCDConfig struct {
	Endpoints []string
}

// RoomConfig 房间配置
type RoomConfig struct {
	DefaultMaxUsers int           // 默认房间最大用户数
	CacheTTL        time.Duration // 房间缓存 TTL
}

// RawYAMLConfig 原始 YAML 配置
type RawYAMLConfig map[string]interface{}

// LoadConfig 加载配置（优先从 YAML 文件，环境变量可覆盖）
func LoadConfig() *Config {
	return LoadConfigFromFile("config.yaml")
}

// LoadConfigFromFile 从指定的 YAML 文件加载配置
func LoadConfigFromFile(filename string) *Config {
	// 加载 YAML 配置
	yamlCfg, err := loadYAMLConfig(filename)
	if err != nil {
		log.Printf("⚠️  无法加载 %s: %v，使用默认配置和环境变量", filename, err)
		yamlCfg = nil
	}

	// 构建配置（YAML 配置 + 环境变量覆盖，环境变量优先）
	return &Config{
		Server: &ServerConfig{
			ID:   getEnvOrYAMLStr(yamlCfg, "SERVER_ID", "server.id", "controller-1"),
			Port: getEnvOrYAMLInt(yamlCfg, "SERVER_PORT", "server.port", 50051),
			Addr: getEnvOrYAMLStr(yamlCfg, "SERVER_ADDR", "server.addr", "0.0.0.0:50052"),
		},
		Database: &DatabaseConfig{
			Host:     getEnvOrYAMLStr(yamlCfg, "DB_HOST", "database.host", "localhost"),
			Port:     getEnvOrYAMLInt(yamlCfg, "DB_PORT", "database.port", 3306),
			User:     getEnvOrYAMLStr(yamlCfg, "DB_USER", "database.user", "pubsub"),
			Password: getEnvOrYAMLStr(yamlCfg, "DB_PASSWORD", "database.password", "pubsub123"),
			DBName:   getEnvOrYAMLStr(yamlCfg, "DB_NAME", "database.database", "pubsub"),
			Charset:  getEnvOrYAMLStr(yamlCfg, "DB_CHARSET", "database.charset", "utf8mb4"),
		},
		Redis: &RedisConfig{
			Addr:     getEnvOrYAMLStr(yamlCfg, "REDIS_ADDR", "redis.addr", "localhost:6379"),
			Password: getEnvOrYAMLStr(yamlCfg, "REDIS_PASSWORD", "redis.password", ""),
			DB:       getEnvOrYAMLInt(yamlCfg, "REDIS_DB", "redis.db", 0),
		},
		ETCD: &ETCDConfig{
			Endpoints: getEnvOrYAMLStrSlice(yamlCfg, "ETCD_ENDPOINTS", "etcd.endpoints", []string{"localhost:2379"}),
		},
		Room: &RoomConfig{
			DefaultMaxUsers: getEnvOrYAMLInt(yamlCfg, "ROOM_MAX_USERS", "room.bucket_size", 100),
			CacheTTL:        time.Duration(getEnvOrYAMLInt(yamlCfg, "ROOM_CACHE_TTL_MINUTES", "", 10)) * time.Minute,
		},
		RpcConfig: &RpcConfig{
			TimeOut: getEnvOrYAMLDuration(yamlCfg, "RPC_TIMEOUT_SECONDS", "rpc.timeout", 10*time.Second),
		},
		Bucket: &BucketConfig{
			Size:          getEnvOrYAMLInt(yamlCfg, "BUCKET_SIZE", "bucket.size", 32),
			Channel:       getEnvOrYAMLInt(yamlCfg, "BUCKET_CHANNEL", "bucket.channel", 1024),
			Room:          getEnvOrYAMLInt(yamlCfg, "BUCKET_ROOM", "bucket.room", 1024),
			RoutineAmount: uint64(getEnvOrYAMLInt(yamlCfg, "BUCKET_ROUTINE_AMOUNT", "", 32)),
			RoutineSize:   getEnvOrYAMLInt(yamlCfg, "BUCKET_ROUTINE_SIZE", "", 1024),
		},
		TCPConfig: &TcpConfig{
			Bind:         []string{getEnvOrYAMLStr(yamlCfg, "TCP_BIND", "", "0.0.0.0:50052")},
			Sndbuf:       getEnvOrYAMLInt(yamlCfg, "TCP_SNDBUF", "", 65536),
			Rcvbuf:       getEnvOrYAMLInt(yamlCfg, "TCP_RCVBUF", "", 262144),
			KeepAlive:    true,
			Reader:       getEnvOrYAMLInt(yamlCfg, "TCP_READER", "", 32),
			ReadBuf:      getEnvOrYAMLInt(yamlCfg, "TCP_READBUF", "", 1024),
			ReadBufSize:  getEnvOrYAMLInt(yamlCfg, "TCP_READBUF_SIZE", "", 8192),
			Writer:       getEnvOrYAMLInt(yamlCfg, "TCP_WRITER", "", 32),
			WriteBuf:     getEnvOrYAMLInt(yamlCfg, "TCP_WRITEBUF", "", 1024),
			WriteBufSize: getEnvOrYAMLInt(yamlCfg, "TCP_WRITEBUF_SIZE", "", 8192),
		},
		Protocol: &Protocol{
			Timer:            getEnvOrYAMLInt(yamlCfg, "PROTOCOL_TIMER", "", 32),
			TimerSize:        getEnvOrYAMLInt(yamlCfg, "PROTOCOL_TIMER_SIZE", "", 2048),
			SvrProto:         getEnvOrYAMLInt(yamlCfg, "PROTOCOL_SVR_PROTO", "", 10),
			CliProto:         getEnvOrYAMLInt(yamlCfg, "PROTOCOL_CLI_PROTO", "", 5),
			HandshakeTimeout: getEnvOrYAMLDuration(yamlCfg, "PROTOCOL_HANDSHAKE_TIMEOUT_SECONDS", "", 5*time.Second),
		},
		GettyConfig: &GettyConfig{
			AppName:         getEnvOrYAMLStr(yamlCfg, "GETTY_APP_NAME", "", "pubsub-server"),
			Host:            getEnvOrYAMLStr(yamlCfg, "GETTY_HOST", "", "0.0.0.0"),
			Ports:           []string{getEnvOrYAMLStr(yamlCfg, "GETTY_PORT", "", "8083")},
			Paths:           []string{getEnvOrYAMLStr(yamlCfg, "GETTY_PATH", "", "/connect")},
			ProfilePort:     getEnvOrYAMLInt(yamlCfg, "GETTY_PROFILE_PORT", "", 10086),
			HeartbeatPeriod: getEnvOrYAMLDuration(yamlCfg, "GETTY_HEARTBEAT_PERIOD_SECONDS", "", 60*time.Second),
			SessionTimeout:  getEnvOrYAMLDuration(yamlCfg, "GETTY_SESSION_TIMEOUT_SECONDS", "", 60*time.Second),
			SessionNumber:   getEnvOrYAMLInt(yamlCfg, "GETTY_SESSION_NUMBER", "", 1000),
			FailFastTimeout: getEnvOrYAMLStr(yamlCfg, "GETTY_FAIL_FAST_TIMEOUT", "", "5s"),
			GettySessionParam: GettySessionParam{
				CompressEncoding: getEnvOrYAMLBool(yamlCfg, "GETTY_COMPRESS_ENCODING", "getty.session_param.compress_encoding", false),
				TcpNoDelay:       getEnvOrYAMLBool(yamlCfg, "GETTY_TCP_NO_DELAY", "getty.session_param.tcp_no_delay", true),
				TcpKeepAlive:     getEnvOrYAMLBool(yamlCfg, "GETTY_TCP_KEEP_ALIVE", "getty.session_param.tcp_keep_alive", true),
				TcpRBufSize:      getEnvOrYAMLInt(yamlCfg, "GETTY_TCP_RBUF_SIZE", "getty.session_param.tcp_read_buf_size", 262144),
				TcpWBufSize:      getEnvOrYAMLInt(yamlCfg, "GETTY_TCP_WBUF_SIZE", "getty.session_param.tcp_write_buf_size", 65536),
				PkgRQSize:        getEnvOrYAMLInt(yamlCfg, "GETTY_PKG_RQ_SIZE", "getty.session_param.pkg_rq_size", 1024),
				PkgWQSize:        getEnvOrYAMLInt(yamlCfg, "GETTY_PKG_WQ_SIZE", "getty.session_param.pkg_wq_size", 1024),
				TcpReadTimeout:   getEnvOrYAMLDuration(yamlCfg, "GETTY_TCP_READ_TIMEOUT", "getty.session_param.tcp_read_timeout", 60*time.Second),
				TcpWriteTimeout:  getEnvOrYAMLDuration(yamlCfg, "GETTY_TCP_WRITE_TIMEOUT", "getty.session_param.tcp_write_timeout", 60*time.Second),
				WaitTimeout:      getEnvOrYAMLDuration(yamlCfg, "GETTY_WAIT_TIMEOUT", "getty.session_param.wait_timeout", 60*time.Second),
				MaxMsgLen:        getEnvOrYAMLInt(yamlCfg, "GETTY_MAX_MSG_LEN", "getty.session_param.max_msg_len", 1024000),
				SessionName:      getEnvOrYAMLStr(yamlCfg, "GETTY_SESSION_NAME", "getty.session_param.session_name", "pubsub-session"),
			},
		},
	}
}

// loadYAMLConfig 加载 YAML 配置文件
func loadYAMLConfig(filename string) (RawYAMLConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 替换环境变量 ${VAR:default}
	content := expandEnvVars(string(data))

	var config RawYAMLConfig
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}

	return config, nil
}

// expandEnvVars 展开环境变量 ${VAR:default}
func expandEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([^:}]+)(?::([^}]*))?\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultValue := ""
		if len(parts) > 2 {
			defaultValue = parts[2]
		}

		if value := os.Getenv(varName); value != "" {
			return value
		}
		return defaultValue
	})
}

// getYAMLValue 从嵌套的 map 中获取值（支持路径如 "server.addr"）
func getYAMLValue(config RawYAMLConfig, path string) interface{} {
	if config == nil || path == "" {
		return nil
	}

	parts := strings.Split(path, ".")
	var current interface{} = config

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
		if current == nil {
			return nil
		}
	}

	return current
}

// 辅助函数：优先使用环境变量，其次 YAML，最后默认值
func getEnvOrYAMLStr(yamlCfg RawYAMLConfig, envKey, yamlPath, defaultValue string) string {
	// 1. 优先使用环境变量
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	// 2. 使用 YAML 值
	if yamlPath != "" {
		if val := getYAMLValue(yamlCfg, yamlPath); val != nil {
			if str, ok := val.(string); ok {
				return str
			}
		}
	}
	// 3. 使用默认值
	return defaultValue
}

func getEnvOrYAMLInt(yamlCfg RawYAMLConfig, envKey, yamlPath string, defaultValue int) int {
	// 1. 优先使用环境变量
	if val := os.Getenv(envKey); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	// 2. 使用 YAML 值
	if yamlPath != "" {
		if val := getYAMLValue(yamlCfg, yamlPath); val != nil {
			switch v := val.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			}
		}
	}
	// 3. 使用默认值
	return defaultValue
}

func getEnvOrYAMLBool(yamlCfg RawYAMLConfig, envKey, yamlPath string, defaultValue bool) bool {
	// 1. 优先使用环境变量
	if val := os.Getenv(envKey); val != "" {
		return val == "true" || val == "1" || val == "yes"
	}
	// 2. 使用 YAML 值
	if yamlPath != "" {
		if val := getYAMLValue(yamlCfg, yamlPath); val != nil {
			if b, ok := val.(bool); ok {
				return b
			}
		}
	}
	// 3. 使用默认值
	return defaultValue
}

func getEnvOrYAMLDuration(yamlCfg RawYAMLConfig, envKey, yamlPath string, defaultValue time.Duration) time.Duration {
	// 1. 优先使用环境变量
	if val := os.Getenv(envKey); val != "" {
		// 如果是纯数字，当作秒处理
		if i, err := strconv.Atoi(val); err == nil {
			return time.Duration(i) * time.Second
		}
		// 否则尝试解析为 duration
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	// 2. 使用 YAML 值
	if yamlPath != "" {
		if val := getYAMLValue(yamlCfg, yamlPath); val != nil {
			if str, ok := val.(string); ok {
				if d, err := time.ParseDuration(str); err == nil {
					return d
				}
			}
		}
	}
	// 3. 使用默认值
	return defaultValue
}

func getEnvOrYAMLStrSlice(yamlCfg RawYAMLConfig, envKey, yamlPath string, defaultValue []string) []string {
	// 1. 优先使用环境变量
	if val := os.Getenv(envKey); val != "" {
		// 支持逗号分隔的多个值
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	// 2. 使用 YAML 值
	if yamlPath != "" {
		if val := getYAMLValue(yamlCfg, yamlPath); val != nil {
			if slice, ok := val.([]interface{}); ok {
				result := make([]string, 0, len(slice))
				for _, item := range slice {
					if str, ok := item.(string); ok {
						result = append(result, str)
					}
				}
				if len(result) > 0 {
					return result
				}
			}
		}
	}
	// 3. 使用默认值
	return defaultValue
}

// getEnv 获取环境变量（带默认值）- 保留向后兼容
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量作为整数 - 保留向后兼容
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
