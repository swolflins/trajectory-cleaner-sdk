package eval

import (
	"cleaner"
	"encoding/json"
	"fmt"
	"os"
)

// RunAllEvaluations 运行全部数据集评测
func RunAllEvaluations(seattlePath, geolifeDir, outputPath string) []Metrics {
	var allMetrics []Metrics

	// ===== 1. 合成数据集（可控实验）=====
	fmt.Println("=== 1. Synthetic Dataset ===")
	synthMetrics := runSyntheticEvaluation()
	allMetrics = append(allMetrics, synthMetrics...)

	// ===== 2. Seattle 数据集 =====
	if seattlePath != "" {
		fmt.Println("\n=== 2. Seattle Dataset (Newson & Krumm) ===")
		seattleMetrics := runSeattleEvaluation(seattlePath)
		allMetrics = append(allMetrics, seattleMetrics...)
	}

	// ===== 3. GeoLife 数据集 =====
	if geolifeDir != "" {
		fmt.Println("\n=== 3. GeoLife Dataset ===")
		geolifeMetrics := runGeoLifeEvaluation(geolifeDir)
		allMetrics = append(allMetrics, geolifeMetrics...)
	}

	// 输出 JSON 报告
	if outputPath != "" {
		writeJSONReport(allMetrics, outputPath)
	}

	// 打印汇总
	fmt.Println("\n========== EVALUATION SUMMARY ==========")
	for _, m := range allMetrics {
		fmt.Println(m.String())
	}

	return allMetrics
}

// runSyntheticEvaluation 合成数据集评测（已知 ground truth）
func runSyntheticEvaluation() []Metrics {
	var metrics []Metrics

	// 场景 1: 正常运动轨迹 + 注入跳变异常
	fmt.Println("  [1a] Normal trajectory with jump anomalies...")
	traj := generateNormalTrajectory(100, 15) // 100 点, 15s 间隔
	injected, labels := InjectAnomalies(traj, 0.05, 0.0) // 5% 跳变

	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10
	m := Evaluate(injected, labels, config, "Synthetic-Jump-5%")
	metrics = append(metrics, m)
	fmt.Printf("    %s\n", m.String())

	// 场景 2: 正常运动轨迹 + 注入静止抖动
	fmt.Println("  [1b] Normal trajectory with static jitter...")
	traj2 := generateNormalTrajectory(100, 15)
	injected2, labels2 := InjectAnomalies(traj2, 0.0, 0.1) // 10% 静止
	m2 := Evaluate(injected2, labels2, config, "Synthetic-Static-10%")
	metrics = append(metrics, m2)
	fmt.Printf("    %s\n", m2.String())

	// 场景 3: 混合异常
	fmt.Println("  [1c] Mixed anomalies...")
	traj3 := generateNormalTrajectory(200, 15)
	injected3, labels3 := InjectAnomalies(traj3, 0.05, 0.08)
	m3 := Evaluate(injected3, labels3, config, "Synthetic-Mixed")
	metrics = append(metrics, m3)
	fmt.Printf("    %s\n", m3.String())

	// 场景 4: Z-score 模式对比
	fmt.Println("  [1d] Z-score mode comparison...")
	zscoreConfig := cleaner.ZScoreConfig()
	zscoreConfig.StatsWindowSize = 10
	m4 := Evaluate(injected3, labels3, zscoreConfig, "Synthetic-Mixed-ZScore")
	metrics = append(metrics, m4)
	fmt.Printf("    %s\n", m4.String())

	// 场景 5: 不同精度阈值
	fmt.Println("  [1e] Accuracy filter impact...")
	strictConfig := cleaner.DefaultConfig()
	strictConfig.MaxAccuracy = 15.0
	m5 := Evaluate(injected3, labels3, strictConfig, "Synthetic-Mixed-StrictAcc")
	metrics = append(metrics, m5)
	fmt.Printf("    %s\n", m5.String())

	return metrics
}

// runSeattleEvaluation Seattle 数据集评测
func runSeattleEvaluation(path string) []Metrics {
	var metrics []Metrics

	fmt.Println("  Loading Seattle GPS data...")
	traj, err := LoadSeattleGPS(path)
	if err != nil {
		fmt.Printf("    Error: %v\n", err)
		return metrics
	}
	fmt.Printf("    Loaded %d points (1Hz original)\n", len(traj.Points))

	// 2a: 原始 1Hz 数据评测（无异常注入，验证不误杀）
	fmt.Println("  [2a] Original 1Hz data (no injection)...")
	labels := make([]AnomalyLabel, len(traj.Points))
	for i := range labels {
		labels[i] = AnomalyLabel{
			Index:       i,
			OriginalLat: traj.Points[i].Latitude,
			OriginalLon: traj.Points[i].Longitude,
		}
	}
	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10
	m := Evaluate(traj, labels, config, "Seattle-1Hz-Original")
	metrics = append(metrics, m)
	fmt.Printf("    %s\n", m.String())

	// 2b: 降采样到 15s + 注入异常
	fmt.Println("  [2b] Downsampled to 15s + 5% anomalies...")
	downsampled := DownsampleTo15s(traj)
	fmt.Printf("    Downsampled to %d points (15s interval)\n", len(downsampled.Points))

	injected, injLabels := InjectAnomalies(downsampled, 0.05, 0.05)
	m2 := Evaluate(injected, injLabels, config, "Seattle-15s-Injected")
	metrics = append(metrics, m2)
	fmt.Printf("    %s\n", m2.String())

	// 2c: 降采样到 15s，无注入（验证降采样后正常数据通过率）
	fmt.Println("  [2c] Downsampled to 15s, no injection...")
	downLabels := make([]AnomalyLabel, len(downsampled.Points))
	for i := range downLabels {
		downLabels[i] = AnomalyLabel{
			Index:       i,
			OriginalLat: downsampled.Points[i].Latitude,
			OriginalLon: downsampled.Points[i].Longitude,
		}
	}
	m3 := Evaluate(downsampled, downLabels, config, "Seattle-15s-Clean")
	metrics = append(metrics, m3)
	fmt.Printf("    %s\n", m3.String())

	return metrics
}

// runGeoLifeEvaluation GeoLife 数据集评测
func runGeoLifeEvaluation(dir string) []Metrics {
	var metrics []Metrics

	fmt.Println("  Loading GeoLife trajectories (first 50)...")
	trajectories, err := LoadGeoLife(dir, 50)
	if err != nil {
		fmt.Printf("    Error: %v\n", err)
		return metrics
	}
	fmt.Printf("    Loaded %d trajectories\n", len(trajectories))

	// 聚合所有轨迹的指标
	aggMetrics := Metrics{DatasetName: "GeoLife-50traj-Aggregate"}
	config := cleaner.DefaultConfig()
	config.StatsWindowSize = 10

	var allErrors []float64

	for i, traj := range trajectories {
		// 降采样到 15s
		ds := DownsampleTo15s(traj)
		if len(ds.Points) < 20 {
			continue
		}

		// 注入 5% 跳变 + 5% 静止
		injected, labels := InjectAnomalies(ds, 0.05, 0.05)
		m := Evaluate(injected, labels, config, fmt.Sprintf("GeoLife-traj%d", i))

		// 聚合
		aggMetrics.TotalPoints += m.TotalPoints
		aggMetrics.OutputPoints += m.OutputPoints
		aggMetrics.DroppedPoints += m.DroppedPoints
		aggMetrics.ReplacedPoints += m.ReplacedPoints
		aggMetrics.TruePositives += m.TruePositives
		aggMetrics.FalsePositives += m.FalsePositives
		aggMetrics.FalseNegatives += m.FalseNegatives
		aggMetrics.TrueNegatives += m.TrueNegatives
		aggMetrics.StaticDetected += m.StaticDetected
		aggMetrics.StaticTotal += m.StaticTotal

		if i < 5 {
			fmt.Printf("    traj %d: %s\n", i, m.String())
		}
	}

	// 计算聚合 RMSE（需要重新遍历）
	// 简化：用聚合指标
	if aggMetrics.TotalPoints > 0 {
		aggMetrics.RetentionRate = float64(aggMetrics.OutputPoints) / float64(aggMetrics.TotalPoints)
	}

	_ = allErrors // 占位

	metrics = append(metrics, aggMetrics)
	fmt.Printf("    AGGREGATE: %s\n", aggMetrics.String())

	// 单独跑 5 条轨迹的详细评测
	fmt.Println("  Running detailed eval on first 5 trajectories...")
	for i := 0; i < 5 && i < len(trajectories); i++ {
		ds := DownsampleTo15s(trajectories[i])
		if len(ds.Points) < 20 {
			continue
		}
		injected, labels := InjectAnomalies(ds, 0.05, 0.05)
		m := Evaluate(injected, labels, config, fmt.Sprintf("GeoLife-detail-%d", i))
		metrics = append(metrics, m)
		fmt.Printf("    %s\n", m.String())
	}

	return metrics
}

// generateNormalTrajectory 生成正常运动轨迹
func generateNormalTrajectory(n int, intervalSec int) Trajectory {
	var points []cleaner.GPSPoint
	lat := 39.9042 // 北京天安门
	lon := 116.4074
	ts := int64(1000000)

	for i := 0; i < n; i++ {
		// 模拟车辆运动：每步移动约 200m (60km/h * 15s)
		lat += 0.0018 * (0.8 + randFloat()*0.4) // 约 200m
		lon += 0.0015 * (0.8 + randFloat()*0.4)
		points = append(points, cleaner.GPSPoint{
			DeviceID:  "synthetic",
			Latitude:  lat,
			Longitude: lon,
			Timestamp: ts,
			Accuracy:  5.0 + randFloat()*5.0, // 5-10m
			Speed:     13.0 + randFloat()*4.0, // 47-61 km/h
		})
		ts += int64(intervalSec * 1000)
	}

	return Trajectory{DeviceID: "synthetic", Points: points}
}

// writeJSONReport 输出 JSON 报告
func writeJSONReport(metrics []Metrics, path string) {
	data, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		fmt.Printf("Error writing JSON: %v\n", err)
		return
	}
	os.WriteFile(path, data, 0644)
	fmt.Printf("\nJSON report saved to %s\n", path)
}
