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

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 命令行参数
	mode := flag.String("mode", "both", "运行模式: ws (WebSocket客户端), grpc (gRPC客户端), both (两者都运行)")
	connectNodeAddr := flag.String("connect-node", "localhost:8083", "Connect-Node 地址 (host:port)")
	pushManagerAddr := flag.String("push-manager", "localhost:50053", "Push-Manager gRPC 地址")
	userID := flag.String("user-id", "user-001", "用户 ID")
	userName := flag.String("user-name", "测试用户", "用户名称")
	roomID := flag.String("room-id", "room-001", "房间 ID")
	message := flag.String("message", "Hello from Biz-Server!", "要广播的消息")
	flag.Parse()

	log.Printf("====================================")
	log.Printf("   PubSub 业务服务器客户端示例")
	log.Printf("====================================")
	log.Printf("")
	log.Printf("运行模式: %s", *mode)
	log.Printf("")

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	switch *mode {
	case "ws":
		runWebSocketClient(*connectNodeAddr, *userID, *userName, *roomID, sigChan)
	case "grpc":
		runGRPCClient(*pushManagerAddr, *roomID, *userID, *message)
	case "both":
		runBothClients(*connectNodeAddr, *pushManagerAddr, *userID, *userName, *roomID, *message, sigChan)
	default:
		log.Fatalf("❌ 未知模式: %s (支持: ws, grpc, both)", *mode)
	}
}

// runWebSocketClient 运行 Getty WebSocket 客户端
func runWebSocketClient(addr, userID, userName, roomID string, sigChan chan os.Signal) {
	log.Printf("🚀 启动 Getty WebSocket 客户端...")
	log.Printf("")

	// 创建 Getty WebSocket 客户端
	wsClient, err := NewGettyWebSocketClient(addr, userID, userName, roomID)
	if err != nil {
		log.Fatalf("❌ 创建 WebSocket 客户端失败: %v", err)
	}
	defer wsClient.Close()

	// 加入房间
	if err := wsClient.JoinRoom(); err != nil {
		log.Fatalf("❌ 加入房间失败: %v", err)
	}

	log.Printf("")
	log.Printf("✅ WebSocket 客户端运行中...")
	log.Printf("📝 按 Ctrl+C 退出")
	log.Printf("")

	// 等待退出信号
	<-sigChan
	log.Printf("")
	log.Printf("👋 收到退出信号，关闭客户端...")
}

// runGRPCClient 运行 gRPC 客户端
func runGRPCClient(addr, roomID, userID, message string) {
	log.Printf("🚀 启动 gRPC 客户端...")
	log.Printf("")

	// 创建 gRPC 客户端
	grpcClient, err := NewPushManagerClient(addr)
	if err != nil {
		log.Fatalf("❌ 创建 gRPC 客户端失败: %v", err)
	}
	defer grpcClient.Close()

	log.Printf("")
	log.Printf("📋 执行操作:")
	log.Printf("")

	// 1. 广播消息到房间
	log.Printf("1️⃣  广播消息到房间...")
	if err := grpcClient.BroadcastToRoom(roomID, message); err != nil {
		log.Printf("❌ 广播失败: %v", err)
	}
	log.Printf("")

	// 2. 推送消息给指定用户
	log.Printf("2️⃣  推送消息给用户...")
	if err := grpcClient.PushToUser(userID, message); err != nil {
		log.Printf("❌ 推送失败: %v", err)
	}
	log.Printf("")

	// 3. 获取系统统计
	log.Printf("3️⃣  获取系统统计...")
	if err := grpcClient.GetRoomStats(); err != nil {
		log.Printf("❌ 获取统计失败: %v", err)
	}
	log.Printf("")

	log.Printf("✅ 所有操作完成")
}

// runBothClients 同时运行两个客户端
func runBothClients(wsAddr, grpcAddr, userID, userName, roomID, message string, sigChan chan os.Signal) {
	log.Printf("🚀 启动 Getty WebSocket 和 gRPC 客户端...")
	log.Printf("")

	// 1. 创建 Getty WebSocket 客户端
	log.Printf("1️⃣  创建 Getty WebSocket 客户端...")
	wsClient, err := NewGettyWebSocketClient(wsAddr, userID, userName, roomID)
	if err != nil {
		log.Fatalf("❌ 创建 WebSocket 客户端失败: %v", err)
	}
	defer wsClient.Close()

	// 加入房间
	if err := wsClient.JoinRoom(); err != nil {
		log.Fatalf("❌ 加入房间失败: %v", err)
	}

	log.Printf("")
	log.Printf("✅ WebSocket 客户端已连接并监听消息")
	log.Printf("")

	// 等待一会儿，确保连接稳定
	time.Sleep(3 * time.Second)

	// 2. 创建 gRPC 客户端并发送广播
	log.Printf("2️⃣  创建 gRPC 客户端...")
	grpcClient, err := NewPushManagerClient(grpcAddr)
	if err != nil {
		log.Fatalf("❌ 创建 gRPC 客户端失败: %v", err)
	}
	defer grpcClient.Close()

	log.Printf("")
	log.Printf("3️⃣  通过 gRPC 广播消息...")
	if err := grpcClient.BroadcastToRoom(roomID, message); err != nil {
		log.Printf("❌ 广播失败: %v", err)
	}

	log.Printf("")
	log.Printf("✅ 消息已发送，WebSocket 客户端应该会收到推送")
	log.Printf("")
	log.Printf("📝 按 Ctrl+C 退出")
	log.Printf("")

	// 等待退出信号
	<-sigChan
	log.Printf("")
	log.Printf("👋 收到退出信号，关闭客户端...")
}
