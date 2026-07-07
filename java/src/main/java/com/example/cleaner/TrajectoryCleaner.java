package com.example.cleaner;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;
import com.example.cleaner.stage.*;

import java.util.ArrayList;
import java.util.List;

/**
 * 轨迹清洗 Pipeline 主入口
 *
 * 将 GPS 点按顺序通过各 Stage 处理:
 *   1. 精度过滤 (AccuracyFilter)
 *   2. 伪静止状态机 (StaticStateMachine)
 *   3. Z-score/IQR 异常检测 (AnomalyDetection)
 *
 * 线程安全: 每个设备应使用独立的 Cleaner 实例（内部有状态）
 *
 * 使用示例:
 * <pre>
 * CleanerConfig config = new CleanerConfig.Builder()
 *     .maxAccuracy(50)
 *     .staticDistanceThreshold(15)
 *     .useIQR()
 *     .build();
 *
 * TrajectoryCleaner cleaner = new TrajectoryCleaner(config);
 *
 * for (GPSPoint point : rawPoints) {
 *     CleanResult result = cleaner.clean(point);
 *     if (result.hasOutput()) {
 *         publish(result.getOutputPoint());
 *     }
 * }
 * </pre>
 */
public class TrajectoryCleaner {

    private final CleanerConfig config;
    private final List<PipelineStage> stages;
    private final PipelineContext context;

    /**
     * 使用指定配置创建清洗器
     */
    public TrajectoryCleaner(CleanerConfig config) {
        this.config = config;
        this.stages = new ArrayList<>();
        this.context = new PipelineContext();

        // 构建 Pipeline
        stages.add(new AccuracyFilterStage(config));
        stages.add(new StaticStateMachineStage(config));
        stages.add(new AnomalyDetectionStage(config));
    }

    /**
     * 使用默认配置创建清洗器
     */
    public TrajectoryCleaner() {
        this(CleanerConfig.defaultConfig());
    }

    /**
     * 清洗单个 GPS 点
     * 这是核心方法，每接收一个设备上报的点调用一次
     *
     * @param point 原始 GPS 点
     * @return 清洗结果（包含处理动作、输出点、原因等）
     */
    public CleanResult clean(GPSPoint point) {
        CleanResult result = null;
        GPSPoint currentPoint = point;

        for (PipelineStage stage : stages) {
            result = stage.process(currentPoint, context);
            if (result.isDropped()) {
                // 被丢弃，不继续后续 Stage
                return result;
            }
            if (result.hasOutput()) {
                currentPoint = result.getOutputPoint();
            }
        }

        // 更新上下文中的上一个输出点
        if (result != null && result.hasOutput()) {
            context.setLastOutputPoint(result.getOutputPoint());
        }

        return result;
    }

    /**
     * 重置清洗器状态（设备重新上线时调用）
     */
    public void reset() {
        context.setMotionState(MotionStateWrapper.STATE_MOVING);
        context.setLastOutputPoint(null);
        // 重新创建各 Stage 以重置内部状态
        stages.clear();
        stages.add(new AccuracyFilterStage(config));
        stages.add(new StaticStateMachineStage(config));
        stages.add(new AnomalyDetectionStage(config));
    }

    public CleanerConfig getConfig() { return config; }
}
