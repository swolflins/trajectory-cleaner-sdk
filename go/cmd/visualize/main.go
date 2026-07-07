package main

import (
	"cleaner"
	"cleaner/eval"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

type PointResult struct {
	Index       int     `json:"index"`
	RawLat      float64 `json:"rawLat"`
	RawLon      float64 `json:"rawLon"`
	OutLat      float64 `json:"outLat"`
	OutLon      float64 `json:"outLon"`
	HasOutput   bool    `json:"hasOutput"`
	Action      string  `json:"action"`
	Reason      string  `json:"reason"`
	ErrorMeters float64 `json:"errorMeters"`
	IsInjected  bool    `json:"isInjected"`
	IsStatic    bool    `json:"isStatic"`
	OrigLat     float64 `json:"origLat"`
	OrigLon     float64 `json:"origLon"`
}

type VisDataset struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Points      []PointResult `json:"points"`
	Stats       VisStats      `json:"stats"`
}

type VisStats struct {
	TotalPoints   int     `json:"totalPoints"`
	Passed       int     `json:"passed"`
	Dropped      int     `json:"dropped"`
	Replaced     int     `json:"replaced"`
	Retention    float64 `json:"retention"`
	InjectedCount int    `json:"injectedCount"`
	DetectedCount int    `json:"detectedCount"`
}

func main() {
	fmt.Println("Generating visualization data...")

	var datasets []VisDataset
	datasets = append(datasets, generateSyntheticVis())
	datasets = append(datasets, generateSeattleVis())
	datasets = append(datasets, generateGeoLifeVis())

	jsonData, _ := json.MarshalIndent(datasets, "", "  ")
	jsonPath := "/data/user/work/vis_data.json"
	os.WriteFile(jsonPath, jsonData, 0644)
	fmt.Printf("JSON data saved to %s (%d bytes)\n", jsonPath, len(jsonData))
	for _, ds := range datasets {
		fmt.Printf("  - %s: %d points\n", ds.Name, len(ds.Points))
	}
}

// injectLargeJumps 注入大幅度跳变异常（5-15km），让可视化效果明显
func injectLargeJumps(traj eval.Trajectory, jumpRate float64, staticRate float64) (eval.Trajectory, []eval.AnomalyLabel) {
	n := len(traj.Points)
	points := make([]cleaner.GPSPoint, n)
	copy(points, traj.Points)
	labels := make([]eval.AnomalyLabel, n)
	for i := range labels {
		labels[i] = eval.AnomalyLabel{Index: i, OriginalLat: points[i].Latitude, OriginalLon: points[i].Longitude}
	}

	// 注入大幅跳变：偏移 0.05-0.15 度（约 5-15km）
	jumpIndices := []int{}
	for i := 5; i < n-1; i++ {
		if randFloat() < jumpRate && len(jumpIndices) < 8 {
			// 大偏移：5-15km
			offset := 0.05 + randFloat()*0.10
			angle := randFloat() * 2 * math.Pi
			originalLat := points[i].Latitude
			originalLon := points[i].Longitude
			points[i].Latitude = originalLat + offset*math.Sin(angle)
			points[i].Longitude = originalLon + offset*math.Cos(angle)
			labels[i].IsAnomaly = true
			labels[i].OriginalLat = originalLat
			labels[i].OriginalLon = originalLon
			jumpIndices = append(jumpIndices, i)
		}
	}

	// 注入静止抖动：在某个位置附近随机抖动 30m
	for i := 5; i < n-8; i++ {
		isJump := false
		for _, ji := range jumpIndices {
			if i >= ji-1 && i <= ji+1 {
				isJump = true
				break
			}
		}
		if isJump {
			continue
		}
		if randFloat() < staticRate {
			baseLat := points[i].Latitude
			baseLon := points[i].Longitude
			count := 3 + int(randFloat()*2)
			for j := 0; j < count && i+j < n; j++ {
				jitterLat := baseLat + (randFloat()-0.5)*0.0003 // ~30m
				jitterLon := baseLon + (randFloat()-0.5)*0.0003
				originalLat := points[i+j].Latitude
				originalLon := points[i+j].Longitude
				points[i+j].Latitude = jitterLat
				points[i+j].Longitude = jitterLon
				labels[i+j].IsDropped = true
				labels[i+j].OriginalLat = originalLat
				labels[i+j].OriginalLon = originalLon
			}
			i += count
		}
	}

	return eval.Trajectory{DeviceID: traj.DeviceID, Points: points}, labels
}

func generateSyntheticVis() VisDataset {
	traj := generateNormalTrajectory(60, 15)
	injected, labels := injectLargeJumps(traj, 0.12, 0.06)

	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10
	cleanerInst := cleaner.New(config)

	return processTrajectory(injected, labels, config, cleanerInst, "Synthetic (合成数据)",
		"60 个点，注入大幅跳变异常(5-15km) + 静止抖动(30m)，15s 上报间隔")
}

func generateSeattleVis() VisDataset {
	traj, err := eval.LoadSeattleGPS("/data/user/work/datasets/seattle_gps.txt")
	if err != nil {
		fmt.Printf("Error loading Seattle: %v\n", err)
		return VisDataset{}
	}

	ds := eval.DownsampleTo15s(traj)
	if len(ds.Points) > 60 {
		ds.Points = ds.Points[:60]
	}

	injected, labels := injectLargeJumps(ds, 0.12, 0.06)

	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10
	cleanerInst := cleaner.New(config)

	return processTrajectory(injected, labels, config, cleanerInst, "Seattle (真实驾驶 GPS)",
		"西雅图驾驶数据降采样到 15s，取前 60 个点，注入大幅跳变(5-15km) + 静止抖动")
}

func generateGeoLifeVis() VisDataset {
	trajectories, err := eval.LoadGeoLife("/data/user/work/datasets/geolife/Geolife Trajectories 1.3/Data", 10)
	if err != nil || len(trajectories) == 0 {
		fmt.Printf("Error loading GeoLife: %v\n", err)
		return VisDataset{}
	}

	var traj eval.Trajectory
	for _, t := range trajectories {
		if len(t.Points) > 200 {
			traj = t
			break
		}
	}
	if len(traj.Points) == 0 {
		traj = trajectories[0]
	}

	ds := eval.DownsampleTo15s(traj)
	if len(ds.Points) > 60 {
		ds.Points = ds.Points[:60]
	}

	injected, labels := injectLargeJumps(ds, 0.12, 0.06)

	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10
	cleanerInst := cleaner.New(config)

	return processTrajectory(injected, labels, config, cleanerInst, "GeoLife (真实用户轨迹)",
		"微软 GeoLife 用户轨迹降采样到 15s，取前 60 个点，注入大幅跳变(5-15km) + 静止抖动")
}

func processTrajectory(injected eval.Trajectory, labels []eval.AnomalyLabel, config cleaner.Config, cleanerInst *cleaner.Cleaner, name, desc string) VisDataset {
	var points []PointResult
	stats := VisStats{}

	for i := 0; i < len(injected.Points); i++ {
		pt := injected.Points[i]
		result := cleanerInst.Clean(pt)

		pr := PointResult{
			Index:      i,
			RawLat:     pt.Latitude,
			RawLon:     pt.Longitude,
			OrigLat:    labels[i].OriginalLat,
			OrigLon:    labels[i].OriginalLon,
			IsInjected: labels[i].IsAnomaly,
			IsStatic:   labels[i].IsDropped,
		}

		if result.HasOutput() {
			out := *result.OutputPoint
			pr.OutLat = out.Latitude
			pr.OutLon = out.Longitude
			pr.HasOutput = true
			pr.ErrorMeters = haversine(labels[i].OriginalLat, labels[i].OriginalLon, out.Latitude, out.Longitude)
		} else {
			pr.OutLat = pt.Latitude
			pr.OutLon = pt.Longitude
			pr.HasOutput = false
		}

		pr.Action = result.Action.String()
		pr.Reason = result.Reason

		stats.TotalPoints++
		if result.HasOutput() {
			stats.Passed++
		} else {
			stats.Dropped++
		}
		if result.Action == cleaner.ActionReplacedAnomaly {
			stats.Replaced++
		}
		if pr.IsInjected || pr.IsStatic {
			stats.InjectedCount++
		}
		if (pr.IsInjected || pr.IsStatic) && (result.Action == cleaner.ActionReplacedAnomaly || result.Action == cleaner.ActionDroppedAnomaly || result.Action == cleaner.ActionDroppedStatic) {
			stats.DetectedCount++
		}

		points = append(points, pr)
	}

	if stats.TotalPoints > 0 {
		stats.Retention = float64(stats.Passed) / float64(stats.TotalPoints)
	}

	return VisDataset{Name: name, Description: desc, Points: points, Stats: stats}
}

func generateNormalTrajectory(n int, intervalSec int) eval.Trajectory {
	var points []cleaner.GPSPoint
	lat := 39.9042
	lon := 116.4074
	ts := int64(1000000)

	for i := 0; i < n; i++ {
		lat += 0.0018 * (0.8 + randFloat()*0.4)
		lon += 0.0015 * (0.8 + randFloat()*0.4)
		points = append(points, cleaner.GPSPoint{
			DeviceID:  "synthetic",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: ts,
			Accuracy:  5.0 + randFloat()*5.0,
			Speed:     13.0 + randFloat()*4.0,
		})
		ts += int64(intervalSec * 1000)
	}

	return eval.Trajectory{DeviceID: "synthetic", Points: points}
}

var randState uint64 = 42

func randFloat() float64 {
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / float64(1<<53)
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	pi := math.Pi
	dLat := (lat2 - lat1) * pi / 180
	dLon := (lon2 - lon1) * pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*pi/180)*math.Cos(lat2*pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return r * c
}
