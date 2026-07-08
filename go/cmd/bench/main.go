package main

import (
	"fmt"
	"math/rand"
	"cleaner"
	"runtime"
	"time"
)

func main() {
	config := cleaner.DefaultConfig()
	c := cleaner.New(config)

	// 生成测试数据
	rng := rand.New(rand.NewSource(42))
	
	// 模拟真实轨迹: 400台设备, 每台15000个点
	totalPoints := 400 * 15000 // 600万点
	
	// 生成轨迹数据
	points := make([]cleaner.GPSPoint, totalPoints)
	for i := 0; i < totalPoints; i++ {
		deviceID := fmt.Sprintf("device_%04d", i%400)
		baseLat := 47.6062 + rng.Float64()*0.01
		baseLng := -122.3321 + rng.Float64()*0.01
		// 每秒一个点
		ts := int64(i/400) * 1000
		
		points[i] = cleaner.GPSPoint{
			DeviceID:  deviceID,
			Latitude:  baseLat + rng.Float64()*0.0001,
			Longitude: baseLng + rng.Float64()*0.0001,
			Timestamp: ts,
			Accuracy:  5.0 + rng.Float64()*10,
			Speed:     5.0 + rng.Float64()*10,
			Heading:   rng.Float64() * 360,
		}
	}

	// 内存统计前
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// 运行清洗
	start := time.Now()
	
	// 为每台设备创建独立的 cleaner
	cleaners := make(map[string]*cleaner.Cleaner)
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("device_%04d", i)
		cleaners[id] = cleaner.New(config)
	}

	processed := 0
	dropped := 0
	for _, pt := range points {
		cl := cleaners[pt.DeviceID]
		result := cl.Clean(pt)
		processed++
		if result.IsDropped() {
			dropped++
		}
	}
	
	elapsed := time.Since(start)

	// 内存统计后
	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	fmt.Println("===== 清洗程序性能基准 =====")
	fmt.Printf("总点数: %d\n", totalPoints)
	fmt.Printf("设备数: 400\n")
	fmt.Printf("处理耗时: %v\n", elapsed)
	fmt.Printf("吞吐量: %.0f 点/秒\n", float64(totalPoints)/elapsed.Seconds())
	fmt.Printf("每点耗时: %.3f μs\n", float64(elapsed.Microseconds())/float64(totalPoints))
	fmt.Printf("丢弃点数: %d (%.1f%%)\n", dropped, float64(dropped)/float64(totalPoints)*100)
	fmt.Println()
	fmt.Println("===== 内存使用 =====")
	fmt.Printf("Heap 分配前: %.2f MB\n", float64(mBefore.HeapAlloc)/1024/1024)
	fmt.Printf("Heap 分配后: %.2f MB\n", float64(mAfter.HeapAlloc)/1024/1024)
	fmt.Printf("Heap 增量: %.2f MB\n", float64(mAfter.HeapAlloc-mBefore.HeapAlloc)/1024/1024)
	fmt.Printf("总分配量: %.2f MB\n", float64(mAfter.TotalAlloc-mBefore.TotalAlloc)/1024/1024)
	fmt.Printf("GC 次数: %d\n", mAfter.NumGC-mBefore.NumGC)
	fmt.Printf("Sys 内存: %.2f MB\n", float64(mAfter.Sys)/1024/1024)
	fmt.Println()
	
	// 单设备 cleaner 内存估算
	oneCleaner := cleaner.New(config)
	oneCleanerSize := sizeofCleaner(oneCleaner)
	fmt.Println("===== 单设备 Cleaner 内存 =====")
	fmt.Printf("Cleaner 结构体大小: %d bytes\n", oneCleanerSize)
	fmt.Printf("400台设备 Cleaner 总内存: %.2f KB\n", float64(oneCleanerSize*400)/1024)
	fmt.Printf("600万点数据内存: %.2f MB\n", float64(totalPoints*112)/1024/1024) // GPSPoint ~112 bytes
	fmt.Println()
	
	// 模拟流式处理吞吐
	fmt.Println("===== 流式处理场景模拟 =====")
	// 400台设备, 每秒400条
	cleaners2 := make(map[string]*cleaner.Cleaner)
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("device_%04d", i)
		cleaners2[id] = cleaner.New(config)
	}
	
	// 模拟1秒内处理400条
	batchStart := time.Now()
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("device_%04d", i)
		pt := cleaner.GPSPoint{
			DeviceID:  id,
			Latitude:  47.6062,
			Longitude: -122.3321,
			Timestamp: int64(i),
			Accuracy:  10.0,
			Speed:     5.0,
			Heading:   0,
		}
		cleaners2[id].Clean(pt)
	}
	batchElapsed := time.Since(batchStart)
	fmt.Printf("400条/秒 处理耗时: %v\n", batchElapsed)
	fmt.Printf("CPU 占比: %.4f%%\n", float64(batchElapsed.Microseconds())/1000000*100)
	fmt.Printf("剩余 CPU: %.2f%%\n", 100-float64(batchElapsed.Microseconds())/1000000*100)
	
	// 模拟1分钟数据
	minuteStart := time.Now()
	for sec := 0; sec < 60; sec++ {
		for dev := 0; dev < 400; dev++ {
			id := fmt.Sprintf("device_%04d", dev)
			pt := cleaner.GPSPoint{
				DeviceID:  id,
				Latitude:  47.6062 + float64(sec)*0.0001,
				Longitude: -122.3321,
				Timestamp: int64(sec) * 1000,
				Accuracy:  10.0,
				Speed:     5.0,
				Heading:   float64(sec) * 6,
			}
			cleaners2[id].Clean(pt)
		}
	}
	minuteElapsed := time.Since(minuteStart)
	fmt.Printf("1分钟数据(24000条)处理耗时: %v\n", minuteElapsed)
	fmt.Printf("CPU 占比: %.2f%%\n", float64(minuteElapsed.Microseconds())/60000000*100)

	_ = c
}

// sizeofCleaner 估算 Cleaner 内存占用
func sizeofCleaner(c *cleaner.Cleaner) int {
	// GPSPoint: DeviceID(16) + Lat(8) + Lng(8) + Timestamp(8) + Accuracy(8) + Speed(8) + Heading(8) = 64 bytes
	// Config: ~100 bytes
	// Cleaner struct fields:
	// - config Config ~100
	// - motionState int 8
	// - staticCounter int 8
	// - stopPoint *GPSPoint 8 + 64 = 72
	// - lastValidPoint *GPSPoint 8 + 64 = 72
	// - motionCounter int 8
	// - velocityWindow []float64 24 + 10*8 = 104
	// - accelerationWindow []float64 24 + 10*8 = 104
	// - calibrated bool 1
	// - prevVelocity *float64 8 + 8 = 16
	// - lastValidOutput *GPSPoint 8 + 64 = 72
	// - lastActualPoint *GPSPoint 8 + 64 = 72
	// - anomalyGraceCount int 8
	// - velocityMean, velocityStd 16
	// - accelMean, accelStd 16
	// - velocityQ1, velocityQ3, velocityIQR 24
	// - accelQ1, accelQ3, accelIQR 24
	// Total ~750 bytes (approximate)
	return 750
}
