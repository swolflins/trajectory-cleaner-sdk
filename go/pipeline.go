package cleaner

import "fmt"

// cleaner 轨迹清洗器（核心结构体）
// 每个 GPS 设备应使用独立的 Cleaner 实例（内部有状态）
type Cleaner struct {
	config Config

	// ===== 运动状态 =====
	motionState MotionState

	// ===== Stage 2: 伪静止状态机 =====
	staticCounter int     // 静止判定队列计数
	stopPoint     *GPSPoint // 停止点 P
	lastValidPoint *GPSPoint // 上一个有效点
	motionCounter  int     // 静止→运动确认计数

	// ===== Stage 3: 异常检测 =====
	velocityWindow     []float64 // 速度序列队列
	accelerationWindow []float64 // 加速度序列队列
	calibrated         bool       // 是否完成首次校准
	prevVelocity       *float64   // 上一个速度值
	lastValidOutput    *GPSPoint  // 上一个有效输出点（用于替换源）
	lastActualPoint    *GPSPoint  // 上一个实际接收点（用于速度计算，避免级联误报）
	anomalyGraceCount  int        // 异常后宽限计数（跳过后续N个点的速度检测）

	// 统计量缓存 (Z-score)
	velocityMean, velocityStd float64
	accelMean, accelStd       float64

	// 统计量缓存 (IQR)
	velocityQ1, velocityQ3, velocityIQR float64
	accelQ1, accelQ3, accelIQR           float64
}

// New 创建清洗器
func New(config Config) *Cleaner {
	return &Cleaner{
		config:               config,
		motionState:          StateMoving,
		velocityWindow:       make([]float64, 0, config.StatsWindowSize),
		accelerationWindow:   make([]float64, 0, config.StatsWindowSize),
	}
}

// NewDefault 使用默认配置创建清洗器
func NewDefault() *Cleaner {
	return New(DefaultConfig())
}

// Clean 清洗单个 GPS 点
// 这是核心方法，每接收一个设备上报的点调用一次
func (c *Cleaner) Clean(point GPSPoint) CleanResult {
	// Stage 1: 精度过滤
	if result := c.accuracyFilter(point); result.IsDropped() {
		return result
	} else if result.HasOutput() {
		point = *result.OutputPoint
	}

	// Stage 2: 伪静止状态机
	if result := c.staticStateMachine(point); result.IsDropped() {
		return result
	} else if result.HasOutput() {
		point = *result.OutputPoint
	}

	// Stage 3: 异常检测
	result := c.anomalyDetection(point)

	// 更新上一个有效输出点
	if result.HasOutput() {
		out := *result.OutputPoint
		c.lastValidOutput = &out
	}

	return result
}

// Reset 重置清洗器状态（设备重新上线时调用）
func (c *Cleaner) Reset() {
	c.motionState = StateMoving
	c.staticCounter = 0
	c.stopPoint = nil
	c.lastValidPoint = nil
	c.motionCounter = 0
	c.velocityWindow = c.velocityWindow[:0]
	c.accelerationWindow = c.accelerationWindow[:0]
	c.calibrated = false
	c.prevVelocity = nil
	c.lastValidOutput = nil
	c.lastActualPoint = nil
	c.anomalyGraceCount = 0
}

// GetMotionState 获取当前运动状态
func (c *Cleaner) GetMotionState() MotionState {
	return c.motionState
}

// ===== Stage 1: 精度过滤 =====

func (c *Cleaner) accuracyFilter(point GPSPoint) CleanResult {
	if !c.config.IsAccuracyFilterEnabled() {
		return Passthrough(point, c.motionState)
	}
	if point.HasAccuracy() && point.Accuracy > c.config.MaxAccuracy {
		return Dropped(ActionDroppedAccuracy,
			fmt.Sprintf("accuracy %.1f > threshold %.1f", point.Accuracy, c.config.MaxAccuracy),
			c.motionState)
	}
	return Passthrough(point, c.motionState)
}

// ===== Stage 2: 伪静止状态机 =====

func (c *Cleaner) staticStateMachine(point GPSPoint) CleanResult {
	// 第一个点，初始化
	if c.lastValidPoint == nil {
		c.lastValidPoint = &point
		return Passthrough(point, c.motionState)
	}

	distance := c.lastValidPoint.DistanceTo(point)
	R := c.config.StaticDistanceThreshold

	switch c.motionState {
	case StateMoving:
		if distance < R {
			c.staticCounter++
			if c.staticCounter >= c.config.StaticQueueSize {
				// T 满，进入静止状态
				stop := *c.lastValidPoint
				c.stopPoint = &stop
				c.motionState = StateStatic
				c.staticCounter = 0
				c.motionCounter = 0
				return Transition(stop,
					fmt.Sprintf("MOVING→STATIC, distance=%.1fm, count=%d", distance, c.config.StaticQueueSize),
					c.motionState)
			}
		// 进入伪静止判定中，更新状态
		c.motionState = StatePendingStatic
		c.lastValidPoint = &point
		return Passthrough(point, c.motionState)
	}
	// 仍然在运动
	c.staticCounter = 0
	c.lastValidPoint = &point
	return Passthrough(point, c.motionState)

	case StateStatic:
		distToStop := c.stopPoint.DistanceTo(point)
		if distToStop < R {
			// 仍然静止，丢弃（维持停止点）
			return Dropped(ActionDroppedStatic,
				fmt.Sprintf("STATIC hold, dist=%.1fm < R=%.1fm", distToStop, R),
				c.motionState)
		}
		// 可能启动，需要确认
		c.motionCounter++
		if c.motionCounter >= c.config.MotionConfirmCount {
			c.lastValidPoint = &point
			c.stopPoint = nil
			c.motionCounter = 0
			c.motionState = StateMoving
			return Transition(point,
				fmt.Sprintf("STATIC→MOVING, confirmed after %d", c.config.MotionConfirmCount),
				c.motionState)
		}
		return Dropped(ActionDroppedStatic,
			fmt.Sprintf("STATIC pending motion, confirm=%d/%d", c.motionCounter, c.config.MotionConfirmCount),
			c.motionState)

	case StatePendingStatic:
		if distance < R {
			c.staticCounter++
			c.lastValidPoint = &point
			if c.staticCounter >= c.config.StaticQueueSize {
				stop := *c.lastValidPoint
				c.stopPoint = &stop
				c.motionState = StateStatic
				c.staticCounter = 0
				c.motionCounter = 0
				return Transition(stop, "PENDING→STATIC confirmed", c.motionState)
			}
			return Passthrough(point, c.motionState)
		}
		c.staticCounter = 0
		c.motionState = StateMoving
		c.lastValidPoint = &point
		return Passthrough(point, c.motionState)
	}

	c.lastValidPoint = &point
	return Passthrough(point, c.motionState)
}

// ===== Stage 3: 异常检测 =====

func (c *Cleaner) anomalyDetection(point GPSPoint) CleanResult {
	// 使用上一个实际接收点来计算速度（避免级联误报）
	var prevPoint GPSPoint
	if c.lastActualPoint != nil {
		prevPoint = *c.lastActualPoint
	} else if c.lastValidOutput != nil {
		prevPoint = *c.lastValidOutput
	} else if c.lastValidPoint != nil {
		prevPoint = *c.lastValidPoint
	} else {
		// 第一个点，初始化
		c.lastActualPoint = &point
		return Passthrough(point, c.motionState)
	}

	// 始终更新 lastActualPoint（用于后续速度计算，避免级联误报）
	actualPoint := point
	c.lastActualPoint = &actualPoint

	// 物理约束：速度超限直接判异常
	velocity := prevPoint.VelocityTo(point)
	if velocity > c.config.MaxVelocity {
		c.anomalyGraceCount = 2 // 异常后宽限 2 个点
		return c.handleAnomaly(point, fmt.Sprintf("velocity %.1f > max %.1f m/s", velocity, c.config.MaxVelocity))
	}

	// 计算加速度
	acceleration := 0.0
	if c.prevVelocity != nil {
		dt := prevPoint.TimeDiffSeconds(point)
		if dt > 0 {
			acceleration = (velocity - *c.prevVelocity) / dt
		}
	}

	// 冷启动：队列未满，只积累不检测
	if len(c.velocityWindow) < c.config.StatsWindowSize {
		c.addToWindow(velocity, acceleration)
		return Passthrough(point, c.motionState)
	}

	// 首次校准
	if !c.calibrated {
		c.calibrate()
		c.calibrated = true
	}

	// 宽限期内跳过统计异常检测（防止级联误报）
	if c.anomalyGraceCount > 0 {
		c.anomalyGraceCount--
		c.addToWindow(velocity, acceleration)
		return Passthrough(point, c.motionState)
	}

	// 持续检测
	velocityAnomaly := c.isAnomaly(velocity, true)
	accelAnomaly := c.isAnomaly(acceleration, false)

	// 无论是否异常，实际值都加入队列
	c.addToWindow(velocity, acceleration)

	if velocityAnomaly || accelAnomaly {
		c.anomalyGraceCount = 2 // 异常后宽限 2 个点
		reason := fmt.Sprintf("anomaly: v=%.1f%s, a=%.2f%s",
			velocity, ifStr(velocityAnomaly, "(X)", ""),
			acceleration, ifStr(accelAnomaly, "(X)", ""))
		return c.handleAnomaly(point, reason)
	}

	return Passthrough(point, c.motionState)
}

// handleAnomaly 处理异常点
func (c *Cleaner) handleAnomaly(point GPSPoint, reason string) CleanResult {
	if c.config.AnomalyStrategy == "replace" && c.lastValidOutput != nil {
		replacement := c.lastValidOutput.WithTimestamp(point.Timestamp)
		return Replaced(replacement, reason, c.motionState)
	}
	return Dropped(ActionDroppedAnomaly, reason, c.motionState)
}

// addToWindow 添加到滑动窗口
func (c *Cleaner) addToWindow(velocity, acceleration float64) {
	c.velocityWindow = append(c.velocityWindow, velocity)
	c.accelerationWindow = append(c.accelerationWindow, acceleration)

	// 维护窗口大小
	if len(c.velocityWindow) > c.config.StatsWindowSize {
		c.velocityWindow = c.velocityWindow[1:]
	}
	if len(c.accelerationWindow) > c.config.StatsWindowSize {
		c.accelerationWindow = c.accelerationWindow[1:]
	}
	pv := velocity
	c.prevVelocity = &pv
}

// calibrate 首次校准
func (c *Cleaner) calibrate() {
	if c.config.IsIqrMethod() {
		c.velocityQ1 = percentile(c.velocityWindow, 25)
		c.velocityQ3 = percentile(c.velocityWindow, 75)
		c.velocityIQR = c.velocityQ3 - c.velocityQ1
		c.accelQ1 = percentile(c.accelerationWindow, 25)
		c.accelQ3 = percentile(c.accelerationWindow, 75)
		c.accelIQR = c.accelQ3 - c.accelQ1
	} else {
		c.velocityMean = mean(c.velocityWindow)
		c.velocityStd = std(c.velocityWindow, c.velocityMean)
		c.accelMean = mean(c.accelerationWindow)
		c.accelStd = std(c.accelerationWindow, c.accelMean)

		// 3σ 清洗：移除超过 3σ 的点
		vThreshold := c.config.InitialSigma * c.velocityStd
		aThreshold := c.config.InitialSigma * c.accelStd
		cleanedV := make([]float64, 0, len(c.velocityWindow))
		cleanedA := make([]float64, 0, len(c.accelerationWindow))
		for i := 0; i < len(c.velocityWindow); i++ {
			if abs(c.velocityWindow[i]-c.velocityMean) <= vThreshold &&
				abs(c.accelerationWindow[i]-c.accelMean) <= aThreshold {
				cleanedV = append(cleanedV, c.velocityWindow[i])
				cleanedA = append(cleanedA, c.accelerationWindow[i])
			}
		}
		c.velocityWindow = cleanedV
		c.accelerationWindow = cleanedA

		// 重新计算
		if len(c.velocityWindow) > 0 {
			c.velocityMean = mean(c.velocityWindow)
			c.velocityStd = std(c.velocityWindow, c.velocityMean)
			c.accelMean = mean(c.accelerationWindow)
			c.accelStd = std(c.accelerationWindow, c.accelMean)
		}
	}
}

// isAnomaly 判断单个值是否异常
func (c *Cleaner) isAnomaly(value float64, isVelocity bool) bool {
	if c.config.IsIqrMethod() {
		var q1, q3, iqr float64
		if isVelocity {
			q1, q3, iqr = c.velocityQ1, c.velocityQ3, c.velocityIQR
		} else {
			q1, q3, iqr = c.accelQ1, c.accelQ3, c.accelIQR
		}
		lower := q1 - c.config.IqrMultiplier*iqr
		upper := q3 + c.config.IqrMultiplier*iqr
		return value < lower || value > upper
	}

	var m, s float64
	if isVelocity {
		m, s = c.velocityMean, c.velocityStd
	} else {
		m, s = c.accelMean, c.accelStd
	}
	if s == 0 {
		return false
	}
	return abs(value-m) > c.config.ContinuousSigma*s
}

// ifStr 三元表达式辅助
func ifStr(cond bool, trueVal, falseVal string) string {
	if cond {
		return trueVal
	}
	return falseVal
}
