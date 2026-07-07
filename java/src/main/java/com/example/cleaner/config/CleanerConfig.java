package com.example.cleaner.config;

/**
 * 清洗 SDK 参数配置
 * 使用 Builder 模式，所有参数都有合理默认值
 */
public final class CleanerConfig {

    // ===== Stage 1: 精度过滤 =====

    /** 定位精度阈值(米)，超过此值的点直接丢弃。-1 表示不检查精度 */
    private final double maxAccuracy;

    // ===== Stage 2: 伪静止状态机 =====

    /** 伪静止判断队列长度 N */
    private final int staticQueueSize;

    /** 静止判定距离阈值 R (米) */
    private final double staticDistanceThreshold;

    /** 静止→运动的确认计数（连续N次距离>R才判定启动） */
    private final int motionConfirmCount;

    // ===== Stage 3: Z-score / IQR 异常检测 =====

    /** 统计窗口大小 */
    private final int statsWindowSize;

    /** 异常检测方法: "zscore" 或 "iqr" */
    private final String anomalyMethod;

    /** 首次校准置信度 (Z-score 的 σ 倍数，如 3.0 = 3σ) */
    private final double initialSigma;

    /** 持续检测置信度 (Z-score 的 σ 倍数，如 2.0 = 2σ) */
    private final double continuousSigma;

    /** IQR 法的乘数因子 (如 1.5 = Q1-1.5*IQR ~ Q3+1.5*IQR) */
    private final double iqrMultiplier;

    /** 速度上限 (m/s)，超过此值判定异常（物理约束） */
    private final double maxVelocity;

    // ===== Stage 4: 异常处理 =====

    /** 异常处理策略: "replace" 用上一有效值替代, "drop" 丢弃 */
    private final String anomalyStrategy;

    private CleanerConfig(Builder builder) {
        this.maxAccuracy = builder.maxAccuracy;
        this.staticQueueSize = builder.staticQueueSize;
        this.staticDistanceThreshold = builder.staticDistanceThreshold;
        this.motionConfirmCount = builder.motionConfirmCount;
        this.statsWindowSize = builder.statsWindowSize;
        this.anomalyMethod = builder.anomalyMethod;
        this.initialSigma = builder.initialSigma;
        this.continuousSigma = builder.continuousSigma;
        this.iqrMultiplier = builder.iqrMultiplier;
        this.maxVelocity = builder.maxVelocity;
        this.anomalyStrategy = builder.anomalyStrategy;
    }

    // Getters
    public double getMaxAccuracy() { return maxAccuracy; }
    public int getStaticQueueSize() { return staticQueueSize; }
    public double getStaticDistanceThreshold() { return staticDistanceThreshold; }
    public int getMotionConfirmCount() { return motionConfirmCount; }
    public int getStatsWindowSize() { return statsWindowSize; }
    public String getAnomalyMethod() { return anomalyMethod; }
    public double getInitialSigma() { return initialSigma; }
    public double getContinuousSigma() { return continuousSigma; }
    public double getIqrMultiplier() { return iqrMultiplier; }
    public double getMaxVelocity() { return maxVelocity; }
    public String getAnomalyStrategy() { return anomalyStrategy; }

    /** 是否启用精度过滤 */
    public boolean isAccuracyFilterEnabled() { return maxAccuracy > 0; }

    /** 是否使用 IQR 方法 */
    public boolean isIqrMethod() { return "iqr".equalsIgnoreCase(anomalyMethod); }

    /**
     * 默认配置 (针对 15s 上报间隔的物流场景)
     */
    public static CleanerConfig defaultConfig() {
        return new Builder().build();
    }

    /**
     * Builder 模式
     */
    public static class Builder {
        // 精度过滤
        private double maxAccuracy = 50.0;          // 50 米，参考 gpspathtransfigure

        // 伪静止状态机
        private int staticQueueSize = 10;           // 队列 T 长度 N
        private double staticDistanceThreshold = 15.0; // R = 15m，参考 DBSCAN eps
        private int motionConfirmCount = 3;         // 连续 3 次确认启动

        // Z-score / IQR
        private int statsWindowSize = 10;            // 队列 L 大小
        private String anomalyMethod = "iqr";        // 默认 IQR（更鲁棒）
        private double initialSigma = 3.0;          // 首次 3σ
        private double continuousSigma = 2.0;        // 持续 2σ
        private double iqrMultiplier = 1.5;         // IQR 乘数
        private double maxVelocity = 41.67;          // 150 km/h = 41.67 m/s

        // 异常处理
        private String anomalyStrategy = "replace";  // 替代模式

        public Builder maxAccuracy(double maxAccuracy) { this.maxAccuracy = maxAccuracy; return this; }
        public Builder staticQueueSize(int staticQueueSize) { this.staticQueueSize = staticQueueSize; return this; }
        public Builder staticDistanceThreshold(double staticDistanceThreshold) { this.staticDistanceThreshold = staticDistanceThreshold; return this; }
        public Builder motionConfirmCount(int motionConfirmCount) { this.motionConfirmCount = motionConfirmCount; return this; }
        public Builder statsWindowSize(int statsWindowSize) { this.statsWindowSize = statsWindowSize; return this; }
        public Builder anomalyMethod(String anomalyMethod) { this.anomalyMethod = anomalyMethod; return this; }
        public Builder initialSigma(double initialSigma) { this.initialSigma = initialSigma; return this; }
        public Builder continuousSigma(double continuousSigma) { this.continuousSigma = continuousSigma; return this; }
        public Builder iqrMultiplier(double iqrMultiplier) { this.iqrMultiplier = iqrMultiplier; return this; }
        public Builder maxVelocity(double maxVelocity) { this.maxVelocity = maxVelocity; return this; }
        public Builder anomalyStrategy(String anomalyStrategy) { this.anomalyStrategy = anomalyStrategy; return this; }

        /**
         * 切换为 Z-score 模式（对应图片方案 2.2.2 方案一）
         */
        public Builder useZScore() {
            this.anomalyMethod = "zscore";
            return this;
        }

        /**
         * 切换为 IQR 模式（更鲁棒，推荐）
         */
        public Builder useIQR() {
            this.anomalyMethod = "iqr";
            return this;
        }

        public CleanerConfig build() { return new CleanerConfig(this); }
    }

    @Override
    public String toString() {
        return String.format(
            "CleanerConfig{method=%s, maxAcc=%.0fm, R=%.1fm, N_static=%d, N_stats=%d, maxV=%.1fm/s}",
            anomalyMethod, maxAccuracy, staticDistanceThreshold, staticQueueSize, statsWindowSize, maxVelocity
        );
    }
}
