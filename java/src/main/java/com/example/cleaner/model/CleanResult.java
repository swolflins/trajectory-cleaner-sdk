package com.example.cleaner.model;

/**
 * 清洗结果
 * 包含处理后的坐标点、处理动作和原因
 */
public final class CleanResult {

    /** 清洗动作 */
    public enum Action {
        /** 正常通过，使用原始坐标 */
        PASSTHROUGH,
        /** 精度过滤丢弃 */
        DROPPED_ACCURACY,
        /** 伪静止状态丢弃（维持停止点） */
        DROPPED_STATIC,
        /** Z-score/IQR 异常，用上一有效值替代 */
        REPLACED_ANOMALY,
        /** 异常点直接丢弃 */
        DROPPED_ANOMALY,
        /** 静止→运动状态切换，正常输出 */
        STATE_TRANSITION
    }

    /** 设备运动状态 */
    public enum MotionState {
        MOVING,
        STATIC,
        /** 正在判断是否进入静止 */
        PENDING_STATIC
    }

    private final GPSPoint outputPoint;
    private final Action action;
    private final String reason;
    private final MotionState currentState;

    private CleanResult(GPSPoint outputPoint, Action action, String reason, MotionState currentState) {
        this.outputPoint = outputPoint;
        this.action = action;
        this.reason = reason;
        this.currentState = currentState;
    }

    /**
     * 正常通过
     */
    public static CleanResult passthrough(GPSPoint point, MotionState state) {
        return new CleanResult(point, Action.PASSTHROUGH, "OK", state);
    }

    /**
     * 丢弃点（精度过滤/伪静止）
     */
    public static CleanResult dropped(GPSPoint point, Action action, String reason, MotionState state) {
        return new CleanResult(null, action, reason, state);
    }

    /**
     * 异常替代（用上一有效值替代当前坐标）
     */
    public static CleanResult replaced(GPSPoint replacement, String reason, MotionState state) {
        return new CleanResult(replacement, Action.REPLACED_ANOMALY, reason, state);
    }

    /**
     * 状态切换
     */
    public static CleanResult transition(GPSPoint point, String reason, MotionState state) {
        return new CleanResult(point, Action.STATE_TRANSITION, reason, state);
    }

    public GPSPoint getOutputPoint() { return outputPoint; }
    public Action getAction() { return action; }
    public String getReason() { return reason; }
    public MotionState getCurrentState() { return currentState; }

    /** 是否产生了有效输出坐标 */
    public boolean hasOutput() { return outputPoint != null; }

    /** 是否被丢弃 */
    public boolean isDropped() {
        return action == Action.DROPPED_ACCURACY
            || action == Action.DROPPED_STATIC
            || action == Action.DROPPED_ANOMALY;
    }

    @Override
    public String toString() {
        String pointStr = outputPoint != null ? outputPoint.toString() : "null";
        return String.format("CleanResult{action=%s, state=%s, reason=%s, point=%s}", action, currentState, reason, pointStr);
    }
}
