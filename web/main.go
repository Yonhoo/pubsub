package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type BroadcastRequest struct {
	RoomID  string `json:"room_id"`
	Message string `json:"message"`
}

type BroadcastResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Desc string `json:"desc"`
}

func main() {
	port := getEnv("WEB_PORT", "8086")
	pushManagerAddr := getEnv("PUSH_MANAGER_ADDR", "localhost:50053")

	log.Printf("🌐 Web 服务器启动中...")
	log.Printf("   端口: %s", port)
	log.Printf("   Push-Manager: %s", pushManagerAddr)
	log.Printf("")

	// 连接 Push-Manager gRPC
	log.Printf("🔗 连接 Push-Manager...")
	conn, err := grpc.Dial(pushManagerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		log.Printf("⚠️  连接 Push-Manager 失败: %v", err)
		log.Printf("⚠️  /broadcast API 将不可用")
		conn = nil
	} else {
		defer conn.Close()
		log.Printf("✅ Push-Manager 客户端已连接")
	}

	var pushClient broadcast.PushServerClient
	if conn != nil {
		pushClient = broadcast.NewPushServerClient(conn)
	}

	log.Printf("")

	// HTTP 路由
	mux := http.NewServeMux()

	// API: 广播消息
	mux.HandleFunc("/broadcast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if pushClient == nil {
			log.Printf("❌ Push-Manager 未连接")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(BroadcastResponse{
				Code: "503",
				Msg:  "Service Unavailable",
				Desc: "Push-Manager 未连接",
			})
			return
		}

		var req BroadcastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("❌ 解析请求失败: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(BroadcastResponse{
				Code: "400",
				Msg:  "Bad Request",
				Desc: err.Error(),
			})
			return
		}

		log.Printf("📡 收到广播请求: room=%s, message=%s", req.RoomID, req.Message)

		// 调用 Push-Manager 广播
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		protoMsg := &proto.Proto{
			Ver:    1,
			Op:     2, // OP_SEND_MSG
			Seq:    1,
			Roomid: req.RoomID,
			Body:   []byte(req.Message),
		}

		_, err := pushClient.Broadcast(ctx, &broadcast.BroadCastReq{Proto: protoMsg})
		if err != nil {
			log.Printf("❌ 广播失败: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(BroadcastResponse{
				Code: "500",
				Msg:  "Internal Server Error",
				Desc: err.Error(),
			})
			return
		}

		log.Printf("✅ 广播成功")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BroadcastResponse{
			Code: "0",
			Msg:  "OK",
			Desc: "消息广播成功",
		})
	})

	// API: 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"service": "web-server",
		})
	})

	// 静态文件服务器
	fs := http.FileServer(http.Dir("./"))
	mux.Handle("/", fs)

	// 设置 CORS 头
	corsHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			h.ServeHTTP(w, r)
		})
	}
	
	log.Printf("🌐 Web 服务器启动: http://localhost:%s", port)
	log.Printf("")
	log.Printf("📝 功能:")
	log.Printf("   - 聊天页面: http://localhost:%s/chat.html", port)
	log.Printf("   - 广播 API: POST http://localhost:%s/broadcast", port)
	log.Printf("   - 健康检查: GET http://localhost:%s/health", port)
	log.Printf("")
	log.Printf("💡 使用说明:")
	log.Printf("   1. 在不同的浏览器窗口打开聊天页面")
	log.Printf("   2. 使用不同的用户 ID 和昵称登录")
	log.Printf("   3. 加入相同的房间（例如：room-001）")
	log.Printf("   4. 开始聊天！")
	log.Printf("")

	if err := http.ListenAndServe(":"+port, corsHandler(mux)); err != nil {
		log.Fatalf("❌ 服务器启动失败: %v", err)
	}
}

// getEnv 获取环境变量（带默认值）
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}


