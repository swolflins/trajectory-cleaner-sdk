// Package main: 参数调优程序
// 对 3 个合成数据集进行分阶段网格搜索，寻找最优清洗参数
// 输出 optimal_params.json 到 ../data/
package main

import (
	"cleaner"
	"cleaner/eval"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
)

// 固定 seed 的伪随机数生成器(用于轨迹生成)
var randState uint64 = 42

func randFloat() float64 {
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / float64(1<<53)
}

// ===== JSON 输出结构 =====

type ConfigJSON struct {
	MaxAccuracy             float64 `json:"maxAccuracy"`
	StaticQueueSize         int     `json:"staticQueueSize"`
	StaticDistanceThreshold float64 `json:"staticDistanceThreshold"`
	MotionConfirmCount      int     `json:"motionConfirmCount"`
	StatsWindowSize         int     `json:"statsWindowSize"`
	AnomalyMethod           string `json:"anomalyMethod"`
	InitialSigma            float64 `json:"initialSigma"`
	ContinuousSigma         float64 `json:"continuousSigma"`
	IqrMultiplier           float64 `json:"iqrMultiplier"`
	MaxVelocity             float64 `json:"maxVelocity"`
	AnomalyStrategy         string `json:"anomalyStrategy"`
}

type MetricsJSON struct {
	F1             float64 `json:"f1"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	RMSE           float64 `json:"rmse"`
	MeanError      float64 `json:"meanError"`
	MaxError       float64 `json:"maxError"`
	P95Error       float64 `json:"p95Error"`
	Retention      float64 `json:"retention"`
	TruePositives  int     `json:"truePositives"`
	FalsePositives int     `json:"falsePositives"`
	FalseNegatives int     `json:"falseNegatives"`
	StaticRecall   float64 `json:"staticRecall"`
}

type DatasetResult struct {
	Config  ConfigJSON  `json:"config"`
	Metrics MetricsJSON `json:"metrics"`
}

type OptimalParams struct {
	Seattle     DatasetResult `json:"seattle"`
	GeoLife     DatasetResult `json:"geolife"`
	Synthetic   DatasetResult `json:"synthetic"`
	Recommended DatasetResult `json:"recommended"`
}

// ===== 内部数据结构 =====

// ComboResult 单次参数组合的评估结果
type ComboResult struct {
	Config  cleaner.Config
	Metrics eval.Metrics
	Score   float64
	Stage   int
}

// preparedDS 预先生成的数据集(轨迹+标签)
type preparedDS struct {
	name     string
	injected eval.Trajectory
	labels   []eval.AnomalyLabel
}

const dataDir = "../data"

func main() {
	fmt.Println("=== 参数调优 (分阶段网格搜索) ===")
	fmt.Println()

	// 定义 3 个数据集规格
	specs := []struct {
		name       string
		jumpRate   float64
		staticRate float64
		genFunc    func() eval.Trajectory
	}{
		{"seattle", 0.05, 0.03, genSeattleTraj},
		{"geolife", 0.06, 0.05, genGeolifeTraj},
		{"synthetic", 0.04, 0.02, genSyntheticTraj},
	}

	// 预先生成轨迹并注入异常(每个数据集只注入一次，复用于所有参数组合)
	var prepared []preparedDS
	for _, sp := range specs {
		traj := sp.genFunc()
		injected, labels := eval.InjectAnomalies(traj, sp.jumpRate, sp.staticRate)
		jumpCount, staticCount := 0, 0
		for _, l := range labels {
			if l.IsAnomaly {
				jumpCount++
			}
			if l.IsDropped {
				staticCount++
			}
		}
		fmt.Printf("[%s] %d 点, 注入: %d 飞点, %d 静止点\n", sp.name, len(traj.Points), jumpCount, staticCount)
		prepared = append(prepared, preparedDS{sp.name, injected, labels})
	}
	fmt.Println()

	// 对每个数据集进行分阶段搜索
	optimal := OptimalParams{}
	var optimalConfigs []cleaner.Config
	var optimalMetrics []eval.Metrics

	for i, p := range prepared {
		fmt.Printf("========== %s 数据集调优 ==========\n", p.name)
		bestConfig, bestMetrics, finalResults := stagedSearch(p.injected, p.labels, p.name)

		fmt.Printf("\n--- %s Top 5 参数组合 (最终阶段) ---\n", p.name)
		printTopN(finalResults, 5)

		fmt.Printf("\n[%s] 最优: F1=%.3f, RMSE=%.2fm, Retention=%.3f\n\n",
			p.name, bestMetrics.F1Score(), bestMetrics.RMSE, bestMetrics.RetentionRate)

		dr := DatasetResult{
			Config:  toConfigJSON(bestConfig),
			Metrics: toMetricsJSON(bestMetrics),
		}
		switch i {
		case 0:
			optimal.Seattle = dr
		case 1:
			optimal.GeoLife = dr
		case 2:
			optimal.Synthetic = dr
		}
		optimalConfigs = append(optimalConfigs, bestConfig)
		optimalMetrics = append(optimalMetrics, bestMetrics)
	}

	// 选择推荐参数: 用每个数据集的最优配置在所有数据集上评估，取平均分最高的
	fmt.Println("========== 选择推荐参数 ==========")
	recIdx := selectRecommended(prepared, optimalConfigs)
	recConfig := optimalConfigs[recIdx]
	recMetrics := optimalMetrics[recIdx]
	optimal.Recommended = DatasetResult{
		Config:  toConfigJSON(recConfig),
		Metrics: toMetricsJSON(recMetrics),
	}
	fmt.Printf("推荐配置来自 [%s]: F1=%.3f, RMSE=%.2fm, Retention=%.3f\n\n",
		specs[recIdx].name, recMetrics.F1Score(), recMetrics.RMSE, recMetrics.RetentionRate)

	writeOptimalParams(optimal)
	fmt.Println("调优完成。")
}

// ===== 分阶段网格搜索 =====

// stagedSearch 三阶段搜索
func stagedSearch(traj eval.Trajectory, labels []eval.AnomalyLabel, name string) (cleaner.Config, eval.Metrics, []ComboResult) {
	base := cleaner.DefaultConfig()

	// 阶段 1: anomalyMethod × (iqrMultiplier | continuousSigma) = 8 组
	fmt.Printf("  [阶段1] 搜索 anomalyMethod × threshold (8 组)\n")
	var stage1Combos []cleaner.Config
	iqrMults := []float64{1.0, 1.5, 2.0, 3.0}
	sigmaVals := []float64{1.5, 2.0, 2.5, 3.0}
	for _, m := range iqrMults {
		c := base
		c.AnomalyMethod = "iqr"
		c.IqrMultiplier = m
		stage1Combos = append(stage1Combos, c)
	}
	for _, s := range sigmaVals {
		c := base
		c.AnomalyMethod = "zscore"
		c.ContinuousSigma = s
		stage1Combos = append(stage1Combos, c)
	}
	stage1Results := evaluateCombos(traj, labels, name, stage1Combos, 1)
	best1 := stage1Results[0].Config
	printStageBest(stage1Results[0], "阶段1")

	// 阶段 2: staticDist × staticQueue = 12 组
	fmt.Printf("  [阶段2] 搜索 staticDist × staticQueue (12 组)\n")
	var stage2Combos []cleaner.Config
	staticDists := []float64{10, 15, 20, 30}
	staticQueues := []int{5, 10, 15}
	for _, d := range staticDists {
		for _, q := range staticQueues {
			c := best1
			c.StaticDistanceThreshold = d
			c.StaticQueueSize = q
			stage2Combos = append(stage2Combos, c)
		}
	}
	stage2Results := evaluateCombos(traj, labels, name, stage2Combos, 2)
	best2 := stage2Results[0].Config
	printStageBest(stage2Results[0], "阶段2")

	// 阶段 3: maxAccuracy × statsWindow = 12 组
	fmt.Printf("  [阶段3] 搜索 maxAccuracy × statsWindow (12 组)\n")
	var stage3Combos []cleaner.Config
	maxAccs := []float64{30, 50, 80, -1}
	statsWindows := []int{5, 10, 20}
	for _, a := range maxAccs {
		for _, w := range statsWindows {
			c := best2
			c.MaxAccuracy = a
			c.StatsWindowSize = w
			stage3Combos = append(stage3Combos, c)
		}
	}
	stage3Results := evaluateCombos(traj, labels, name, stage3Combos, 3)
	printStageBest(stage3Results[0], "阶段3")

	return stage3Results[0].Config, stage3Results[0].Metrics, stage3Results
}

// evaluateCombos 评估一组参数组合并计算综合评分
func evaluateCombos(traj eval.Trajectory, labels []eval.AnomalyLabel, name string, combos []cleaner.Config, stage int) []ComboResult {
	var results []ComboResult
	for _, cfg := range combos {
		m := eval.Evaluate(traj, labels, cfg, name)
		results = append(results, ComboResult{Config: cfg, Metrics: m, Stage: stage})
	}

	// 计算该阶段的最大 RMSE 用于归一化
	maxRMSE := 0.0
	for _, r := range results {
		if r.Metrics.RMSE > maxRMSE {
			maxRMSE = r.Metrics.RMSE
		}
	}
	if maxRMSE < 1.0 {
		maxRMSE = 1.0
	}

	// 综合评分: score = F1*0.4 + (1-RMSE/maxRMSE)*0.3 + Retention*0.3
	for i := range results {
		r := &results[i]
		rmseTerm := 1.0 - r.Metrics.RMSE/maxRMSE
		if rmseTerm < 0 {
			rmseTerm = 0
		}
		if rmseTerm > 1 {
			rmseTerm = 1
		}
		r.Score = r.Metrics.F1Score()*0.4 + rmseTerm*0.3 + r.Metrics.RetentionRate*0.3
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// selectRecommended 选择推荐配置: 用每个最优配置在所有数据集上评估，取平均分最高
func selectRecommended(prepared []preparedDS, configs []cleaner.Config) int {
	bestAvg := -1.0
	bestIdx := 0
	for ci, cfg := range configs {
		// 先收集所有 RMSE 用于归一化
		var metricsList []eval.Metrics
		var rmses []float64
		maxRMSE := 0.0
		for _, p := range prepared {
			m := eval.Evaluate(p.injected, p.labels, cfg, p.name)
			metricsList = append(metricsList, m)
			rmses = append(rmses, m.RMSE)
			if m.RMSE > maxRMSE {
				maxRMSE = m.RMSE
			}
		}
		if maxRMSE < 1.0 {
			maxRMSE = 1.0
		}
		var totalScore float64
		for i, m := range metricsList {
			rmseTerm := 1.0 - rmses[i]/maxRMSE
			if rmseTerm < 0 {
				rmseTerm = 0
			}
			score := m.F1Score()*0.4 + rmseTerm*0.3 + m.RetentionRate*0.3
			totalScore += score
		}
		avg := totalScore / float64(len(prepared))
		fmt.Printf("  配置[%d] (来自 %s) 平均分: %.4f\n", ci, prepared[ci].name, avg)
		if avg > bestAvg {
			bestAvg = avg
			bestIdx = ci
		}
	}
	return bestIdx
}

// ===== 轨迹生成 (与 gendata 一致的特征) =====

func genSeattleTraj() eval.Trajectory {
	n := 200
	lat, lon := 47.6205, -122.3493
	intervalMs := int64(3000)
	baseTs := int64(1700000000000)
	points := make([]cleaner.GPSPoint, n)
	heading := 30.0
	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID: "seattle_tune", Latitude: lat, Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs, Accuracy: 5.0 + randFloat()*5.0,
		}
		if i < n-1 {
			speed := 10.0 + randFloat()*10.0
			if randFloat() < 0.12 {
				heading += (randFloat() - 0.5) * 90
			}
			heading += (randFloat() - 0.5) * 4
			heading = normalizeHeading(heading)
			lat, lon = moveBy(lat, lon, heading, speed*3.0)
		}
	}
	return eval.Trajectory{DeviceID: "seattle_tune", Points: points}
}

func genGeolifeTraj() eval.Trajectory {
	n := 200
	lat, lon := 39.9847, 116.3606
	intervalMs := int64(5000)
	baseTs := int64(1700000000000)
	points := make([]cleaner.GPSPoint, n)
	heading := 90.0
	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID: "geolife_tune", Latitude: lat, Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs, Accuracy: 5.0 + randFloat()*5.0,
		}
		if i < n-1 {
			var speed float64
			if i < 60 {
				speed = 1.0 + randFloat()*0.5
				if randFloat() < 0.2 {
					heading += (randFloat() - 0.5) * 60
				}
			} else if i < 140 {
				speed = 8.0 + randFloat()*7.0
				if randFloat() < 0.08 {
					heading += (randFloat() - 0.5) * 60
				}
			} else {
				speed = 1.0 + randFloat()*0.5
				if randFloat() < 0.2 {
					heading += (randFloat() - 0.5) * 60
				}
			}
			heading += (randFloat() - 0.5) * 3
			heading = normalizeHeading(heading)
			lat, lon = moveBy(lat, lon, heading, speed*5.0)
		}
	}
	return eval.Trajectory{DeviceID: "geolife_tune", Points: points}
}

func genSyntheticTraj() eval.Trajectory {
	n := 200
	lat, lon := 40.0, 116.4
	intervalMs := int64(15000)
	baseTs := int64(1700000000000)
	points := make([]cleaner.GPSPoint, n)
	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID: "synthetic_tune", Latitude: lat, Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs, Accuracy: 5.0 + randFloat()*3.0,
		}
		if i < n-1 {
			lat, lon = moveBy(lat, lon, 0.0, 20.0*15.0)
		}
	}
	return eval.Trajectory{DeviceID: "synthetic_tune", Points: points}
}

// ===== 工具函数 =====

func moveBy(lat, lon, headingDeg, distance float64) (float64, float64) {
	h := headingDeg * math.Pi / 180.0
	dLat := distance * math.Cos(h) / 111320.0
	dLon := distance * math.Sin(h) / (111320.0 * math.Cos(lat*math.Pi/180.0))
	return lat + dLat, lon + dLon
}

func normalizeHeading(h float64) float64 {
	for h < 0 {
		h += 360
	}
	for h >= 360 {
		h -= 360
	}
	return h
}

func toConfigJSON(c cleaner.Config) ConfigJSON {
	return ConfigJSON{
		MaxAccuracy:             c.MaxAccuracy,
		StaticQueueSize:         c.StaticQueueSize,
		StaticDistanceThreshold: c.StaticDistanceThreshold,
		MotionConfirmCount:      c.MotionConfirmCount,
		StatsWindowSize:         c.StatsWindowSize,
		AnomalyMethod:           c.AnomalyMethod,
		InitialSigma:            c.InitialSigma,
		ContinuousSigma:         c.ContinuousSigma,
		IqrMultiplier:           c.IqrMultiplier,
		MaxVelocity:             c.MaxVelocity,
		AnomalyStrategy:         c.AnomalyStrategy,
	}
}

func toMetricsJSON(m eval.Metrics) MetricsJSON {
	return MetricsJSON{
		F1: m.F1Score(), Precision: m.Precision(), Recall: m.Recall(),
		RMSE: m.RMSE, MeanError: m.MeanError, MaxError: m.MaxError, P95Error: m.P95Error,
		Retention: m.RetentionRate, TruePositives: m.TruePositives,
		FalsePositives: m.FalsePositives, FalseNegatives: m.FalseNegatives,
		StaticRecall: m.StaticRecall(),
	}
}

// printTopN 打印 Top N 结果
func printTopN(results []ComboResult, n int) {
	if n > len(results) {
		n = len(results)
	}
	fmt.Printf("%-4s %-8s %-6s %-6s %-6s %-6s %-6s %-6s  %s\n",
		"Rank", "Method", "IQR", "Sigma", "SWin", "Acc", "SDist", "SQueue", "Score")
	for i := 0; i < n; i++ {
		r := results[i]
		fmt.Printf("%-4d %-8s %-6.1f %-6.1f %-6d %-6.0f %-6.0f %-6d  %.4f (F1=%.3f RMSE=%.1f Ret=%.3f)\n",
			i+1, r.Config.AnomalyMethod, r.Config.IqrMultiplier, r.Config.ContinuousSigma,
			r.Config.StatsWindowSize, r.Config.MaxAccuracy, r.Config.StaticDistanceThreshold,
			r.Config.StaticQueueSize, r.Score, r.Metrics.F1Score(), r.Metrics.RMSE, r.Metrics.RetentionRate)
	}
}

func printStageBest(r ComboResult, stageName string) {
	fmt.Printf("  -> %s 最优: score=%.4f, F1=%.3f, RMSE=%.2fm, Retention=%.3f\n",
		stageName, r.Score, r.Metrics.F1Score(), r.Metrics.RMSE, r.Metrics.RetentionRate)
}

func writeOptimalParams(optimal OptimalParams) {
	data, err := json.MarshalIndent(optimal, "", "  ")
	if err != nil {
		fmt.Printf("序列化失败: %v\n", err)
		os.Exit(1)
	}
	path := dataDir + "/optimal_params.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Printf("写文件失败 %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("最优参数已写入: %s (%d bytes)\n", path, len(data))
}
