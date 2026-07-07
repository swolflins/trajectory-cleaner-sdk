package cleaner

// Config 清洗 SDK 参数配置
type Config struct {
	// ===== Stage 1: 精度过滤 =====
	// 定位精度阈值(米)，超过此值的点直接丢弃。<=0 表示不检查
	MaxAccuracy float64

	// ===== Stage 2: 伪静止状态机 =====
	// 伪静止判断队列长度 N
	StaticQueueSize int
	// 静止判定距离阈值 R (米)
	StaticDistanceThreshold float64
	// 静止→运动的确认计数（连续N次距离>R才判定启动）
	MotionConfirmCount int

	// ===== Stage 3: 异常检测 =====
	// 统计窗口大小
	StatsWindowSize int
	// 异常检测方法: "zscore" 或 "iqr"
	AnomalyMethod string
	// 首次校准置信度 (Z-score σ 倍数，如 3.0)
	InitialSigma float64
	// 持续检测置信度 (Z-score σ 倍数，如 2.0)
	ContinuousSigma float64
	// IQR 乘数因子 (如 1.5)
	IqrMultiplier float64
	// 速度上限 (m/s)，超过判异常
	MaxVelocity float64

	// ===== Stage 4: 异常处理 =====
	// "replace" 用上一有效值替代, "drop" 丢弃
	AnomalyStrategy string
}

// DefaultConfig 返回针对 15s 上报间隔物流场景的默认配置
func DefaultConfig() Config {
	return Config{
		MaxAccuracy:             50.0,    // 50 米
		StaticQueueSize:         10,      // 队列 T 长度
		StaticDistanceThreshold: 15.0,    // R = 15m
		MotionConfirmCount:      3,       // 连续 3 次确认启动
		StatsWindowSize:         10,      // 队列 L 大小
		AnomalyMethod:           "iqr",   // IQR 更鲁棒
		InitialSigma:            3.0,     // 首次 3σ
		ContinuousSigma:         2.0,     // 持续 2σ
		IqrMultiplier:           1.5,     // 标准 IQR 乘数
		MaxVelocity:             41.67,   // 150 km/h = 41.67 m/s
		AnomalyStrategy:         "replace",
	}
}

// ZScoreConfig 切换为 Z-score 模式（对应图片方案 2.2.2 方案一）
func ZScoreConfig() Config {
	c := DefaultConfig()
	c.AnomalyMethod = "zscore"
	return c
}

// IsAccuracyFilterEnabled 是否启用精度过滤
func (c Config) IsAccuracyFilterEnabled() bool {
	return c.MaxAccuracy > 0
}

// IsIqrMethod 是否使用 IQR 方法
func (c Config) IsIqrMethod() bool {
	return c.AnomalyMethod == "iqr"
}
