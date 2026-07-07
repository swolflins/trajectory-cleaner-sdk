package cleaner

import (
	"fmt"
	"testing"
	"time"
)

// TestExampleUsage 展示 SDK 的完整使用流程
func TestExampleUsage(t *testing.T) {
	// 1. 创建配置（可自定义或使用默认值）
	config := DefaultConfig()
	// 也可切换为 Z-score 模式（对应图片方案 2.2.2 方案一）
	// config := ZScoreConfig()

	// 自定义参数
	config.MaxAccuracy = 30.0              // 精度阈值 30m
	config.StaticDistanceThreshold = 15.0  // 静止判定距离 15m
	config.StatsWindowSize = 10             // 统计窗口

	// 2. 创建清洗器（每个设备一个实例）
	cleaner := New(config)

	// 3. 模拟设备 15s 上报数据
	deviceID := "truck-001"
	baseTime := time.Now().UnixMilli()

	// 模拟正常运动轨迹
	for i := 0; i < 5; i++ {
		point := GPSPoint{
			DeviceID:  deviceID,
			Latitude:  39.9042 + float64(i)*0.002, // 每次向北移动约 222m
			Longitude: 116.4074,
			Timestamp: baseTime + int64(i*15000),
			Accuracy:  8.0,
			Speed:     14.8, // 约 53 km/h
		}
		result := cleaner.Clean(point)
		fmt.Printf("[%s] %s → output: %v\n",
			time.UnixMilli(point.Timestamp).Format("15:04:05"),
			result.Action, result.HasOutput())
	}

	// 4. 模拟一个异常跳变点（信号漂移）
	anomalyPoint := GPSPoint{
		DeviceID:  deviceID,
		Latitude:  40.50, // 突然跳到 60km 外
		Longitude: 116.4074,
		Timestamp: baseTime + 75000,
		Accuracy:  8.0,
	}
	result := cleaner.Clean(anomalyPoint)
	fmt.Printf("[异常点] action=%s, reason=%s, replaced=%v\n",
		result.Action, result.Reason, result.Action == ActionReplacedAnomaly)

	// 5. 模拟静止场景（设备停车）
	stopConfig := DefaultConfig()
	stopConfig.StaticQueueSize = 3 // 便于测试
	stopCleaner := New(stopConfig)

	// 第一个运动点
	stopCleaner.Clean(GPSPoint{DeviceID: deviceID, Latitude: 39.9042, Longitude: 116.4074, Timestamp: baseTime, Accuracy: 5})

	// 3 个几乎不动的点（进入静止）
	for i := 1; i <= 3; i++ {
		point := GPSPoint{
			DeviceID:  deviceID,
			Latitude:  39.9042,
			Longitude: 116.4074 + float64(i)*0.00001, // 微小偏移
			Timestamp: baseTime + int64(i*15000),
			Accuracy:  5,
		}
		result := stopCleaner.Clean(point)
		fmt.Printf("[静止判定 %d] action=%s, state=%s\n", i, result.Action, result.CurrentState)
	}

	// 继续静止的点应被丢弃
	for i := 4; i <= 5; i++ {
		point := GPSPoint{
			DeviceID:  deviceID,
			Latitude:  39.9042,
			Longitude: 116.4074 + float64(i)*0.00001,
			Timestamp: baseTime + int64(i*15000),
			Accuracy:  5,
		}
		result := stopCleaner.Clean(point)
		fmt.Printf("[静止维持 %d] action=%s, dropped=%v\n", i, result.Action, result.IsDropped())
	}

	// 6. 设备重新上线时重置
	cleaner.Reset()
	fmt.Println("Cleaner reset for device reconnection")

	// Output:
	// (示例输出，实际运行结果取决于测试数据)
}
