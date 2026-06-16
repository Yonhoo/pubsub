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

package redis

import (
	"github.com/livekit/psrpc"
	"github.com/redis/go-redis/v9"
)

// NewRedisBus 创建基于 Redis 的 psrpc MessageBus
func NewRedisBus(client *redis.Client) (psrpc.MessageBus, error) {
	// 使用 psrpc 的 Redis 实现
	// 注意：这需要引用主项目的 bus 实现
	return psrpc.NewRedisMessageBus(client), nil
}
