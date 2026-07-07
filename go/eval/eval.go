// Package eval 提供轨迹清洗 SDK 的评测框架
// 支持多数据集加载、降采样、异常注入、指标计算
package eval

import (
	"cleaner"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ===== 数据结构 =====

// Trajectory 一条完整的轨迹
type Trajectory struct {
	DeviceID string
	Points   []cleaner.GPSPoint
}

// AnomalyLabel 标记某个点是否为注入的异常
type AnomalyLabel struct {
	Index       int  // 在原始轨迹中的索引
	IsAnomaly   bool // 是否是注入的异常
	IsDropped   bool // 是否应该被丢弃（静止点）
	OriginalLat float64 // 原始纬度（用于计算偏差）
	OriginalLon float64 // 原始经度
}

// Dataset 一个数据集的评测数据
type Dataset struct {
	Name        string
	Trajectories []Trajectory
	Labels      [][]AnomalyLabel // 每条轨迹的异常标签
}

// Metrics 评测指标
type Metrics struct {
	DatasetName string

	// 基础统计
	TotalPoints    int // 总点数
	OutputPoints   int // 输出点数（通过清洗的）
	DroppedPoints  int // 丢弃点数
	ReplacedPoints int // 替换点数

	// 坐标精度
	RMSE      float64 // 均方根误差(米)
	MeanError float64 // 平均误差(米)
	MaxError  float64 // 最大误差(米)
	P95Error  float64 // 95 百分位误差(米)

	// 异常检测
	TruePositives  int // 正确检测的异常
	FalsePositives  int // 误报（正常点被判异常）
	FalseNegatives  int // 漏报（异常点未检测到）
	TrueNegatives   int // 正确放行的正常点

	// 静止检测
	StaticDetected    int // 正确检测的静止点
	StaticTotal       int // 应检测的静止点总数

	// 数据保留率
	RetentionRate float64 // 输出点数/总点数
}

// Precision 精确率
func (m Metrics) Precision() float64 {
	tp := m.TruePositives
	fp := m.FalsePositives
	if tp+fp == 0 {
		return 0
	}
	return float64(tp) / float64(tp+fp)
}

// Recall 召回率
func (m Metrics) Recall() float64 {
	tp := m.TruePositives
	fn := m.FalseNegatives
	if tp+fn == 0 {
		return 0
	}
	return float64(tp) / float64(tp+fn)
}

// F1Score F1 值
func (m Metrics) F1Score() float64 {
	p := m.Precision()
	r := m.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// StaticRecall 静止检测召回率
func (m Metrics) StaticRecall() float64 {
	if m.StaticTotal == 0 {
		return 0
	}
	return float64(m.StaticDetected) / float64(m.StaticTotal)
}

func (m Metrics) String() string {
	return fmt.Sprintf(
		"%s: points=%d, output=%d (%.1f%%), RMSE=%.2fm, meanErr=%.2fm, maxErr=%.2fm, P95=%.2fm, "+
			"anomaly P=%.3f R=%.3f F1=%.3f, static R=%.3f",
		m.DatasetName, m.TotalPoints, m.OutputPoints, m.RetentionRate*100,
		m.RMSE, m.MeanError, m.MaxError, m.P95Error,
		m.Precision(), m.Recall(), m.F1Score(), m.StaticRecall(),
	)
}

// ===== 数据加载器 =====

// LoadSeattleGPS 加载 Seattle 数据集 GPS 数据
// 格式: Date(UTC)  Time(UTC)  Latitude  Longitude (tab-separated)
func LoadSeattleGPS(path string) (Trajectory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Trajectory{}, err
	}

	lines := strings.Split(string(data), "\n")
	var points []cleaner.GPSPoint

	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // 跳过表头和空行
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		dateStr := fields[0] // 17-Jan-2009
		timeStr := fields[1] // 20:27:37
		lat := parseFloat(fields[2])
		lon := parseFloat(fields[3])

		ts := parseSeattleTimestamp(dateStr, timeStr)

		points = append(points, cleaner.GPSPoint{
			DeviceID:  "seattle_test",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: ts,
			Accuracy:  10.0, // Seattle 数据没有精度字段，假设 10m
		})
	}

	return Trajectory{DeviceID: "seattle_test", Points: points}, nil
}

// LoadGeoLife 加载 GeoLife 数据集（取前 N 条轨迹）
// 格式: .plt 文件，前 6 行为头，第 7 行起为 lat,lon,0,alt,days,date,time
func LoadGeoLife(baseDir string, maxTrajectories int) ([]Trajectory, error) {
	var files []string

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasSuffix(path, ".plt") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files) // 确保可重复
	if maxTrajectories > 0 && len(files) > maxTrajectories {
		files = files[:maxTrajectories]
	}

	var trajectories []Trajectory
	for i, f := range files {
		traj, err := loadGeoLifeFile(f, fmt.Sprintf("geolife_%d", i))
		if err != nil || len(traj.Points) < 20 {
			continue
		}
		trajectories = append(trajectories, traj)
	}

	return trajectories, nil
}

func loadGeoLifeFile(path string, deviceID string) (Trajectory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Trajectory{}, err
	}

	lines := strings.Split(string(data), "\n")
	var points []cleaner.GPSPoint

	for i := 6; i < len(lines); i++ { // 跳过前 6 行头
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 7 {
			continue
		}
		lat := parseFloat(fields[0])
		lon := parseFloat(fields[1])
		dateStr := fields[5] // 2008-05-29
		timeStr := fields[6] // 01:20:31
		ts := parseGeoLifeTimestamp(dateStr, timeStr)

		points = append(points, cleaner.GPSPoint{
			DeviceID:  deviceID,
			Latitude:  lat,
			Longitude: lon,
			Timestamp: ts,
			Accuracy:  10.0,
		})
	}

	return Trajectory{DeviceID: deviceID, Points: points}, nil
}

// ===== 降采样 =====

// DownsampleTo15s 将轨迹降采样到 15 秒间隔
func DownsampleTo15s(traj Trajectory) Trajectory {
	if len(traj.Points) == 0 {
		return traj
	}

	var result []cleaner.GPSPoint
	result = append(result, traj.Points[0])
	lastTs := traj.Points[0].Timestamp

	for i := 1; i < len(traj.Points); i++ {
		if traj.Points[i].Timestamp-lastTs >= 15000 { // 15 秒 = 15000ms
			result = append(result, traj.Points[i])
			lastTs = traj.Points[i].Timestamp
		}
	}

	return Trajectory{DeviceID: traj.DeviceID, Points: result}
}

// ===== 异常注入 =====

// InjectAnomalies 向轨迹中注入合成异常，返回带标签的轨迹
// 异常类型: 跳变点(坐标突变)、静止抖动(原地抖动)
func InjectAnomalies(traj Trajectory, jumpRate float64, staticRate float64) (Trajectory, []AnomalyLabel) {
	n := len(traj.Points)
	if n < 10 {
		return traj, make([]AnomalyLabel, n)
	}

	points := make([]cleaner.GPSPoint, n)
	copy(points, traj.Points)
	labels := make([]AnomalyLabel, n)
	for i := range labels {
		labels[i] = AnomalyLabel{Index: i, OriginalLat: points[i].Latitude, OriginalLon: points[i].Longitude}
	}

	// 注入跳变异常：坐标突变到 500m-2km 外
	for i := 5; i < n-1; i++ {
		if randFloat() < jumpRate {
			// 随机偏移 0.005-0.02 度（约 500m-2km）
			offset := 0.005 + randFloat()*0.015
			angle := randFloat() * 2 * math.Pi
			originalLat := points[i].Latitude
			originalLon := points[i].Longitude
			points[i].Latitude = originalLat + offset*math.Sin(angle)
			points[i].Longitude = originalLon + offset*math.Cos(angle)
			labels[i].IsAnomaly = true
		}
	}

	// 注入静止抖动：在某个位置附近随机抖动
	for i := 5; i < n-5; i++ {
		if randFloat() < staticRate {
			// 选一个基准点
			baseLat := points[i].Latitude
			baseLon := points[i].Longitude
			// 接下来的 3-5 个点改为在基准点附近抖动
			count := 3 + int(randFloat()*3)
			for j := 0; j < count && i+j < n; j++ {
				jitterLat := baseLat + (randFloat()-0.5)*0.0002 // ~20m
				jitterLon := baseLon + (randFloat()-0.5)*0.0002
				points[i+j].Latitude = jitterLat
				points[i+j].Longitude = jitterLon
				labels[i+j].IsDropped = true // 静止点应该被丢弃
				i += count
			}
		}
	}

	return Trajectory{DeviceID: traj.DeviceID, Points: points}, labels
}

// ===== 评测运行 =====

// Evaluate 在单个轨迹上运行评测
func Evaluate(traj Trajectory, labels []AnomalyLabel, config cleaner.Config, datasetName string) Metrics {
	cleanerInst := cleaner.New(config)
	m := Metrics{DatasetName: datasetName}

	var errors []float64

	for i := 0; i < len(traj.Points); i++ {
		point := traj.Points[i]
		result := cleanerInst.Clean(point)

		m.TotalPoints++

		if result.HasOutput() {
			m.OutputPoints++
			// 计算坐标误差
			if i < len(labels) {
				origLat := labels[i].OriginalLat
				origLon := labels[i].OriginalLon
				if origLat != 0 {
					out := *result.OutputPoint
					err := haversine(origLat, origLon, out.Latitude, out.Longitude)
					errors = append(errors, err)
					m.MeanError += err
					if err > m.MaxError {
						m.MaxError = err
					}
				}
			}
		} else {
			m.DroppedPoints++
		}

		if result.Action == cleaner.ActionReplacedAnomaly {
			m.ReplacedPoints++
		}

		// 异常检测统计
		if i < len(labels) {
			isAnomaly := labels[i].IsAnomaly
			isDetected := result.Action == cleaner.ActionReplacedAnomaly ||
				result.Action == cleaner.ActionDroppedAnomaly

			if isAnomaly && isDetected {
				m.TruePositives++
			} else if isAnomaly && !isDetected {
				m.FalseNegatives++
			} else if !isAnomaly && isDetected {
				m.FalsePositives++
			} else {
				m.TrueNegatives++
			}

			// 静止点检测
			if labels[i].IsDropped {
				m.StaticTotal++
				if result.IsDropped() || result.Action == cleaner.ActionDroppedStatic {
					m.StaticDetected++
				}
			}
		}
	}

	// 计算汇总指标
	if len(errors) > 0 {
		m.MeanError /= float64(len(errors))
		var sumSq float64
		for _, e := range errors {
			sumSq += e * e
		}
		m.RMSE = math.Sqrt(sumSq / float64(len(errors)))

		// P95
		sort.Float64s(errors)
		p95Idx := int(float64(len(errors)) * 0.95)
		if p95Idx >= len(errors) {
			p95Idx = len(errors) - 1
		}
		m.P95Error = errors[p95Idx]
	}

	if m.TotalPoints > 0 {
		m.RetentionRate = float64(m.OutputPoints) / float64(m.TotalPoints)
	}

	return m
}

// ===== 工具函数 =====

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f
}

func parseSeattleTimestamp(dateStr, timeStr string) int64 {
	// 17-Jan-2009 20:27:37
	layout := "2-Jan-2006 15:04:05"
	t, err := time.Parse(layout, dateStr+" "+timeStr)
	if err != nil {
		// 尝试其他格式
		t, err = time.Parse("2006-01-02 15:04:05", dateStr+" "+timeStr)
		if err != nil {
			return 0
		}
	}
	return t.UnixMilli()
}

func parseGeoLifeTimestamp(dateStr, timeStr string) int64 {
	// 2008-05-29 01:20:31
	t, err := time.Parse("2006-01-02 15:04:05", dateStr+" "+timeStr)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return r * c
}

var randState uint64 = 12345

func randFloat() float64 {
	// 简单的伪随机数生成器（不依赖 math/rand）
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / float64(1<<53)
}
