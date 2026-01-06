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

// import (
// 	"context"
// 	"flag"
// 	"log"
// 	"time"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"

// 	pb "github.com/livekit/psrpc/examples/pubsub/proto"
// )

// func main() {
// 	// 命令行参数
// 	pushManagerAddr := flag.String("push-manager", "localhost:50053", "Push-Manager 地址")
// 	action := flag.String("action", "push-to-room", "操作: push-to-room, push-to-user, broadcast")
// 	roomID := flag.String("room", "room-001", "房间 ID")
// 	userID := flag.String("user", "user-001", "用户 ID")
// 	message := flag.String("message", "Hello from Biz-Server!", "消息内容")
// 	msgType := flag.String("type", "TEXT", "消息类型: TEXT, AUDIO, VIDEO, TRANSLATION, SYSTEM")
// 	flag.Parse()

// 	// 连接 Push-Manager
// 	log.Printf("连接 Push-Manager: %s\n", *pushManagerAddr)
// 	conn, err := grpc.Dial(*pushManagerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		log.Fatalf("❌ 连接失败: %v\n", err)
// 	}
// 	defer conn.Close()

// 	client := pb.NewPushManagerServiceClient(conn)
// 	log.Printf("✅ 连接成功\n")

// 	// 根据操作类型执行不同的推送
// 	switch *action {
// 	case "push-to-room":
// 		pushToRoom(client, *roomID, *message, *msgType)
// 	case "push-to-user":
// 		pushToUser(client, *userID, *message, *msgType)
// 	case "broadcast":
// 		broadcastMessage(client, *message, *msgType)
// 	default:
// 		log.Printf("未知操作: %s\n", *action)
// 	}
// }

// // pushToRoom 推送消息到房间
// func pushToRoom(client pb.PushManagerServiceClient, roomID, message, msgType string) {
// 	log.Printf("📤 推送消息到房间: %s\n", roomID)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	resp, err := client.PushToRoom(ctx, &pb.PushToRoomRequest{
// 		RoomId: roomID,
// 		Content: &pb.MessageContent{
// 			Type:      parseMessageType(msgType),
// 			Data:      []byte(message),
// 			Timestamp: time.Now().Unix(),
// 			Metadata: map[string]string{
// 				"source": "biz-server",
// 			},
// 		},
// 	})

// 	if err != nil {
// 		log.Fatalf("❌ 推送失败: %v\n", err)
// 	}

// 	if !resp.Success {
// 		log.Printf("❌ 推送失败: %s\n", resp.Message)
// 		return
// 	}

// 	log.Printf("✅ 推送成功: %d 人收到消息\n", resp.DeliveredCount)
// }

// // pushToUser 推送消息给指定用户
// func pushToUser(client pb.PushManagerServiceClient, userID, message, msgType string) {
// 	log.Printf("📤 推送消息给用户: %s\n", userID)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	resp, err := client.PushToUser(ctx, &pb.PushToUserRequest{
// 		UserId: userID,
// 		Content: &pb.MessageContent{
// 			Type:      parseMessageType(msgType),
// 			Data:      []byte(message),
// 			Timestamp: time.Now().Unix(),
// 			Metadata: map[string]string{
// 				"source": "biz-server",
// 			},
// 		},
// 	})

// 	if err != nil {
// 		log.Fatalf("❌ 推送失败: %v\n", err)
// 	}

// 	if !resp.Success {
// 		log.Printf("❌ 推送失败: %s\n", resp.Message)
// 		return
// 	}

// 	log.Printf("✅ 推送成功\n")
// }

// // broadcastMessage 广播消息
// func broadcastMessage(client pb.PushManagerServiceClient, message, msgType string) {
// 	log.Printf("📢 广播消息\n")

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	resp, err := client.BroadcastMessage(ctx, &pb.BroadcastMessageRequest{
// 		Content: &pb.MessageContent{
// 			Type:      parseMessageType(msgType),
// 			Data:      []byte(message),
// 			Timestamp: time.Now().Unix(),
// 			Metadata: map[string]string{
// 				"source": "biz-server",
// 			},
// 		},
// 	})

// 	if err != nil {
// 		log.Fatalf("❌ 广播失败: %v\n", err)
// 	}

// 	if !resp.Success {
// 		log.Printf("❌ 广播失败\n")
// 		return
// 	}

// 	log.Printf("✅ 广播成功: %d 人收到消息\n", resp.TotalDelivered)
// }

// // parseMessageType 解析消息类型
// func parseMessageType(msgType string) pb.MessageType {
// 	switch msgType {
// 	case "TEXT":
// 		return pb.MessageType_TEXT
// 	case "AUDIO":
// 		return pb.MessageType_AUDIO
// 	case "VIDEO":
// 		return pb.MessageType_VIDEO
// 	case "TRANSLATION":
// 		return pb.MessageType_TRANSLATION
// 	case "SYSTEM":
// 		return pb.MessageType_SYSTEM
// 	default:
// 		return pb.MessageType_TEXT
// 	}
// }

