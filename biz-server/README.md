# Biz-Server 业务服务示例

## 概述

Biz-Server 是业务逻辑处理服务，负责处理音频、翻译等业务，并通过 Push-Manager 推送消息给客户端。

## 功能说明

这是一个示例 Biz-Server，展示如何调用 Push-Manager 推送消息。

实际的 Biz-Server 可能包括：
- 音频处理服务
- 翻译服务
- AI 服务
- 其他业务逻辑

## 使用示例

### 推送音频消息到房间

```go
package main

import (
    "context"
    "log"
    "google.golang.org/grpc"
    pb "github.com/livekit/psrpc/examples/pubsub/proto"
)

func main() {
    // 连接 Push-Manager
    conn, err := grpc.Dial("localhost:50053", grpc.WithInsecure())
    if err != nil {
        log.Fatalf("连接失败: %v", err)
    }
    defer conn.Close()

    client := pb.NewPushManagerServiceClient(conn)

    // 推送音频消息到房间
    resp, err := client.PushToRoom(context.Background(), &pb.PushToRoomRequest{
        RoomId: "room-001",
        Content: &pb.MessageContent{
            Type:      pb.MessageType_AUDIO,
            Data:      []byte("audio data..."),
            Timestamp: time.Now().Unix(),
            Metadata: map[string]string{
                "format": "opus",
                "duration": "3000",
            },
        },
    })

    if err != nil {
        log.Fatalf("推送失败: %v", err)
    }

    log.Printf("推送成功: delivered=%d\n", resp.DeliveredCount)
}
```

### 推送翻译消息给指定用户

```go
func pushTranslation(client pb.PushManagerServiceClient, userID string, text string) {
    resp, err := client.PushToUser(context.Background(), &pb.PushToUserRequest{
        UserId: userID,
        Content: &pb.MessageContent{
            Type:      pb.MessageType_TRANSLATION,
            Data:      []byte(text),
            Timestamp: time.Now().Unix(),
            Metadata: map[string]string{
                "language": "zh-CN",
                "source": "en-US",
            },
        },
    })

    if err != nil {
        log.Printf("推送失败: %v", err)
        return
    }

    log.Printf("推送成功: %v\n", resp.Success)
}
```

### 系统广播消息

```go
func broadcastSystemMessage(client pb.PushManagerServiceClient, message string) {
    resp, err := client.BroadcastMessage(context.Background(), &pb.BroadcastMessageRequest{
        Content: &pb.MessageContent{
            Type:      pb.MessageType_SYSTEM,
            Data:      []byte(message),
            Timestamp: time.Now().Unix(),
            Metadata: map[string]string{
                "priority": "high",
            },
        },
    })

    if err != nil {
        log.Printf("广播失败: %v", err)
        return
    }

    log.Printf("广播成功: delivered=%d\n", resp.TotalDelivered)
}
```

## 消息类型

支持的消息类型：

| 类型 | 说明 | 示例用途 |
|------|------|----------|
| TEXT | 文本消息 | 聊天、通知 |
| AUDIO | 音频数据 | 实时语音 |
| VIDEO | 视频数据 | 实时视频 |
| TRANSLATION | 翻译文本 | 实时翻译 |
| SYSTEM | 系统消息 | 公告、提示 |

## 最佳实践

### 1. 错误处理
```go
resp, err := client.PushToRoom(ctx, req)
if err != nil {
    // gRPC 调用失败，Push-Manager 不可用
    log.Printf("调用失败: %v", err)
    // 重试或降级处理
    return
}

if !resp.Success {
    // 推送失败，房间不存在或用户不在线
    log.Printf("推送失败: %s", resp.Message)
    // 记录失败日志，可选择重试
    return
}

// 推送成功
log.Printf("推送成功: %d 人收到", resp.DeliveredCount)
```

### 2. 超时控制
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

resp, err := client.PushToUser(ctx, req)
```

### 3. 重试机制
```go
func pushWithRetry(client pb.PushManagerServiceClient, req *pb.PushToUserRequest, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        resp, err := client.PushToUser(context.Background(), req)
        if err == nil && resp.Success {
            return nil
        }
        
        log.Printf("推送失败，重试 %d/%d", i+1, maxRetries)
        time.Sleep(time.Second * time.Duration(i+1))
    }
    return fmt.Errorf("推送失败，已重试 %d 次", maxRetries)
}
```

### 4. 批量推送优化
如果需要推送给多个用户，建议使用房间推送：
```go
// ❌ 不推荐：循环推送给每个用户
for _, userID := range userIDs {
    client.PushToUser(ctx, &pb.PushToUserRequest{
        UserId: userID,
        Content: content,
    })
}

// ✅ 推荐：确保用户在同一房间，使用房间推送
client.PushToRoom(ctx, &pb.PushToRoomRequest{
    RoomId: roomID,
    Content: content,
})
```

## 集成示例

完整的业务服务示例参见：
- [音频处理服务示例](./examples/audio_service.go)
- [翻译服务示例](./examples/translation_service.go)

## 相关文档

- [Push-Manager API](../push-manager/README.md)
- [消息协议定义](../proto/push_manager.proto)
- [系统架构](../pub-msg.jpg)


