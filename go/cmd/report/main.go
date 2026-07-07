// Package main: 验证 HTML 报告生成器
// 读取调优结果，生成多维度数据验证报告 (ECharts 可视化)
// 输出: ../reports/validation-report.html
package main

import (
	"cleaner"
	"cleaner/eval"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ===== JSON 结构 (与 tuning 输出一致) =====

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

// ===== 报告数据结构 =====

type PointData struct {
	Index     int     `json:"index"`
	RawLat    float64 `json:"rawLat"`
	RawLon    float64 `json:"rawLon"`
	CleanLat  float64 `json:"cleanLat"`
	CleanLon  float64 `json:"cleanLon"`
	HasOutput bool    `json:"hasOutput"`
	Action    string  `json:"action"`
	ErrorM    float64 `json:"errorM"`
	IsAnomaly bool    `json:"isAnomaly"`
	IsStatic  bool    `json:"isStatic"`
	Accuracy  float64 `json:"accuracy"`
}

type DatasetReport struct {
	Name           string      `json:"name"`
	DisplayName    string      `json:"displayName"`
	Description    string      `json:"description"`
	Points         []PointData `json:"points"`
	DefaultMetrics MetricsJSON `json:"defaultMetrics"`
	OptimalMetrics MetricsJSON `json:"optimalMetrics"`
	DefaultConfig  ConfigJSON  `json:"defaultConfig"`
	OptimalConfig  ConfigJSON  `json:"optimalConfig"`
}

type SensitivitySeries struct {
	Param    string    `json:"param"`
	Labels   []string  `json:"labels"`
	F1Scores []float64 `json:"f1Scores"`
	RMSEs    []float64 `json:"rmses"`
}

type ReportData struct {
	Datasets    []DatasetReport    `json:"datasets"`
	Sensitivity []SensitivitySeries `json:"sensitivity"`
}

// ===== 常量 =====

var randState uint64 = 42

func randFloat() float64 {
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / float64(1<<53)
}

const dataDir = "../data"
const reportDir = "../reports"

func main() {
	fmt.Println("=== 生成验证 HTML 报告 ===")

	// 1. 读取调优结果
	optimal := readOptimalParams()

	// 2. 确保报告目录存在
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("创建报告目录失败: %v\n", err)
		os.Exit(1)
	}

	// 3. 生成 3 个数据集并处理
	var reports []DatasetReport

	// Seattle
	seattleTraj := genSeattleTraj()
	seattleInjected, seattleLabels := eval.InjectAnomalies(seattleTraj, 0.05, 0.03)
	reports = append(reports, processDataset("seattle", "Seattle (西雅图驾驶)",
		"200点, 3s间隔, 城市道路 10-20 m/s", seattleInjected, seattleLabels,
		optimal.Seattle.Config))

	// GeoLife
	geolifeTraj := genGeolifeTraj()
	geolifeInjected, geolifeLabels := eval.InjectAnomalies(geolifeTraj, 0.06, 0.05)
	reports = append(reports, processDataset("geolife", "GeoLife (北京混合出行)",
		"200点, 5s间隔, 步行+驾车混合", geolifeInjected, geolifeLabels,
		optimal.GeoLife.Config))

	// Synthetic
	synthTraj := genSyntheticTraj()
	synthInjected, synthLabels := eval.InjectAnomalies(synthTraj, 0.04, 0.02)
	reports = append(reports, processDataset("synthetic", "Synthetic (合成正常轨迹)",
		"200点, 15s间隔, 匀速 20 m/s 直线", synthInjected, synthLabels,
		optimal.Synthetic.Config))

	// 4. 参数敏感性分析 (用 synthetic 数据集)
	sensitivity := runSensitivity(synthInjected, synthLabels)

	// 5. 构建报告数据
	reportData := ReportData{
		Datasets:    reports,
		Sensitivity: sensitivity,
	}

	// 6. 渲染 HTML
	renderHTML(reportData, optimal)
	fmt.Println("报告生成完成。")
}

// ===== 核心处理 =====

// processDataset 处理单个数据集: 用默认和最优配置分别运行，收集逐点数据
func processDataset(name, displayName, description string, injected eval.Trajectory,
	labels []eval.AnomalyLabel, optimalCfg ConfigJSON) DatasetReport {

	defaultConfig := cleaner.DefaultConfig()
	optimalConfig := fromConfigJSON(optimalCfg)

	// 用最优配置运行，收集逐点数据
	cleanerInst := cleaner.New(optimalConfig)
	points := make([]PointData, len(injected.Points))

	for i := 0; i < len(injected.Points); i++ {
		pt := injected.Points[i]
		result := cleanerInst.Clean(pt)

		pd := PointData{
			Index:     i,
			RawLat:    pt.Latitude,
			RawLon:    pt.Longitude,
			Accuracy:  pt.Accuracy,
			IsAnomaly: labels[i].IsAnomaly,
			IsStatic:  labels[i].IsDropped,
		}

		if result.HasOutput() {
			out := *result.OutputPoint
			pd.CleanLat = out.Latitude
			pd.CleanLon = out.Longitude
			pd.HasOutput = true
			pd.ErrorM = haversine(labels[i].OriginalLat, labels[i].OriginalLon,
				out.Latitude, out.Longitude)
		} else {
			pd.CleanLat = pt.Latitude
			pd.CleanLon = pt.Longitude
			pd.HasOutput = false
			pd.ErrorM = haversine(labels[i].OriginalLat, labels[i].OriginalLon,
				pt.Latitude, pt.Longitude)
		}
		pd.Action = result.Action.String()
		points[i] = pd
	}

	// 计算默认和最优的聚合指标
	defaultMetrics := eval.Evaluate(injected, labels, defaultConfig, name)
	optimalMetrics := eval.Evaluate(injected, labels, optimalConfig, name)

	return DatasetReport{
		Name:           name,
		DisplayName:    displayName,
		Description:    description,
		Points:         points,
		DefaultMetrics: toMetricsJSON(defaultMetrics),
		OptimalMetrics: toMetricsJSON(optimalMetrics),
		DefaultConfig:  toConfigJSON(defaultConfig),
		OptimalConfig:  optimalCfg,
	}
}

// runSensitivity 参数敏感性分析: 逐个变化关键参数，测量 F1 和 RMSE
func runSensitivity(traj eval.Trajectory, labels []eval.AnomalyLabel) []SensitivitySeries {
	base := cleaner.DefaultConfig()
	base.StatsWindowSize = 10

	var series []SensitivitySeries

	// 1. IqrMultiplier 敏感性
	iqrMults := []float64{1.0, 1.5, 2.0, 2.5, 3.0}
	s1 := SensitivitySeries{Param: "IQR 乘数"}
	for _, m := range iqrMults {
		c := base
		c.AnomalyMethod = "iqr"
		c.IqrMultiplier = m
		metrics := eval.Evaluate(traj, labels, c, "sensitivity")
		s1.Labels = append(s1.Labels, fmt.Sprintf("%.1f", m))
		s1.F1Scores = append(s1.F1Scores, metrics.F1Score())
		s1.RMSEs = append(s1.RMSEs, metrics.RMSE)
	}
	series = append(series, s1)

	// 2. ContinuousSigma 敏感性
	sigmas := []float64{1.5, 2.0, 2.5, 3.0, 3.5}
	s2 := SensitivitySeries{Param: "Z-score σ"}
	for _, s := range sigmas {
		c := base
		c.AnomalyMethod = "zscore"
		c.ContinuousSigma = s
		metrics := eval.Evaluate(traj, labels, c, "sensitivity")
		s2.Labels = append(s2.Labels, fmt.Sprintf("%.1f", s))
		s2.F1Scores = append(s2.F1Scores, metrics.F1Score())
		s2.RMSEs = append(s2.RMSEs, metrics.RMSE)
	}
	series = append(series, s2)

	// 3. StaticDistanceThreshold 敏感性
	dists := []float64{5, 10, 15, 20, 25, 30}
	s3 := SensitivitySeries{Param: "静止距离阈值 (m)"}
	for _, d := range dists {
		c := base
		c.StaticDistanceThreshold = d
		metrics := eval.Evaluate(traj, labels, c, "sensitivity")
		s3.Labels = append(s3.Labels, fmt.Sprintf("%.0f", d))
		s3.F1Scores = append(s3.F1Scores, metrics.F1Score())
		s3.RMSEs = append(s3.RMSEs, metrics.RMSE)
	}
	series = append(series, s3)

	// 4. StatsWindowSize 敏感性
	windows := []int{5, 10, 15, 20, 25, 30}
	s4 := SensitivitySeries{Param: "统计窗口大小"}
	for _, w := range windows {
		c := base
		c.StatsWindowSize = w
		metrics := eval.Evaluate(traj, labels, c, "sensitivity")
		s4.Labels = append(s4.Labels, fmt.Sprintf("%d", w))
		s4.F1Scores = append(s4.F1Scores, metrics.F1Score())
		s4.RMSEs = append(s4.RMSEs, metrics.RMSE)
	}
	series = append(series, s4)

	// 5. MaxAccuracy 敏感性
	accs := []float64{-1, 20, 30, 50, 80, 100}
	s5 := SensitivitySeries{Param: "精度阈值 (m)"}
	for _, a := range accs {
		c := base
		c.MaxAccuracy = a
		metrics := eval.Evaluate(traj, labels, c, "sensitivity")
		if a < 0 {
			s5.Labels = append(s5.Labels, "禁用")
		} else {
			s5.Labels = append(s5.Labels, fmt.Sprintf("%.0f", a))
		}
		s5.F1Scores = append(s5.F1Scores, metrics.F1Score())
		s5.RMSEs = append(s5.RMSEs, metrics.RMSE)
	}
	series = append(series, s5)

	return series
}

// ===== 轨迹生成 (与 tuning 一致) =====

func genSeattleTraj() eval.Trajectory {
	n := 200
	lat, lon := 47.6205, -122.3493
	intervalMs := int64(3000)
	baseTs := int64(1700000000000)
	points := make([]cleaner.GPSPoint, n)
	heading := 30.0
	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID: "seattle_rep", Latitude: lat, Longitude: lon,
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
	return eval.Trajectory{DeviceID: "seattle_rep", Points: points}
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
			DeviceID: "geolife_rep", Latitude: lat, Longitude: lon,
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
	return eval.Trajectory{DeviceID: "geolife_rep", Points: points}
}

func genSyntheticTraj() eval.Trajectory {
	n := 200
	lat, lon := 40.0, 116.4
	intervalMs := int64(15000)
	baseTs := int64(1700000000000)
	points := make([]cleaner.GPSPoint, n)
	for i := 0; i < n; i++ {
		points[i] = cleaner.GPSPoint{
			DeviceID: "synthetic_rep", Latitude: lat, Longitude: lon,
			Timestamp: baseTs + int64(i)*intervalMs, Accuracy: 5.0 + randFloat()*3.0,
		}
		if i < n-1 {
			lat, lon = moveBy(lat, lon, 0.0, 20.0*15.0)
		}
	}
	return eval.Trajectory{DeviceID: "synthetic_rep", Points: points}
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

func fromConfigJSON(c ConfigJSON) cleaner.Config {
	return cleaner.Config{
		MaxAccuracy:             c.MaxAccuracy,
		StaticQueueSize:         c.StaticQueueSize,
		StaticDistanceThreshold: c.StaticDistanceThreshold,
		MotionConfirmCount:      c.MotionConfirmCount,
		StatsWindowSize:         c.StatsWindowSize,
		AnomalyMethod:           c.AnomalyMethod,
		InitialSigma:            c.InitialSigma,
		ContinuousSigma:          c.ContinuousSigma,
		IqrMultiplier:           c.IqrMultiplier,
		MaxVelocity:             c.MaxVelocity,
		AnomalyStrategy:         c.AnomalyStrategy,
	}
}

func toConfigJSON(c cleaner.Config) ConfigJSON {
	return ConfigJSON{
		MaxAccuracy: c.MaxAccuracy, StaticQueueSize: c.StaticQueueSize,
		StaticDistanceThreshold: c.StaticDistanceThreshold, MotionConfirmCount: c.MotionConfirmCount,
		StatsWindowSize: c.StatsWindowSize, AnomalyMethod: c.AnomalyMethod,
		InitialSigma: c.InitialSigma, ContinuousSigma: c.ContinuousSigma,
		IqrMultiplier: c.IqrMultiplier, MaxVelocity: c.MaxVelocity,
		AnomalyStrategy: c.AnomalyStrategy,
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

// readOptimalParams 读取调优结果
func readOptimalParams() OptimalParams {
	path := filepath.Join(dataDir, "optimal_params.json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("读取调优结果失败 %s: %v\n", path, err)
		fmt.Println("请先运行 cmd/tuning/main.go")
		os.Exit(1)
	}
	var opt OptimalParams
	if err := json.Unmarshal(data, &opt); err != nil {
		fmt.Printf("解析调优结果失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已读取调优结果: %s\n", path)
	return opt
}

// renderHTML 渲染 HTML 报告
func renderHTML(data ReportData, optimal OptimalParams) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("序列化报告数据失败: %v\n", err)
		os.Exit(1)
	}

	// 生成概览卡片 HTML
	overviewHTML := buildOverviewHTML(data)
	// 生成参数对比表 HTML
	paramTableHTML := buildParamTableHTML(data)
	// 生成图表容器 HTML
	chartsHTML := buildChartsHTML(data)

	html := htmlTemplate
	html = strings.Replace(html, "{{OVERVIEW_CARDS}}", overviewHTML, 1)
	html = strings.Replace(html, "{{PARAM_TABLE}}", paramTableHTML, 1)
	html = strings.Replace(html, "{{CHARTS}}", chartsHTML, 1)
	html = strings.Replace(html, "__REPORT_DATA__", string(jsonData), 1)

	path := filepath.Join(reportDir, "validation-report.html")
	if err := os.WriteFile(path, []byte(html), 0644); err != nil {
		fmt.Printf("写 HTML 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("HTML 报告已生成: %s (%d bytes)\n", path, len(html))
}

// buildOverviewHTML 构建概览卡片
func buildOverviewHTML(data ReportData) string {
	var sb strings.Builder
	for _, ds := range data.Datasets {
		dm := ds.DefaultMetrics
		om := ds.OptimalMetrics
		f1Delta := om.F1 - dm.F1
		rmseDelta := om.RMSE - dm.RMSE
		retDelta := om.Retention - dm.Retention
		sb.WriteString(fmt.Sprintf(`
    <div class="card overview-card">
      <div class="card-header">%s</div>
      <div class="card-desc">%s</div>
      <div class="metric-grid">
        <div class="metric-box">
          <div class="metric-label">F1 Score</div>
          <div class="metric-row"><span class="tag tag-default">默认</span><span class="metric-val">%.3f</span></div>
          <div class="metric-row"><span class="tag tag-optimal">最优</span><span class="metric-val">%.3f</span></div>
          <div class="metric-delta %s">Δ %s%.3f</div>
        </div>
        <div class="metric-box">
          <div class="metric-label">RMSE (m)</div>
          <div class="metric-row"><span class="tag tag-default">默认</span><span class="metric-val">%.1f</span></div>
          <div class="metric-row"><span class="tag tag-optimal">最优</span><span class="metric-val">%.1f</span></div>
          <div class="metric-delta %s">Δ %s%.1f</div>
        </div>
        <div class="metric-box">
          <div class="metric-label">保留率</div>
          <div class="metric-row"><span class="tag tag-default">默认</span><span class="metric-val">%.3f</span></div>
          <div class="metric-row"><span class="tag tag-optimal">最优</span><span class="metric-val">%.3f</span></div>
          <div class="metric-delta %s">Δ %s%.3f</div>
        </div>
      </div>
    </div>`,
			ds.DisplayName, ds.Description,
			dm.F1, om.F1, deltaClass(f1Delta), deltaSign(f1Delta), math.Abs(f1Delta),
			dm.RMSE, om.RMSE, deltaClass(-rmseDelta), deltaSign(-rmseDelta), math.Abs(rmseDelta),
			dm.Retention, om.Retention, deltaClass(retDelta), deltaSign(retDelta), math.Abs(retDelta),
		))
	}
	return sb.String()
}

func deltaClass(delta float64) string {
	if delta >= 0 {
		return "delta-positive"
	}
	return "delta-negative"
}

func deltaSign(delta float64) string {
	if delta >= 0 {
		return "+"
	}
	return ""
}

// buildParamTableHTML 构建参数对比表
func buildParamTableHTML(data ReportData) string {
	params := []struct{ jsonName, label string }{
		{"maxAccuracy", "精度阈值"},
		{"staticQueueSize", "静止队列"},
		{"staticDistanceThreshold", "静止距离"},
		{"statsWindowSize", "统计窗口"},
		{"anomalyMethod", "检测方法"},
		{"iqrMultiplier", "IQR乘数"},
		{"continuousSigma", "连续σ"},
		{"maxVelocity", "速度上限"},
		{"anomalyStrategy", "异常策略"},
	}

	var sb strings.Builder
	sb.WriteString(`
    <table class="param-table">
      <thead>
        <tr>
          <th>参数</th>`)
	for _, ds := range data.Datasets {
		sb.WriteString(fmt.Sprintf(`<th>%s 默认</th><th>%s 最优</th>`, ds.Name, ds.Name))
	}
	sb.WriteString(`</tr>
      </thead>
      <tbody>`)

	for _, p := range params {
		sb.WriteString(fmt.Sprintf(`<tr><td class="param-name">%s</td>`, p.label))
		for _, ds := range data.Datasets {
			dv := getConfigField(ds.DefaultConfig, p.jsonName)
			ov := getConfigField(ds.OptimalConfig, p.jsonName)
			diffClass := ""
			if dv != ov {
				diffClass = " param-changed"
			}
			sb.WriteString(fmt.Sprintf(`<td>%s</td><td class="param-optimal%s">%s</td>`, dv, diffClass, ov))
		}
		sb.WriteString(`</tr>`)
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

func getConfigField(c ConfigJSON, field string) string {
	switch field {
	case "maxAccuracy":
		if c.MaxAccuracy < 0 {
			return "禁用"
		}
		return fmt.Sprintf("%.0f", c.MaxAccuracy)
	case "staticQueueSize":
		return fmt.Sprintf("%d", c.StaticQueueSize)
	case "staticDistanceThreshold":
		return fmt.Sprintf("%.0f", c.StaticDistanceThreshold)
	case "statsWindowSize":
		return fmt.Sprintf("%d", c.StatsWindowSize)
	case "anomalyMethod":
		return c.AnomalyMethod
	case "iqrMultiplier":
		return fmt.Sprintf("%.1f", c.IqrMultiplier)
	case "continuousSigma":
		return fmt.Sprintf("%.1f", c.ContinuousSigma)
	case "maxVelocity":
		return fmt.Sprintf("%.2f", c.MaxVelocity)
	case "anomalyStrategy":
		return c.AnomalyStrategy
	}
	return ""
}

// buildChartsHTML 构建图表容器
func buildChartsHTML(data ReportData) string {
	var sb strings.Builder
	for _, ds := range data.Datasets {
		sb.WriteString(fmt.Sprintf(`
    <div class="chart-group" id="group-%s">
      <h3 class="chart-group-title">%s</h3>
      <p class="chart-group-desc">%s</p>
      <div class="chart-row">
        <div class="chart-container">
          <div class="chart-title">轨迹散点图 (原始 vs 清洗)</div>
          <div id="scatter-%s" class="chart"></div>
        </div>
        <div class="chart-container">
          <div class="chart-title">误差分布 (逐点)</div>
          <div id="error-%s" class="chart"></div>
        </div>
      </div>
      <div class="chart-row">
        <div class="chart-container">
          <div class="chart-title">逐点处理时间线 (Action 着色)</div>
          <div id="timeline-%s" class="chart"></div>
        </div>
        <div class="chart-container">
          <div class="chart-title">Action 分布统计</div>
          <div id="action-pie-%s" class="chart"></div>
        </div>
      </div>
    </div>`, ds.Name, ds.DisplayName, ds.Description, ds.Name, ds.Name, ds.Name, ds.Name))
	}
	return sb.String()
}

// ===== HTML 模板 =====

const htmlTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>轨迹清洗 SDK 验证报告</title>
<script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
<style>
  :root {
    --bg: #0d1117;
    --bg-card: #161b22;
    --bg-hover: #1c2128;
    --border: #30363d;
    --text: #c9d1d9;
    --text-muted: #8b949e;
    --text-bright: #f0f6fc;
    --blue: #58a6ff;
    --green: #3fb950;
    --red: #f85149;
    --yellow: #d29922;
    --orange: #db6d28;
    --purple: #bc8cff;
    --cyan: #39c5cf;
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    background: var(--bg);
    color: var(--text);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
    line-height: 1.6;
    padding: 24px;
  }
  .header {
    text-align: center;
    padding: 32px 0 24px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 32px;
  }
  .header h1 {
    font-size: 28px;
    color: var(--text-bright);
    font-weight: 600;
  }
  .header p {
    color: var(--text-muted);
    margin-top: 8px;
    font-size: 14px;
  }
  .section {
    margin-bottom: 40px;
  }
  .section-title {
    font-size: 20px;
    color: var(--text-bright);
    margin-bottom: 16px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border);
  }
  .overview-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 16px;
  }
  .card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 20px;
  }
  .card-header {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-bright);
    margin-bottom: 4px;
  }
  .card-desc {
    font-size: 12px;
    color: var(--text-muted);
    margin-bottom: 16px;
  }
  .metric-grid {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 12px;
  }
  .metric-box {
    text-align: center;
  }
  .metric-label {
    font-size: 12px;
    color: var(--text-muted);
    margin-bottom: 8px;
  }
  .metric-row {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    margin-bottom: 4px;
  }
  .metric-val {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-bright);
  }
  .tag {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 4px;
  }
  .tag-default { background: #30363d; color: var(--text-muted); }
  .tag-optimal { background: #1a3a2e; color: var(--green); }
  .metric-delta {
    font-size: 11px;
    margin-top: 4px;
  }
  .delta-positive { color: var(--green); }
  .delta-negative { color: var(--red); }
  .param-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  .param-table th {
    background: var(--bg-hover);
    color: var(--text-muted);
    padding: 10px 12px;
    text-align: left;
    border-bottom: 1px solid var(--border);
    font-weight: 600;
  }
  .param-table td {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
  }
  .param-table tr:hover { background: var(--bg-hover); }
  .param-name { color: var(--blue); font-weight: 500; }
  .param-optimal { color: var(--green); }
  .param-changed { font-weight: 600; }
  .chart-group {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 20px;
    margin-bottom: 24px;
  }
  .chart-group-title {
    font-size: 18px;
    color: var(--text-bright);
    margin-bottom: 4px;
  }
  .chart-group-desc {
    font-size: 13px;
    color: var(--text-muted);
    margin-bottom: 16px;
  }
  .chart-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
    margin-bottom: 16px;
  }
  .chart-container {
    background: var(--bg);
    border-radius: 6px;
    padding: 12px;
  }
  .chart-title {
    font-size: 13px;
    color: var(--text-muted);
    margin-bottom: 8px;
  }
  .chart {
    width: 100%;
    height: 350px;
  }
  .sensitivity-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
  }
  @media (max-width: 1200px) {
    .overview-grid { grid-template-columns: 1fr; }
    .chart-row { grid-template-columns: 1fr; }
    .sensitivity-grid { grid-template-columns: 1fr; }
  }
</style>
</head>
<body>
<div class="header">
  <h1>Trajectory Cleaner SDK - 多维度验证报告</h1>
  <p>3 个数据集 | 默认参数 vs 最优参数对比 | ECharts 可视化</p>
</div>

<div class="section">
  <h2 class="section-title">概览: 关键指标对比</h2>
  <div class="overview-grid">
    {{OVERVIEW_CARDS}}
  </div>
</div>

<div class="section">
  <h2 class="section-title">参数对比表 (默认 vs 最优)</h2>
  {{PARAM_TABLE}}
</div>

<div class="section">
  <h2 class="section-title">轨迹散点图 / 误差分布 / 处理时间线</h2>
  {{CHARTS}}
</div>

<div class="section">
  <h2 class="section-title">参数敏感性分析</h2>
  <div class="sensitivity-grid">
    <div class="chart-container"><div class="chart-title">F1 随参数变化</div><div id="sensitivity-f1" class="chart"></div></div>
    <div class="chart-container"><div class="chart-title">RMSE 随参数变化</div><div id="sensitivity-rmse" class="chart"></div></div>
  </div>
</div>

<script>
const REPORT_DATA = __REPORT_DATA__;

// Action 颜色映射
const ACTION_COLORS = {
  'PASSTHROUGH': '#3fb950',
  'DROPPED_ACCURACY': '#f85149',
  'DROPPED_STATIC': '#d29922',
  'REPLACED_ANOMALY': '#db6d28',
  'DROPPED_ANOMALY': '#f85149',
  'STATE_TRANSITION': '#58a6ff'
};

const CHART_BG = '#0d1117';
const AXIS_COLOR = '#8b949e';
const SPLIT_COLOR = '#21262d';

const commonGrid = { left: '8%', right: '5%', top: '10%', bottom: '12%' };
const commonAxis = {
  axisLine: { lineStyle: { color: AXIS_COLOR } },
  axisLabel: { color: AXIS_COLOR, fontSize: 10 },
  splitLine: { lineStyle: { color: SPLIT_COLOR } }
};
const commonTooltip = {
  trigger: 'item',
  backgroundColor: '#161b22',
  borderColor: '#30363d',
  textStyle: { color: '#c9d1d9' }
};

function renderScatter(ds) {
  const el = document.getElementById('scatter-' + ds.name);
  if (!el) return;
  const chart = echarts.init(el);
  const rawData = [], cleanData = [], anomalyData = [];
  ds.points.forEach(p => {
    rawData.push([p.rawLon, p.rawLat]);
    cleanData.push([p.cleanLon, p.cleanLat]);
    if (p.isAnomaly || p.isStatic) {
      anomalyData.push([p.rawLon, p.rawLat]);
    }
  });
  chart.setOption({
    backgroundColor: CHART_BG,
    tooltip: { ...commonTooltip, formatter: p => 'lon: ' + p.data[0].toFixed(5) + '<br/>lat: ' + p.data[1].toFixed(5) },
    legend: { textStyle: { color: AXIS_COLOR }, top: 0 },
    grid: commonGrid,
    xAxis: { ...commonAxis, type: 'value', name: 'Longitude', nameTextStyle: { color: AXIS_COLOR } },
    yAxis: { ...commonAxis, type: 'value', name: 'Latitude', nameTextStyle: { color: AXIS_COLOR } },
    series: [
      { name: '原始', type: 'scatter', data: rawData, symbolSize: 4, itemStyle: { color: '#f85149', opacity: 0.5 } },
      { name: '清洗后', type: 'scatter', data: cleanData, symbolSize: 4, itemStyle: { color: '#3fb950', opacity: 0.7 } },
      { name: '异常点', type: 'scatter', data: anomalyData, symbolSize: 8, itemStyle: { color: '#d29922' } }
    ]
  });
  return chart;
}

function renderError(ds) {
  const el = document.getElementById('error-' + ds.name);
  if (!el) return;
  const chart = echarts.init(el);
  const data = ds.points.map(p => ({
    value: [p.index, p.errorM],
    itemStyle: { color: ACTION_COLORS[p.action] || '#8b949e' }
  }));
  chart.setOption({
    backgroundColor: CHART_BG,
    tooltip: { ...commonTooltip, formatter: p => 'idx: ' + p.data[0] + '<br/>error: ' + p.data[1].toFixed(1) + 'm<br/>action: ' + ds.points[p.data[0]].action },
    grid: commonGrid,
    xAxis: { ...commonAxis, type: 'value', name: 'Point Index', nameTextStyle: { color: AXIS_COLOR } },
    yAxis: { ...commonAxis, type: 'value', name: 'Error (m)', nameTextStyle: { color: AXIS_COLOR } },
    series: [{ type: 'bar', data: data, barWidth: '90%' }]
  });
  return chart;
}

function renderTimeline(ds) {
  const el = document.getElementById('timeline-' + ds.name);
  if (!el) return;
  const chart = echarts.init(el);
  const series = {};
  ds.points.forEach(p => {
    if (!series[p.action]) series[p.action] = [];
    series[p.action].push([p.index, p.errorM]);
  });
  const seriesList = Object.keys(series).map(action => ({
    name: action,
    type: 'scatter',
    data: series[action],
    symbolSize: 6,
    itemStyle: { color: ACTION_COLORS[action] || '#8b949e' }
  }));
  chart.setOption({
    backgroundColor: CHART_BG,
    tooltip: { ...commonTooltip, formatter: p => 'idx: ' + p.data[0] + '<br/>error: ' + p.data[1].toFixed(1) + 'm' },
    legend: { textStyle: { color: AXIS_COLOR }, top: 0, type: 'scroll' },
    grid: { ...commonGrid, top: '15%' },
    xAxis: { ...commonAxis, type: 'value', name: 'Point Index', nameTextStyle: { color: AXIS_COLOR } },
    yAxis: { ...commonAxis, type: 'value', name: 'Error (m)', nameTextStyle: { color: AXIS_COLOR } },
    series: seriesList
  });
  return chart;
}

function renderActionPie(ds) {
  const el = document.getElementById('action-pie-' + ds.name);
  if (!el) return;
  const chart = echarts.init(el);
  const counts = {};
  ds.points.forEach(p => { counts[p.action] = (counts[p.action] || 0) + 1; });
  const data = Object.keys(counts).map(k => ({
    name: k, value: counts[k], itemStyle: { color: ACTION_COLORS[k] || '#8b949e' }
  }));
  chart.setOption({
    backgroundColor: CHART_BG,
    tooltip: { ...commonTooltip },
    legend: { textStyle: { color: AXIS_COLOR }, bottom: 0, type: 'scroll' },
    series: [{
      type: 'pie', radius: ['35%', '65%'], center: ['50%', '45%'],
      data: data, label: { color: AXIS_COLOR, fontSize: 10 }
    }]
  });
  return chart;
}

function renderSensitivity() {
  const sens = REPORT_DATA.sensitivity;
  const colors = ['#58a6ff', '#3fb950', '#d29922', '#bc8cff', '#f85149'];

  // F1 chart
  const f1El = document.getElementById('sensitivity-f1');
  if (f1El) {
    const chart = echarts.init(f1El);
    const series = sens.map((s, i) => ({
      name: s.param, type: 'line', data: s.f1Scores,
      lineStyle: { width: 2 }, itemStyle: { color: colors[i % colors.length] },
      symbol: 'circle', symbolSize: 8
    }));
    chart.setOption({
      backgroundColor: CHART_BG,
      tooltip: { trigger: 'axis', backgroundColor: '#161b22', borderColor: '#30363d', textStyle: { color: '#c9d1d9' } },
      legend: { textStyle: { color: AXIS_COLOR }, top: 0, type: 'scroll' },
      grid: { ...commonGrid, top: '15%' },
      xAxis: { ...commonAxis, type: 'category', data: sens[0].labels },
      yAxis: { ...commonAxis, type: 'value', name: 'F1 Score', nameTextStyle: { color: AXIS_COLOR } },
      series: series
    });
  }

  // RMSE chart
  const rmseEl = document.getElementById('sensitivity-rmse');
  if (rmseEl) {
    const chart = echarts.init(rmseEl);
    const series = sens.map((s, i) => ({
      name: s.param, type: 'line', data: s.rmses,
      lineStyle: { width: 2 }, itemStyle: { color: colors[i % colors.length] },
      symbol: 'circle', symbolSize: 8
    }));
    chart.setOption({
      backgroundColor: CHART_BG,
      tooltip: { trigger: 'axis', backgroundColor: '#161b22', borderColor: '#30363d', textStyle: { color: '#c9d1d9' } },
      legend: { textStyle: { color: AXIS_COLOR }, top: 0, type: 'scroll' },
      grid: { ...commonGrid, top: '15%' },
      xAxis: { ...commonAxis, type: 'category', data: sens[0].labels },
      yAxis: { ...commonAxis, type: 'value', name: 'RMSE (m)', nameTextStyle: { color: AXIS_COLOR } },
      series: series
    });
  }
}

// 渲染所有图表
const charts = [];
REPORT_DATA.datasets.forEach(ds => {
  charts.push(renderScatter(ds));
  charts.push(renderError(ds));
  charts.push(renderTimeline(ds));
  charts.push(renderActionPie(ds));
});
renderSensitivity();

// 响应式
window.addEventListener('resize', () => {
  charts.forEach(c => c && c.resize());
});
</script>
</body>
</html>`
