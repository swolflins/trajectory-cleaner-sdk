package cleaner

import (
	"fmt"
	"testing"
)

func TestBasicClean(t *testing.T) {
	config := DefaultConfig()
	c := New(config)

	// 模拟正常运动轨迹（每 15s 一个点，约 60km/h）
	points := []GPSPoint{
		{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4074, Timestamp: 1000000, Accuracy: 10},
		{DeviceID: "dev1", Latitude: 39.9060, Longitude: 116.4090, Timestamp: 1015000, Accuracy: 10},
		{DeviceID: "dev1", Latitude: 39.9078, Longitude: 116.4106, Timestamp: 1030000, Accuracy: 10},
		{DeviceID: "dev1", Latitude: 39.9096, Longitude: 116.4122, Timestamp: 1045000, Accuracy: 10},
	}

	for i, p := range points {
		result := c.Clean(p)
		fmt.Printf("Point %d: %s\n", i, result)
		if !result.HasOutput() && i < 3 {
			t.Errorf("Point %d should have output, got %s", i, result.Action)
		}
	}
}

func TestAccuracyFilter(t *testing.T) {
	config := DefaultConfig()
	config.MaxAccuracy = 30.0
	c := New(config)

	// 精度 50m 超过阈值 30m，应被丢弃
	badPoint := GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4074, Timestamp: 1000000, Accuracy: 50}
	result := c.Clean(badPoint)
	if !result.IsDropped() {
		t.Errorf("Bad accuracy point should be dropped, got action=%s", result.Action)
	}
}

func TestStaticState(t *testing.T) {
	config := DefaultConfig()
	config.StaticQueueSize = 3
	config.StaticDistanceThreshold = 10.0
	c := New(config)

	// 先来一个运动点
	c.Clean(GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4074, Timestamp: 1000000, Accuracy: 5})

	// 来 3 个几乎不动的点（距离 < 10m），应进入静止状态
	c.Clean(GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4075, Timestamp: 1015000, Accuracy: 5})
	c.Clean(GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4076, Timestamp: 1030000, Accuracy: 5})
	c.Clean(GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4077, Timestamp: 1045000, Accuracy: 5})

	// 此时应该是静止状态
	if c.GetMotionState() != StateStatic {
		t.Errorf("Should be STATIC after 3 close points, got %s", c.GetMotionState())
	}

	// 再来一个不动点，应被丢弃
	result := c.Clean(GPSPoint{DeviceID: "dev1", Latitude: 39.9042, Longitude: 116.4078, Timestamp: 1060000, Accuracy: 5})
	if !result.IsDropped() {
		t.Errorf("Static point should be dropped, got %s", result.Action)
	}
}

func TestAnomalyDetection(t *testing.T) {
	config := DefaultConfig()
	config.StatsWindowSize = 5
	config.AnomalyMethod = "iqr"
	c := New(config)

	// 先积累 5 个正常点（每 15s 移动约 200m）
	baseLat := 39.9042
	for i := 0; i < 5; i++ {
		c.Clean(GPSPoint{
			DeviceID:  "dev1",
			Latitude:  baseLat + float64(i)*0.002,
			Longitude: 116.4074,
			Timestamp: int64(1000000 + i*15000),
			Accuracy:  5,
		})
	}

	// 第 6 个点跳到 50km 外，应该被检测为异常
	farPoint := GPSPoint{
		DeviceID:  "dev1",
		Latitude:   baseLat + 0.5, // 约 55km 外
		Longitude: 116.4074,
		Timestamp:  1075000,
		Accuracy:   5,
	}
	result := c.Clean(farPoint)
	if result.Action != ActionReplacedAnomaly && result.Action != ActionDroppedAnomaly {
		t.Errorf("Far point should be detected as anomaly, got %s (reason: %s)", result.Action, result.Reason)
	}
}
