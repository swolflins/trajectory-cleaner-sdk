package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;

/**
 * Stage 1: 精度过滤
 * 基于 accuracy / HDOP 字段过滤低质量定位点
 *
 * 逻辑极简：accuracy > maxAccuracy 的点直接丢弃
 */
public class AccuracyFilterStage implements PipelineStage {

    private final CleanerConfig config;

    public AccuracyFilterStage(CleanerConfig config) {
        this.config = config;
    }

    @Override
    public CleanResult process(GPSPoint point, PipelineContext context) {
        if (!config.isAccuracyFilterEnabled()) {
            return CleanResult.passthrough(point, context.getMotionState());
        }

        // 如果设备有 accuracy 字段且超过阈值，直接丢弃
        if (point.hasAccuracy() && point.getAccuracy() > config.getMaxAccuracy()) {
            return CleanResult.dropped(
                point,
                CleanResult.Action.DROPPED_ACCURACY,
                String.format("accuracy %.1f > threshold %.1f", point.getAccuracy(), config.getMaxAccuracy()),
                context.getMotionState()
            );
        }

        return CleanResult.passthrough(point, context.getMotionState());
    }

    @Override
    public String getName() { return "AccuracyFilter"; }
}
