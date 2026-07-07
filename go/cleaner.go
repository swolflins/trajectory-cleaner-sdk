// Package cleaner 提供轻量级 GNSS 轨迹数据清洗能力
//
// Pipeline: 精度过滤 → 伪静止状态机 → Z-score/IQR 异常检测 → 异常点替代 → 输出
//
// 使用示例:
//
//	config := cleaner.DefaultConfig()
//	c := cleaner.New(config)
//
//	for _, point := range rawPoints {
//	    result := c.Clean(point)
//	    if result.HasOutput() {
//	        publish(result.OutputPoint)
//	    }
//	}
package cleaner

// GPSPoint GNSS 轨迹点数据模型
type GPSPoint struct {
	DeviceID  string
	Latitude  float64 // 纬度 WGS84
	Longitude float64 // 经度 WGS84
	Timestamp int64   // 毫秒时间戳
	Accuracy  float64 // 定位精度(米)，-1 表示无此字段
	Speed     float64 // 速度 m/s，-1 表示无此字段
	Heading   float64 // 航向角 0-360，-1 表示无此字段
}

// HasAccuracy 是否有精度字段
func (p GPSPoint) HasAccuracy() bool {
	return p.Accuracy >= 0
}

// HasSpeed 是否有速度字段
func (p GPSPoint) HasSpeed() bool {
	return p.Speed >= 0
}

// DistanceTo 计算与另一个点的大圆距离(米)，使用 Haversine 公式
func (p GPSPoint) DistanceTo(other GPSPoint) float64 {
	const earthRadius = 6371000.0 // 米

	lat1 := p.Latitude * pi / 180
	lat2 := other.Latitude * pi / 180
	dLat := (other.Latitude - p.Latitude) * pi / 180
	dLon := (other.Longitude - p.Longitude) * pi / 180

	a := sin(dLat/2)*sin(dLat/2) +
		cos(lat1)*cos(lat2)*sin(dLon/2)*sin(dLon/2)
	c := 2 * atan2(sqrt(a), sqrt(1-a))

	return earthRadius * c
}

// TimeDiffSeconds 计算与另一个点的时间差(秒)
func (p GPSPoint) TimeDiffSeconds(other GPSPoint) float64 {
	diff := p.Timestamp - other.Timestamp
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) / 1000.0
}

// VelocityTo 计算到另一个点的瞬时速度(m/s)
func (p GPSPoint) VelocityTo(other GPSPoint) float64 {
	dt := p.TimeDiffSeconds(other)
	if dt == 0 {
		return 0
	}
	return p.DistanceTo(other) / dt
}

// WithTimestamp 创建一个相同位置、不同时间戳的新点(用于异常替代)
func (p GPSPoint) WithTimestamp(ts int64) GPSPoint {
	return GPSPoint{
		DeviceID:  p.DeviceID,
		Latitude:  p.Latitude,
		Longitude: p.Longitude,
		Timestamp: ts,
		Accuracy:  p.Accuracy,
		Speed:     p.Speed,
		Heading:   p.Heading,
	}
}
