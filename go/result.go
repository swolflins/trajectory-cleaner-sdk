package cleaner

import "fmt"

// Action 清洗动作类型
type Action int

const (
	ActionPassthrough      Action = iota // 正常通过，使用原始坐标
	ActionDroppedAccuracy               // 精度过滤丢弃
	ActionDroppedStatic                  // 伪静止状态丢弃
	ActionReplacedAnomaly                // Z-score/IQR 异常，用上一有效值替代
	ActionDroppedAnomaly                 // 异常点直接丢弃
	ActionStateTransition                // 静止→运动状态切换
)

func (a Action) String() string {
	switch a {
	case ActionPassthrough:
		return "PASSTHROUGH"
	case ActionDroppedAccuracy:
		return "DROPPED_ACCURACY"
	case ActionDroppedStatic:
		return "DROPPED_STATIC"
	case ActionReplacedAnomaly:
		return "REPLACED_ANOMALY"
	case ActionDroppedAnomaly:
		return "DROPPED_ANOMALY"
	case ActionStateTransition:
		return "STATE_TRANSITION"
	default:
		return "UNKNOWN"
	}
}

// MotionState 设备运动状态
type MotionState int

const (
	StateMoving        MotionState = iota // 运动中
	StateStatic                          // 静止
	StatePendingStatic                   // 正在判断是否进入静止
)

func (s MotionState) String() string {
	switch s {
	case StateMoving:
		return "MOVING"
	case StateStatic:
		return "STATIC"
	case StatePendingStatic:
		return "PENDING_STATIC"
	default:
		return "UNKNOWN"
	}
}

// CleanResult 清洗结果
type CleanResult struct {
	// 处理后的输出坐标点，nil 表示被丢弃
	OutputPoint *GPSPoint
	// 清洗动作
	Action Action
	// 原因说明
	Reason string
	// 当前运动状态
	CurrentState MotionState
}

// Passthrough 正常通过
func Passthrough(point GPSPoint, state MotionState) CleanResult {
	return CleanResult{
		OutputPoint:  &point,
		Action:       ActionPassthrough,
		Reason:       "OK",
		CurrentState: state,
	}
}

// Dropped 丢弃
func Dropped(action Action, reason string, state MotionState) CleanResult {
	return CleanResult{
		OutputPoint:  nil,
		Action:       action,
		Reason:       reason,
		CurrentState: state,
	}
}

// Replaced 异常替代
func Replaced(replacement GPSPoint, reason string, state MotionState) CleanResult {
	return CleanResult{
		OutputPoint:  &replacement,
		Action:       ActionReplacedAnomaly,
		Reason:       reason,
		CurrentState: state,
	}
}

// Transition 状态切换
func Transition(point GPSPoint, reason string, state MotionState) CleanResult {
	return CleanResult{
		OutputPoint:  &point,
		Action:       ActionStateTransition,
		Reason:       reason,
		CurrentState: state,
	}
}

// HasOutput 是否有有效输出
func (r CleanResult) HasOutput() bool {
	return r.OutputPoint != nil
}

// IsDropped 是否被丢弃
func (r CleanResult) IsDropped() bool {
	return r.Action == ActionDroppedAccuracy ||
		r.Action == ActionDroppedStatic ||
		r.Action == ActionDroppedAnomaly
}

func (r CleanResult) String() string {
	var pointStr string
	if r.OutputPoint != nil {
		pointStr = fmt.Sprintf("{lat:%.6f, lon:%.6f, ts:%d}", r.OutputPoint.Latitude, r.OutputPoint.Longitude, r.OutputPoint.Timestamp)
	} else {
		pointStr = "nil"
	}
	return fmt.Sprintf("CleanResult{action:%s, state:%s, reason:%s, point:%s}",
		r.Action, r.CurrentState, r.Reason, pointStr)
}
