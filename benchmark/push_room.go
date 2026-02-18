// bench_push_room.go
package main

// 用法示例：
// go run push_room.go -room=room-001 -dur=15m -addr=localhost:8086 -msg='{"test":1}' -unlimited
//
// -room       房间 ID
// -rate       每秒广播次数（如果不设置 -unlimited）
// -unlimited  无限制速率模式（最大速率发送）
// -workers    并发工作协程数（仅在 unlimited 模式下有效，默认 10）
// -dur        持续时间
// -addr       Web-Server 地址（提供 /broadcast 的那个）
// -msg        消息内容

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

type broadcastReq struct {
	RoomID  string `json:"room_id"`
	Message string `json:"message"`
}

type broadcastResp struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Desc string `json:"desc"`
}

func main() {
	room := flag.String("room", "room-001", "room id")
	addr := flag.String("addr", "localhost:8086", "web-server addr(host:port)")
	rate := flag.Int("rate", 40, "broadcasts per second (ignored if unlimited=true)")
	unlimited := flag.Bool("unlimited", false, "unlimited rate mode (send as fast as possible)")
	workers := flag.Int("workers", 10, "number of concurrent workers in unlimited mode")
	dur := flag.Duration("dur", 15*time.Minute, "duration")
	msg := flag.String("msg", `{"test":1}`, "message payload")
	verbose := flag.Bool("verbose", false, "show detailed logs for each request")
	flag.Parse()

	log.Printf("==========================================")
	log.Printf("📡 广播消息推送工具")
	log.Printf("==========================================")
	log.Printf("房间 ID: %s", *room)
	log.Printf("Web-Server: %s", *addr)
	if *unlimited {
		log.Printf("推送模式: 🚀 无限制速率（最大速度）")
		log.Printf("并发工作数: %d", *workers)
	} else {
		log.Printf("推送模式: 📊 速率限制")
		log.Printf("推送速率: %d 条/秒", *rate)
	}
	log.Printf("持续时间: %s", dur.String())
	log.Printf("消息内容: %s", *msg)
	log.Printf("==========================================")
	log.Printf("")

	bodyStruct := broadcastReq{
		RoomID:  *room,
		Message: *msg,
	}
	bodyBytes, err := json.Marshal(bodyStruct)
	if err != nil {
		log.Fatalf("❌ JSON 序列化失败: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	var ok, fail, total int64
	startTime := time.Now()
	stop := time.After(*dur)

	// 统计协程：每 5 秒打印一次统计信息
	go func() {
		var (
			lastOk   int64
			interval = int64(5) // 每 5 秒统计一次
		)
		statTicker := time.NewTicker(time.Duration(interval) * time.Second)
		defer statTicker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-statTicker.C:
				nowOk := atomic.LoadInt64(&ok)
				nowFail := atomic.LoadInt64(&fail)

				// 计算增量（用于最近速率）
				diffOk := nowOk - lastOk
				lastOk = nowOk

				// 计算每秒速率（基于最近 5 秒的增量）
				recentRate := float64(diffOk) / float64(interval)

				elapsed := time.Since(startTime)
				avgRate := float64(nowOk) / elapsed.Seconds()

				log.Printf("[stat] 运行时间:%s | 成功:%d | 失败:%d | 最近速率:%.1f条/s | 平均速率:%.1f条/s",
					elapsed.Round(time.Second), nowOk, nowFail, recentRate, avgRate)
			}
		}
	}()

	log.Printf("🚀 开始推送消息...")
	log.Printf("")

	if *unlimited {
		// 无限制模式：使用多个 worker 协程持续发送
		runUnlimitedMode(client, *addr, bodyBytes, *workers, stop, &ok, &fail, &total, *verbose, *room)
	} else {
		// 速率限制模式：使用 ticker 控制发送速率
		runRateLimitedMode(client, *addr, bodyBytes, *rate, stop, &ok, &fail, &total, *verbose, *room)
	}

	// 打印最终统计
	printFinalStats(startTime, &ok, &fail, &total)
}

// runUnlimitedMode 无限制速率模式
func runUnlimitedMode(client *http.Client, addr string, bodyBytes []byte, workers int, stop <-chan time.Time, ok, fail, total *int64, verbose bool, room string) {
	// 创建一个信号通道来控制 worker 的停止
	done := make(chan struct{})

	// 启动多个 worker
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			for {
				select {
				case <-done:
					return
				default:
					atomic.AddInt64(total, 1)
					sendBroadcast(client, addr, bodyBytes, ok, fail, verbose, room)
				}
			}
		}(i)
	}

	// 等待停止信号
	<-stop
	close(done)
	// 给 worker 一点时间完成当前请求
	time.Sleep(100 * time.Millisecond)
}

// runRateLimitedMode 速率限制模式（使用 ticker 控制每秒发送次数）
func runRateLimitedMode(client *http.Client, addr string, bodyBytes []byte, rateLimit int, stop <-chan time.Time, ok, fail, total *int64, verbose bool, room string) {
	if rateLimit <= 0 {
		rateLimit = 1
	}
	// 每隔 (1/rateLimit) 秒发送一条，即每秒 rateLimit 条
	interval := time.Second / time.Duration(rateLimit)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			atomic.AddInt64(total, 1)
			go sendBroadcast(client, addr, bodyBytes, ok, fail, verbose, room)
		}
	}
}

// sendBroadcast 发送广播消息
func sendBroadcast(client *http.Client, addr string, bodyBytes []byte, ok, fail *int64, verbose bool, room string) {
	resp, err := client.Post("http://"+addr+"/broadcast",
		"application/json",
		bytes.NewReader(bodyBytes))
	if err != nil {
		if verbose {
			log.Printf("❌ [推送失败] %v", err)
		}
		atomic.AddInt64(fail, 1)
		return
	}
	defer resp.Body.Close()

	// 读取响应
	body, _ := io.ReadAll(resp.Body)
	var respData broadcastResp
	json.Unmarshal(body, &respData)

	if resp.StatusCode == http.StatusOK && respData.Code == "0" {
		atomic.AddInt64(ok, 1)
		if verbose {
			log.Printf("✅ [推送成功] 房间=%s, 响应=%s", room, respData.Msg)
		}
	} else {
		atomic.AddInt64(fail, 1)
		if verbose {
			log.Printf("⚠️  [推送异常] 状态码=%d, 响应=%s", resp.StatusCode, string(body))
		}
	}
}

// printFinalStats 打印最终统计信息
func printFinalStats(startTime time.Time, ok, fail, total *int64) {
	okVal := atomic.LoadInt64(ok)
	failVal := atomic.LoadInt64(fail)
	totalVal := atomic.LoadInt64(total)
	elapsed := time.Since(startTime)
	avgRate := float64(okVal) / elapsed.Seconds()

	log.Printf("")
	log.Printf("==========================================")
	log.Printf("✅ 推送完成")
	log.Printf("==========================================")
	log.Printf("总运行时间: %s", elapsed.Round(time.Second))
	log.Printf("总请求数: %d", totalVal)
	log.Printf("成功: %d (%.1f%%)", okVal, float64(okVal)/float64(totalVal)*100)
	log.Printf("失败: %d (%.1f%%)", failVal, float64(failVal)/float64(totalVal)*100)
	log.Printf("平均速率: %.1f 条/秒", avgRate)
	log.Printf("==========================================")
}
