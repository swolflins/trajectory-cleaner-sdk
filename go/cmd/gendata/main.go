// Package main: 数据生成器
// 生成 3 个 CSV 轨迹数据集及其对应的 .labels.json 标签文件
// 数据集: seattle (西雅图驾驶), geolife (北京混合出行), synthetic (合成正常轨迹)
package main

import (
	"cleaner"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

// 用固定 seed 生成可重复的伪随机数
var randState uint64 = 42

func randFloat() float64 {
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / float64(1<<53)
}

// LabelEntry 单条异常标签
type LabelEntry struct {
	Index       int     `json:"index"`
	Type        string  `json:"type"`
	OrigLat     float64 `json:"origLat,omitempty"`
	OrigLon     float64 `json:"origLon,omitempty"`
	InjectedLat float64 `json:"injectedLat,omitempty"`
	InjectedLon float64 `json:"injectedLon,omitempty"`
	Accuracy    float64 `json:"accuracy,omitempty"`
	StartIndex  int     `json:"startIndex,omitempty"`
	EndIndex    int     `json:"endIndex,omitempty"`
}

// LabelFile 标签文件
type LabelFile struct {
	TotalPoints int          `json:"totalPoints"`
	Anomalies   []LabelEntry `json:"anomalies"`
}

// dataDir 数据输出根目录(相对于 go 目录)
const dataDir = "../data"

func main() {
	fmt.Println("=== 轨迹数据生成器 ===")

	// 确保目录存在
	for _, sub := range []string{"seattle", "geolife", "synthetic"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
			fmt.Printf("创建目录失败 %s: %v\n", sub, err)
			os.Exit(1)
		}
	}

	// 数据集 1: 西雅图驾驶
	seattlePoints, seattleLabels := generateSeattle()
	writeCSV(filepath.Join(dataDir, "seattle", "gps_sim.csv"), seattlePoints)
	writeLabels(filepath.Join(dataDir, "seattle", "gps_sim.labels.json"), len(seattlePoints), seattleLabels)
	fmt.Printf("[OK] seattle/gps_sim.csv  - %d 点, %d 异常\n", len(seattlePoints), len(seattleLabels))

	// 数据集 2: 北京混合出行 (GeoLife 风格)
	geolifePoints, geolifeLabels := generateGeolife()
	writeCSV(filepath.Join(dataDir, "geolife", "gps_sim.csv"), geolifePoints)
	writeLabels(filepath.Join(dataDir, "geolife", "gps_sim.labels.json"), len(geolifePoints), geolifeLabels)
	fmt.Printf("[OK] geolife/gps_sim.csv - %d 点, %d 异常\n", len(geolifePoints), len(geolifeLabels))

	// 数据集 3: 合成正常轨迹
	synthPoints, synthLabels := generateSynthetic()
	writeCSV(filepath.Join(dataDir, "synthetic", "gps_normal.csv"), synthPoints)
	writeLabels(filepath.Join(dataDir, "synthetic", "gps_normal.labels.json"), len(synthPoints), synthLabels)
	fmt.Printf("[OK] synthetic/gps_normal.csv - %d 点, %d 异常\n", len(synthPoints), len(synthLabels))

	fmt.Println("\n所有数据集生成完成。")
}

// ===== 数据集 1: 西雅图驾驶 =====
// 200 点，起点 47.6205, -122.3493，3 秒间隔，城市道路速度 10-20 m/s
// 注入: 2 个飞点(50,120, 偏移 0.05-0.10 度), 1 段静止抖动(80-89, 30m), 2 个精度劣化点(30,140, acc=80)
func generateSeattle() ([]cleaner.GPSPoint, []LabelEntry) {
	n := 200
	startLat := 47.6205
	startLon := -122.3493
	intervalMs := int64(3000) // 3s
	baseTs := int64(1700000000000)

	points := make([]cleaner.GPSPoint, n)
	lat := startLat
	lon := startLon
	heading := 30.0 // 初始航向

	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID:  "seattle_sim",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs,
			Accuracy:  5.0 + randFloat()*5.0, // 5-10m
		}
		if i < n-1 {
			// 城市道路: 速度 10-20 m/s
			speed := 10.0 + randFloat()*10.0
			// 偶尔转弯(模拟城市路网)
			if randFloat() < 0.12 {
				heading += (randFloat() - 0.5) * 90
			}
			heading += (randFloat() - 0.5) * 4 // 小幅抖动
			heading = normalizeHeading(heading)

			distance := speed * 3.0 // 3s
			lat, lon = moveBy(lat, lon, heading, distance)
		}
	}

	var labels []LabelEntry

	// 注入 2 个飞点 (index 50, 120, 偏移 0.05-0.10 度)
	for _, idx := range []int{50, 120} {
		origLat := points[idx].Latitude
		origLon := points[idx].Longitude
		offset := 0.05 + randFloat()*0.05 // 0.05-0.10 deg
		angle := randFloat() * 2 * math.Pi
		newLat := origLat + offset*math.Sin(angle)
		newLon := origLon + offset*math.Cos(angle)
		points[idx].Latitude = newLat
		points[idx].Longitude = newLon
		labels = append(labels, LabelEntry{
			Index:       idx,
			Type:        "jump",
			OrigLat:     origLat,
			OrigLon:     origLon,
			InjectedLat: newLat,
			InjectedLon: newLon,
		})
	}

	// 注入 1 段静止抖动 (index 80-89, 30m 范围)
	baseJLat := points[80].Latitude
	baseJLon := points[80].Longitude
	for i := 80; i <= 89; i++ {
		// 30m 范围: 30/111320 ≈ 0.000269 deg, 用 ±0.00027
		points[i].Latitude = baseJLat + (randFloat()-0.5)*0.00054
		points[i].Longitude = baseJLon + (randFloat()-0.5)*0.00054
	}
	labels = append(labels, LabelEntry{
		Index:      80,
		Type:       "static",
		StartIndex: 80,
		EndIndex:   89,
	})

	// 注入 2 个精度劣化点 (index 30, 140, accuracy=80)
	for _, idx := range []int{30, 140} {
		points[idx].Accuracy = 80.0
		labels = append(labels, LabelEntry{
			Index:    idx,
			Type:     "accuracy",
			Accuracy: 80.0,
		})
	}

	computeSpeedHeading(points)
	return points, labels
}

// ===== 数据集 2: 北京混合出行 (GeoLife 风格) =====
// 200 点，起点 39.9847, 116.3606，5 秒间隔
// 前 60 步行(1-1.5 m/s), 60-140 驾车(8-15 m/s), 140-200 步行
// 注入: 5 个飞点(20,45,70,100,160), 3 段静止抖动(35-44, 75-84, 175-184)
func generateGeolife() ([]cleaner.GPSPoint, []LabelEntry) {
	n := 200
	startLat := 39.9847
	startLon := 116.3606
	intervalMs := int64(5000) // 5s
	baseTs := int64(1700000000000)

	points := make([]cleaner.GPSPoint, n)
	lat := startLat
	lon := startLon
	heading := 90.0 // 初始向东

	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID:  "geolife_sim",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs,
			Accuracy:  5.0 + randFloat()*5.0,
		}
		if i < n-1 {
			var speed float64
			if i < 60 {
				// 步行 1-1.5 m/s
				speed = 1.0 + randFloat()*0.5
				if randFloat() < 0.2 {
					heading += (randFloat() - 0.5) * 60
				}
			} else if i < 140 {
				// 驾车 8-15 m/s
				speed = 8.0 + randFloat()*7.0
				if randFloat() < 0.08 {
					heading += (randFloat() - 0.5) * 60
				}
			} else {
				// 步行
				speed = 1.0 + randFloat()*0.5
				if randFloat() < 0.2 {
					heading += (randFloat() - 0.5) * 60
				}
			}
			heading += (randFloat() - 0.5) * 3
			heading = normalizeHeading(heading)

			distance := speed * 5.0 // 5s
			lat, lon = moveBy(lat, lon, heading, distance)
		}
	}

	var labels []LabelEntry

	// 注入 5 个飞点 (index 20,45,70,100,160, 偏移 0.02-0.05 度)
	for _, idx := range []int{20, 45, 70, 100, 160} {
		origLat := points[idx].Latitude
		origLon := points[idx].Longitude
		offset := 0.02 + randFloat()*0.03 // 0.02-0.05 deg
		angle := randFloat() * 2 * math.Pi
		newLat := origLat + offset*math.Sin(angle)
		newLon := origLon + offset*math.Cos(angle)
		points[idx].Latitude = newLat
		points[idx].Longitude = newLon
		labels = append(labels, LabelEntry{
			Index:       idx,
			Type:        "jump",
			OrigLat:     origLat,
			OrigLon:     origLon,
			InjectedLat: newLat,
			InjectedLon: newLon,
		})
	}

	// 注入 3 段静止抖动 (35-44, 75-84, 175-184, 30m 范围)
	staticRanges := [][2]int{{35, 44}, {75, 84}, {175, 184}}
	for _, r := range staticRanges {
		baseJLat := points[r[0]].Latitude
		baseJLon := points[r[0]].Longitude
		for i := r[0]; i <= r[1]; i++ {
			points[i].Latitude = baseJLat + (randFloat()-0.5)*0.00054
			points[i].Longitude = baseJLon + (randFloat()-0.5)*0.00054
		}
		labels = append(labels, LabelEntry{
			Index:      r[0],
			Type:       "static",
			StartIndex: r[0],
			EndIndex:   r[1],
		})
	}

	computeSpeedHeading(points)
	return points, labels
}

// ===== 数据集 3: 合成正常轨迹 =====
// 200 点，起点 40.0, 116.4，15 秒间隔，匀速 20 m/s 直线，无异常
func generateSynthetic() ([]cleaner.GPSPoint, []LabelEntry) {
	n := 200
	startLat := 40.0
	startLon := 116.4
	intervalMs := int64(15000) // 15s
	baseTs := int64(1700000000000)

	points := make([]cleaner.GPSPoint, n)
	lat := startLat
	lon := startLon
	heading := 0.0 // 正北直线

	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID:  "synthetic_normal",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs,
			Accuracy:  5.0 + randFloat()*3.0, // 5-8m
		}
		if i < n-1 {
			// 匀速 20 m/s, 15s → 300m, 直线(航向恒定, 加微小噪声)
			distance := 20.0 * 15.0
			h := heading + (randFloat()-0.5)*1.0 // 极小抖动保持近似直线
			lat, lon = moveBy(lat, lon, h, distance)
		}
	}

	computeSpeedHeading(points)
	// 无异常
	return points, []LabelEntry{}
}

// ===== 工具函数 =====

// moveBy 按航向(度)和距离(米)移动坐标
func moveBy(lat, lon, headingDeg, distance float64) (float64, float64) {
	h := headingDeg * math.Pi / 180.0
	dLat := distance * math.Cos(h) / 111320.0
	dLon := distance * math.Sin(h) / (111320.0 * math.Cos(lat*math.Pi/180.0))
	return lat + dLat, lon + dLon
}

// normalizeHeading 归一化航向到 [0, 360)
func normalizeHeading(h float64) float64 {
	for h < 0 {
		h += 360
	}
	for h >= 360 {
		h -= 360
	}
	return h
}

// haversine 计算两点间大圆距离(米)
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	pi := math.Pi
	dLat := (lat2 - lat1) * pi / 180
	dLon := (lon2 - lon1) * pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*pi/180)*math.Cos(lat2*pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// computeSpeedHeading 根据实际位移计算速度和航向角
func computeSpeedHeading(points []cleaner.GPSPoint) {
	var prevHeading float64
	for i := range points {
		if i == 0 {
			points[i].Speed = 0
			points[i].Heading = 0
			continue
		}
		prev := points[i-1]
		curr := points[i]
		dist := haversine(prev.Latitude, prev.Longitude, curr.Latitude, curr.Longitude)
		dt := float64(curr.Timestamp-prev.Timestamp) / 1000.0
		if dt > 0 {
			points[i].Speed = dist / dt
		}
		// 航向角: 以正北为 0, 顺时针
		dLatM := (curr.Latitude - prev.Latitude) * 111320.0
		dLonM := (curr.Longitude - prev.Longitude) * 111320.0 * math.Cos(prev.Latitude*math.Pi/180.0)
		if dist < 0.5 {
			// 几乎没动, 保持上一航向
			points[i].Heading = prevHeading
		} else {
			hdg := math.Atan2(dLonM, dLatM) * 180.0 / math.Pi
			hdg = normalizeHeading(hdg)
			points[i].Heading = hdg
			prevHeading = hdg
		}
	}
}

// writeCSV 写 CSV 文件: timestamp,lat,lon,accuracy,speed,heading
func writeCSV(path string, points []cleaner.GPSPoint) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("创建 CSV 失败 %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// 表头
	w.Write([]string{"timestamp", "lat", "lon", "accuracy", "speed", "heading"})

	for _, p := range points {
		w.Write([]string{
			strconv.FormatInt(p.Timestamp, 10),
			strconv.FormatFloat(p.Latitude, 'f', 7, 64),
			strconv.FormatFloat(p.Longitude, 'f', 7, 64),
			strconv.FormatFloat(p.Accuracy, 'f', 2, 64),
			strconv.FormatFloat(p.Speed, 'f', 3, 64),
			strconv.FormatFloat(p.Heading, 'f', 3, 64),
		})
	}
}

// writeLabels 写标签 JSON 文件
func writeLabels(path string, total int, labels []LabelEntry) {
	lf := LabelFile{
		TotalPoints: total,
		Anomalies:   labels,
	}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		fmt.Printf("序列化标签失败 %s: %v\n", path, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Printf("写标签文件失败 %s: %v\n", path, err)
		os.Exit(1)
	}
}
