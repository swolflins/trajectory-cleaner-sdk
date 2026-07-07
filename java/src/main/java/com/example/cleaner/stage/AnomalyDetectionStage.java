package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;

import java.util.LinkedList;
import java.util.Queue;

/**
 * Stage 3: Z-score / IQR 异常检测
 * 对应图片方案 2.2.2 低频大幅度位置变化处理
 *
 * 两种检测方法:
 *   - Z-score: 首次 3σ 校准 + 持续 2σ 检测（对应图片方案一）
 *   - IQR:    四分位距法，不依赖正态分布假设，更鲁棒（推荐）
 *
 * 检测对象:
 *   - 速度序列 {v}（一阶差分计算）
 *   - 加速度序列 {a}（二阶差分计算）
 *
 * 关键设计:
 *   - 冷启动: 队列 L 未满时不检测，只积累
 *   - 首次校准: 队列满时做一次 3σ 清洗
 *   - 持续检测: 后续每点做 2σ 检测
 *   - 无论是否异常，实际值都加入队列 L（防死循环）
 */
public class AnomalyDetectionStage implements PipelineStage {

    private final CleanerConfig config;

    /** 速度序列队列 L */
    private final Queue<Double> velocityWindow = new LinkedList<>();

    /** 加速度序列队列 L */
    private final Queue<Double> accelerationWindow = new LinkedList<>();

    /** 是否已完成首次校准 */
    private boolean calibrated = false;

    /** 上一个速度值 (用于计算加速度) */
    private Double prevVelocity = null;

    /** 上一个有效输出点 (用于异常替代) */
    private GPSPoint lastValidOutput = null;

    // 统计量缓存
    private double velocityMean, velocityStd;
    private double velocityQ1, velocityQ3, velocityIQR;
    private double accelMean, accelStd;
    private double accelQ1, accelQ3, accelIQR;

    public AnomalyDetectionStage(CleanerConfig config) {
        this.config = config;
    }

    @Override
    public CleanResult process(GPSPoint point, PipelineContext context) {
        // 需要上一个点来计算速度
        GPSPoint prevPoint = context.getLastOutputPoint();
        if (prevPoint == null) {
            lastValidOutput = point;
            return CleanResult.passthrough(point, context.getMotionState());
        }

        // 物理约束：速度超限直接判异常
        double velocity = prevPoint.velocityTo(point);
        if (velocity > config.getMaxVelocity()) {
            return handleAnomaly(point, String.format("velocity %.1f > max %.1f m/s", velocity, config.getMaxVelocity()), context);
        }

        // 计算加速度
        double acceleration = 0;
        if (prevVelocity != null) {
            double dt = prevPoint.timeDiffSeconds(point);
            if (dt > 0) {
                acceleration = (velocity - prevVelocity) / dt;
            }
        }

        // 冷启动：队列未满，只积累不检测
        if (velocityWindow.size() < config.getStatsWindowSize()) {
            addToWindow(velocity, acceleration);
            lastValidOutput = point;
            return CleanResult.passthrough(point, context.getMotionState());
        }

        // 首次校准
        if (!calibrated) {
            calibrate();
            calibrated = true;
        }

        // 持续检测
        boolean velocityAnomaly = isAnomaly(velocity, true);
        boolean accelerationAnomaly = isAnomaly(acceleration, false);

        // 无论是否异常，实际值都加入队列（防死循环）
        addToWindow(velocity, acceleration);

        if (velocityAnomaly || accelerationAnomaly) {
            String reason = String.format("anomaly: v=%.1f%s, a=%.2f%s",
                velocity, velocityAnomaly ? "(X)" : "",
                acceleration, accelerationAnomaly ? "(X)" : "");
            return handleAnomaly(point, reason, context);
        }

        lastValidOutput = point;
        prevVelocity = velocity;
        return CleanResult.passthrough(point, context.getMotionState());
    }

    /**
     * 处理异常点：replace 或 drop
     */
    private CleanResult handleAnomaly(GPSPoint point, String reason, PipelineContext context) {
        if ("replace".equalsIgnoreCase(config.getAnomalyStrategy()) && lastValidOutput != null) {
            // 用上一有效值替代（保持时间戳为当前点）
            GPSPoint replacement = lastValidOutput.withTimestamp(point.getTimestamp());
            return CleanResult.replaced(replacement, reason, context.getMotionState());
        } else {
            return CleanResult.dropped(point, CleanResult.Action.DROPPED_ANOMALY, reason, context.getMotionState());
        }
    }

    /**
     * 添加到滑动窗口并维护大小
     */
    private void addToWindow(double velocity, double acceleration) {
        velocityWindow.add(velocity);
        accelerationWindow.add(acceleration);
        while (velocityWindow.size() > config.getStatsWindowSize()) {
            velocityWindow.poll();
        }
        while (accelerationWindow.size() > config.getStatsWindowSize()) {
            accelerationWindow.poll();
        }
        prevVelocity = velocity;
    }

    /**
     * 首次校准：对队列做一次 3σ 清洗，计算统计量
     */
    private void calibrate() {
        double[] velocities = toDoubleArray(velocityWindow);
        double[] accelerations = toDoubleArray(accelerationWindow);

        if (config.isIqrMethod()) {
            // IQR 法
            velocityQ1 = percentile(velocities, 25);
            velocityQ3 = percentile(velocities, 75);
            velocityIQR = velocityQ3 - velocityQ1;
            accelQ1 = percentile(accelerations, 25);
            accelQ3 = percentile(accelerations, 75);
            accelIQR = accelQ3 - accelQ1;
        } else {
            // Z-score 法
            velocityMean = mean(velocities);
            velocityStd = std(velocities, velocityMean);
            accelMean = mean(accelerations);
            accelStd = std(accelerations, accelMean);

            // 3σ 清洗：移除超过 3σ 的点
            double vThreshold = config.getInitialSigma() * velocityStd;
            double aThreshold = config.getInitialSigma() * accelStd;
            Queue<Double> cleanedV = new LinkedList<>();
            Queue<Double> cleanedA = new LinkedList<>();
            Double[] vArr = velocityWindow.toArray(new Double[0]);
            Double[] aArr = accelerationWindow.toArray(new Double[0]);
            velocityWindow.clear();
            accelerationWindow.clear();
            for (int i = 0; i < vArr.length; i++) {
                if (Math.abs(vArr[i] - velocityMean) <= vThreshold &&
                    Math.abs(aArr[i] - accelMean) <= aThreshold) {
                    velocityWindow.add(vArr[i]);
                    accelerationWindow.add(aArr[i]);
                }
            }
            // 重新计算
            if (!velocityWindow.isEmpty()) {
                velocities = toDoubleArray(velocityWindow);
                accelerations = toDoubleArray(accelerationWindow);
                velocityMean = mean(velocities);
                velocityStd = std(velocities, velocityMean);
                accelMean = mean(accelerations);
                accelStd = std(accelerations, accelMean);
            }
        }
    }

    /**
     * 判断单个值是否异常
     */
    private boolean isAnomaly(double value, boolean isVelocity) {
        if (config.isIqrMethod()) {
            double q1 = isVelocity ? velocityQ1 : accelQ1;
            double q3 = isVelocity ? velocityQ3 : accelQ3;
            double iqr = isVelocity ? velocityIQR : accelIQR;
            double lower = q1 - config.getIqrMultiplier() * iqr;
            double upper = q3 + config.getIqrMultiplier() * iqr;
            return value < lower || value > upper;
        } else {
            double mean = isVelocity ? velocityMean : accelMean;
            double std = isVelocity ? velocityStd : accelStd;
            if (std == 0) return false;
            double sigma = config.getContinuousSigma();
            return Math.abs(value - mean) > sigma * std;
        }
    }

    // ===== 统计工具方法 =====

    private double[] toDoubleArray(Queue<Double> queue) {
        return queue.stream().mapToDouble(Double::doubleValue).toArray();
    }

    private double mean(double[] arr) {
        if (arr.length == 0) return 0;
        double sum = 0;
        for (double v : arr) sum += v;
        return sum / arr.length;
    }

    private double std(double[] arr, double mean) {
        if (arr.length <= 1) return 0;
        double sum = 0;
        for (double v : arr) sum += (v - mean) * (v - mean);
        return Math.sqrt(sum / (arr.length - 1));
    }

    private double percentile(double[] arr, double p) {
        if (arr.length == 0) return 0;
        double[] sorted = arr.clone();
        java.util.Arrays.sort(sorted);
        double index = p / 100.0 * (sorted.length - 1);
        int lower = (int) Math.floor(index);
        int upper = (int) Math.ceil(index);
        if (lower == upper) return sorted[lower];
        return sorted[lower] + (index - lower) * (sorted[upper] - sorted[lower]);
    }

    @Override
    public String getName() { return "AnomalyDetection"; }
}
