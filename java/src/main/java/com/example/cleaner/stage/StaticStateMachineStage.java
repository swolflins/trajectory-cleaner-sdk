package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;

/**
 * Stage 2: 伪静止状态机
 * 对应图片方案 2.2.1 高频小幅度位置变化处理
 *
 * 状态转换逻辑:
 *   运动状态 → 静止判定中 (队列 T 计数)
 *   静止判定中 → 运动状态 (距离 > R，清空 T)
 *   静止判定中 → 静止状态 (T 满)
 *   静止状态 → 静止状态 (距离 < R，维持停止点 P)
 *   静止状态 → 运动状态 (距离 > R，确认启动)
 *
 * 改进点:
 *   - 静止→运动需要 motionConfirmCount 次连续确认，避免拥堵走走停停误判
 *   - 静止期间使用停止点 P 替代而非丢弃，保证坐标连续
 */
public class StaticStateMachineStage implements PipelineStage {

    private final CleanerConfig config;

    /** 静止判定队列计数 */
    private int staticCounter = 0;

    /** 停止点 P (进入静止状态时的最后一个点) */
    private GPSPoint stopPoint = null;

    /** 上一个有效点 (用于距离计算) */
    private GPSPoint lastValidPoint = null;

    /** 静止→运动的确认计数 */
    private int motionCounter = 0;

    public StaticStateMachineStage(CleanerConfig config) {
        this.config = config;
    }

    @Override
    public CleanResult process(GPSPoint point, PipelineContext context) {
        MotionStateWrapper state = context.getMotionStateWrapper();

        // 第一个点，初始化
        if (lastValidPoint == null) {
            lastValidPoint = point;
            state.set(MotionStateWrapper.STATE_MOVING);
            return CleanResult.passthrough(point, state.get());
        }

        double distance = lastValidPoint.distanceTo(point);
        double R = config.getStaticDistanceThreshold();

        switch (state.getValue()) {
            case MotionStateWrapper.STATE_MOVING: {
                // 运动状态：检查是否开始静止
                if (distance < R) {
                    staticCounter++;
                    if (staticCounter >= config.getStaticQueueSize()) {
                        // T 满，进入静止状态
                        stopPoint = lastValidPoint;
                        state.set(MotionStateWrapper.STATE_STATIC);
                        staticCounter = 0;
                        motionCounter = 0;
                        return CleanResult.transition(
                            stopPoint,
                            String.format("MOVING→STATIC, distance=%.1fm < R=%.1fm, count=%d", distance, R, config.getStaticQueueSize()),
                            state.get()
                        );
                    }
                    // 静止判定中，暂不输出（或输出原始点等待后续判断）
                    // 这里选择输出原始点，因为此时还不确定是否真的静止
                    state.set(MotionStateWrapper.STATE_PENDING_STATIC);
                    return CleanResult.passthrough(point, state.get());
                } else {
                    // 仍然在运动，清空计数
                    staticCounter = 0;
                    lastValidPoint = point;
                    return CleanResult.passthrough(point, state.get());
                }
            }

            case MotionStateWrapper.STATE_STATIC: {
                // 静止状态：与停止点 P 比较距离
                double distToStop = stopPoint.distanceTo(point);
                if (distToStop < R) {
                    // 仍然静止，用停止点替代，维持坐标稳定
                    return CleanResult.dropped(
                        point,
                        CleanResult.Action.DROPPED_STATIC,
                        String.format("STATIC hold, dist to P=%.1fm < R=%.1fm", distToStop, R),
                        state.get()
                    );
                } else {
                    // 距离 > R，可能是启动，需要确认
                    motionCounter++;
                    if (motionCounter >= config.getMotionConfirmCount()) {
                        // 确认启动
                        lastValidPoint = point;
                        stopPoint = null;
                        motionCounter = 0;
                        state.set(MotionStateWrapper.STATE_MOVING);
                        return CleanResult.transition(
                            point,
                            String.format("STATIC→MOVING, confirmed after %d points", config.getMotionConfirmCount()),
                            state.get()
                        );
                    } else {
                        // 等待确认，期间维持停止点
                        return CleanResult.dropped(
                            point,
                            CleanResult.Action.DROPPED_STATIC,
                            String.format("STATIC pending motion, confirm=%d/%d", motionCounter, config.getMotionConfirmCount()),
                            state.get()
                        );
                    }
                }
            }

            case MotionStateWrapper.STATE_PENDING_STATIC: {
                // 伪静止判定中，重新评估
                if (distance < R) {
                    staticCounter++;
                    if (staticCounter >= config.getStaticQueueSize()) {
                        stopPoint = lastValidPoint;
                        state.set(MotionStateWrapper.STATE_STATIC);
                        staticCounter = 0;
                        motionCounter = 0;
                        return CleanResult.transition(stopPoint, "PENDING→STATIC confirmed", state.get());
                    }
                    return CleanResult.passthrough(point, state.get());
                } else {
                    // 距离 > R，回到运动状态
                    staticCounter = 0;
                    state.set(MotionStateWrapper.STATE_MOVING);
                    lastValidPoint = point;
                    return CleanResult.passthrough(point, state.get());
                }
            }

            default:
                lastValidPoint = point;
                return CleanResult.passthrough(point, state.get());
        }
    }

    @Override
    public String getName() { return "StaticStateMachine"; }

    /**
     * 获取当前停止点（用于外部读取或补偿）
     */
    public GPSPoint getStopPoint() { return stopPoint; }
}
