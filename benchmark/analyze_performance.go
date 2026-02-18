package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type StatRecord struct {
	Time  time.Time
	Sec   int
	Recv  int64
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("用法: %s <stat.log文件>", os.Args[0])
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("打开文件失败: %v", err)
	}
	defer file.Close()

	var records []StatRecord
	scanner := bufio.NewScanner(file)
	re := regexp.MustCompile(`\[stat\] sec=(\d+)\s+recv=(\d+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if len(matches) == 3 {
			sec, _ := strconv.Atoi(matches[1])
			recv, _ := strconv.ParseInt(matches[2], 10, 64)
			
			// 解析时间
			timeStr := strings.Fields(line)[0] + " " + strings.Fields(line)[1]
			t, err := time.Parse("2006/01/02 15:04:05", timeStr)
			if err != nil {
				continue
			}

			records = append(records, StatRecord{
				Time: t,
				Sec:  sec,
				Recv: recv,
			})
		}
	}

	if len(records) == 0 {
		log.Fatalf("未找到有效的统计记录")
	}

	// 分析性能
	analyzePerformance(records)
}

func analyzePerformance(records []StatRecord) {
	fmt.Println("=" + strings.Repeat("=", 80))
	fmt.Println("📊 性能分析报告")
	fmt.Println("=" + strings.Repeat("=", 80))
	fmt.Println()

	// 1. 基本统计
	totalRecv := int64(0)
	maxRecv := int64(0)
	minRecv := int64(999999999)
	nonZeroCount := 0
	zeroCount := 0

	for _, r := range records {
		totalRecv += r.Recv
		if r.Recv > maxRecv {
			maxRecv = r.Recv
		}
		if r.Recv < minRecv {
			minRecv = r.Recv
		}
		if r.Recv > 0 {
			nonZeroCount++
		} else {
			zeroCount++
		}
	}

	avgRecv := float64(totalRecv) / float64(len(records))
	avgRecvNonZero := float64(0)
	if nonZeroCount > 0 {
		nonZeroRecv := int64(0)
		for _, r := range records {
			if r.Recv > 0 {
				nonZeroRecv += r.Recv
			}
		}
		avgRecvNonZero = float64(nonZeroRecv) / float64(nonZeroCount)
	}

	fmt.Println("📈 基本统计信息")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("  总记录数: %d 条\n", len(records))
	fmt.Printf("  总接收消息数: %d 条\n", totalRecv)
	fmt.Printf("  平均每秒接收: %.2f 条/秒\n", avgRecv)
	if nonZeroCount > 0 {
		fmt.Printf("  非零记录平均: %.2f 条/秒\n", avgRecvNonZero)
	}
	fmt.Printf("  最大接收速率: %d 条/秒\n", maxRecv)
	fmt.Printf("  最小接收速率: %d 条/秒\n", minRecv)
	fmt.Printf("  有数据记录数: %d 条\n", nonZeroCount)
	fmt.Printf("  零数据记录数: %d 条\n", zeroCount)
	fmt.Println()

	// 2. 时间范围
	if len(records) > 0 {
		startTime := records[0].Time
		endTime := records[len(records)-1].Time
		duration := endTime.Sub(startTime)
		fmt.Println("⏱️  时间范围")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("  开始时间: %s\n", startTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("  结束时间: %s\n", endTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("  持续时间: %s\n", duration.Round(time.Second))
		fmt.Println()
	}

	// 3. 吞吐量分析（只统计有数据的记录）
	if nonZeroCount > 0 {
		var nonZeroRecords []StatRecord
		for _, r := range records {
			if r.Recv > 0 {
				nonZeroRecords = append(nonZeroRecords, r)
			}
		}

		if len(nonZeroRecords) > 1 {
			startTime := nonZeroRecords[0].Time
			endTime := nonZeroRecords[len(nonZeroRecords)-1].Time
			duration := endTime.Sub(startTime)
			totalInPeriod := int64(0)
			for _, r := range nonZeroRecords {
				totalInPeriod += r.Recv
			}

			throughput := float64(totalInPeriod) / duration.Seconds()

			fmt.Println("🚀 吞吐量分析（有数据期间）")
			fmt.Println(strings.Repeat("-", 80))
			fmt.Printf("  数据开始时间: %s\n", startTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("  数据结束时间: %s\n", endTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("  数据持续时间: %s\n", duration.Round(time.Second))
			fmt.Printf("  期间总接收: %d 条\n", totalInPeriod)
			fmt.Printf("  平均吞吐量: %.2f 条/秒\n", throughput)
			fmt.Println()
		}
	}

	// 4. 性能分布
	fmt.Println("📊 性能分布")
	fmt.Println(strings.Repeat("-", 80))
	
	// 计算百分位数
	if len(records) > 0 {
		recvValues := make([]int64, 0, len(records))
		for _, r := range records {
			if r.Recv > 0 {
				recvValues = append(recvValues, r.Recv)
			}
		}
		
		if len(recvValues) > 0 {
			// 简单排序（冒泡排序）
			for i := 0; i < len(recvValues)-1; i++ {
				for j := 0; j < len(recvValues)-i-1; j++ {
					if recvValues[j] > recvValues[j+1] {
						recvValues[j], recvValues[j+1] = recvValues[j+1], recvValues[j]
					}
				}
			}

			p50 := recvValues[len(recvValues)*50/100]
			p75 := recvValues[len(recvValues)*75/100]
			p90 := recvValues[len(recvValues)*90/100]
			p95 := recvValues[len(recvValues)*95/100]
			p99 := recvValues[len(recvValues)*99/100]

			fmt.Printf("  P50 (中位数): %d 条/秒\n", p50)
			fmt.Printf("  P75: %d 条/秒\n", p75)
			fmt.Printf("  P90: %d 条/秒\n", p90)
			fmt.Printf("  P95: %d 条/秒\n", p95)
			fmt.Printf("  P99: %d 条/秒\n", p99)
			fmt.Println()
		}
	}

	// 5. 稳定性分析（标准差）
	if nonZeroCount > 1 {
		sumSqDiff := float64(0)
		for _, r := range records {
			if r.Recv > 0 {
				diff := float64(r.Recv) - avgRecvNonZero
				sumSqDiff += diff * diff
			}
		}
		variance := sumSqDiff / float64(nonZeroCount)
		stdDev := variance
		for i := 0; i < 10; i++ {
			stdDev = (stdDev + variance/stdDev) / 2
		}
		coefficient := (stdDev / avgRecvNonZero) * 100

		fmt.Println("📉 稳定性分析")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("  标准差: %.2f 条/秒\n", stdDev)
		fmt.Printf("  变异系数: %.2f%%\n", coefficient)
		if coefficient < 10 {
			fmt.Printf("  稳定性: ✅ 非常稳定\n")
		} else if coefficient < 20 {
			fmt.Printf("  稳定性: ✅ 稳定\n")
		} else if coefficient < 30 {
			fmt.Printf("  稳定性: ⚠️  一般\n")
		} else {
			fmt.Printf("  稳定性: ❌ 不稳定\n")
		}
		fmt.Println()
	}

	// 6. 与推送速率对比（如果知道推送速率）
	fmt.Println("💡 性能评估")
	fmt.Println(strings.Repeat("-", 80))
	if avgRecvNonZero > 0 {
		// 假设推送速率是 40 条/秒，客户端数量是 1000（从用户提供的信息）
		pushRate := 40.0
		clientCount := 1000.0
		expectedRecvRate := pushRate * clientCount // 理论接收速率
		actualRecvRate := avgRecvNonZero
		efficiency := (actualRecvRate / expectedRecvRate) * 100
		
		fmt.Printf("  推送速率: %.1f 条/秒\n", pushRate)
		fmt.Printf("  客户端数量: %.0f 个\n", clientCount)
		fmt.Printf("  理论接收速率: %.0f 条/秒 (推送速率 × 客户端数)\n", expectedRecvRate)
		fmt.Printf("  实际接收速率: %.2f 条/秒\n", actualRecvRate)
		fmt.Printf("  接收效率: %.2f%%\n", efficiency)
		fmt.Printf("  消息丢失率: %.2f%%\n", 100-efficiency)
		
		if efficiency >= 95 {
			fmt.Printf("  评估: ✅ 优秀（接收效率 >= 95%%）\n")
		} else if efficiency >= 80 {
			fmt.Printf("  评估: ✅ 良好（接收效率 >= 80%%）\n")
		} else if efficiency >= 60 {
			fmt.Printf("  评估: ⚠️  一般（接收效率 >= 60%%）\n")
		} else {
			fmt.Printf("  评估: ❌ 较差（接收效率 < 60%%）\n")
		}
		
		// 计算每秒每个客户端平均接收的消息数
		recvPerClient := actualRecvRate / clientCount
		fmt.Printf("  每客户端平均接收: %.2f 条/秒\n", recvPerClient)
		fmt.Printf("  每客户端接收率: %.2f%% (相对于推送速率)\n", (recvPerClient/pushRate)*100)
	}
	
	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 80))
}

