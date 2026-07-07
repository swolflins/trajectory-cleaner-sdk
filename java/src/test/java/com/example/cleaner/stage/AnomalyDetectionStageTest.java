package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * AnomalyDetectionStage 异常检测阶段单元测试
 */
class AnomalyDetectionStageTest {

    private static final double LAT = 39.9087;
    private static final double LON = 116.3975;
    private static final long BASE_TS = 1_700_000_000_000L;
    private static final double METER_TO_DEG = 1.0 / 111_320.0;

    private PipelineContext context;

    @BeforeEach
    void setUp() {
        context = new PipelineContext();
    }

    /** 向北移动 meters 米，时间戳 baseTs + seconds*1000 */
    private GPSPoint trackPoint(double meters, long baseTs, int secondOffset) {
        return new GPSPoint.Builder("dev-1", LAT + meters * METER_TO_DEG, LON, baseTs + secondOffset * 1000L)
                .accuracy(10)
                .build();
    }

    /**
     * 模拟 TrajectoryCleaner 的行为：执行 stage 后更新 context 的 lastOutputPoint
     */
    private CleanResult run(AnomalyDetectionStage stage, GPSPoint point) {
        CleanResult result = stage.process(point, context);
        if (result.hasOutput()) {
            context.setLastOutputPoint(result.getOutputPoint());
        }
        return result;
    }

    @Test
    @DisplayName("冷启动: 队列未满时前 statsWindowSize 个点全部 PASSTHROUGH")
    void testColdStartWindowNotFull() {
        int windowSize = 5;
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(windowSize)
                .maxAccuracy(-1)
                .maxVelocity(41.67)
                .anomalyStrategy("drop")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);

        // 前 5 个点（10m/s, 1s 间隔）应全部通过
        for (int i = 0; i < windowSize; i++) {
            GPSPoint p = trackPoint(i * 10.0, BASE_TS, i);
            CleanResult r = run(stage, p);
            assertEquals(CleanResult.Action.PASSTHROUGH, r.getAction(),
                    "第 " + (i + 1) + " 个点（队列未满）应 PASSTHROUGH");
            assertTrue(r.hasOutput(), "应有输出点");
        }
    }

    @Test
    @DisplayName("速度超限: velocity > maxVelocity 时检测到异常")
    void testVelocityExceedsLimit() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(5)
                .maxAccuracy(-1)
                .maxVelocity(41.67)
                .anomalyStrategy("drop")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);

        // 第 1 个点正常通过
        GPSPoint p1 = trackPoint(0, BASE_TS, 0);
        CleanResult r1 = run(stage, p1);
        assertEquals(CleanResult.Action.PASSTHROUGH, r1.getAction());

        // 第 2 个点跳 100m / 1s = 100 m/s > 41.67 → 异常
        GPSPoint p2 = trackPoint(100, BASE_TS, 1);
        CleanResult r2 = run(stage, p2);
        assertNotEquals(CleanResult.Action.PASSTHROUGH, r2.getAction(), "速度超限应检测到异常");
        assertTrue(r2.isDropped() || r2.getAction() == CleanResult.Action.REPLACED_ANOMALY,
                "应为丢弃或替代异常");
    }

    @Test
    @DisplayName("Z-score 模式: 填充窗口后注入异常速度点被检测")
    void testZScoreModeDetectsAnomaly() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(5)
                .maxAccuracy(-1)
                .useZScore()
                .initialSigma(3.0)
                .continuousSigma(2.0)
                .maxVelocity(41.67)
                .anomalyStrategy("replace")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);
        assertEquals("zscore", config.getAnomalyMethod());

        // 用变化的速度（9,11,10,9,11 m/s）填充窗口，使 std > 0
        double[] steps = {0, 9, 20, 30, 39, 50}; // 速度序列 9,11,10,9,11
        for (int i = 0; i < steps.length; i++) {
            GPSPoint p = trackPoint(steps[i], BASE_TS, i);
            CleanResult r = run(stage, p);
            assertEquals(CleanResult.Action.PASSTHROUGH, r.getAction(),
                    "窗口填充阶段第 " + (i + 1) + " 个点应 PASSTHROUGH");
        }

        // 注入异常速度点：30 m/s（< 41.67 不触发物理约束，但远离均值 ~10）
        GPSPoint anomaly = trackPoint(80, BASE_TS, 6); // 距上一点 30m / 1s = 30 m/s
        CleanResult result = run(stage, anomaly);
        assertNotEquals(CleanResult.Action.PASSTHROUGH, result.getAction(),
                "Z-score 模式应检测到异常速度点");
        assertEquals(CleanResult.Action.REPLACED_ANOMALY, result.getAction(),
                "replace 策略下应为 REPLACED_ANOMALY");
        assertTrue(result.hasOutput(), "替代策略应有输出点");
    }

    @Test
    @DisplayName("IQR 模式: 填充窗口后注入异常速度点被检测")
    void testIQRModeDetectsAnomaly() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(5)
                .maxAccuracy(-1)
                .useIQR()
                .iqrMultiplier(1.5)
                .maxVelocity(41.67)
                .anomalyStrategy("replace")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);
        assertEquals("iqr", config.getAnomalyMethod());
        assertTrue(config.isIqrMethod());

        // 用变化的速度填充窗口
        double[] steps = {0, 9, 20, 30, 39, 50}; // 速度 9,11,10,9,11
        for (int i = 0; i < steps.length; i++) {
            GPSPoint p = trackPoint(steps[i], BASE_TS, i);
            CleanResult r = run(stage, p);
            assertEquals(CleanResult.Action.PASSTHROUGH, r.getAction(),
                    "IQR 窗口填充阶段第 " + (i + 1) + " 个点应 PASSTHROUGH");
        }

        // 注入异常速度点：30 m/s
        GPSPoint anomaly = trackPoint(80, BASE_TS, 6); // 30m / 1s
        CleanResult result = run(stage, anomaly);
        assertNotEquals(CleanResult.Action.PASSTHROUGH, result.getAction(),
                "IQR 模式应检测到异常速度点");
        assertEquals(CleanResult.Action.REPLACED_ANOMALY, result.getAction(),
                "replace 策略下应为 REPLACED_ANOMALY");
        assertTrue(result.hasOutput(), "替代策略应有输出点");
    }

    @Test
    @DisplayName("级联防护(grace period): 异常后下一个正常点不会因 velocity 变大被误判")
    void testGracePeriodAfterAnomaly() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(5)
                .maxAccuracy(-1)
                .useIQR()
                .maxVelocity(41.67)
                .anomalyStrategy("replace")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);

        // p1 正常点
        GPSPoint p1 = trackPoint(0, BASE_TS, 0);
        run(stage, p1);

        // p2 飞点：跳 5000m / 1s = 5000 m/s → 异常，replace 后输出为 p1 替代点
        GPSPoint fly = trackPoint(5000, BASE_TS, 1);
        CleanResult rFly = run(stage, fly);
        assertEquals(CleanResult.Action.REPLACED_ANOMALY, rFly.getAction(), "飞点应被替代");
        assertTrue(rFly.hasOutput(), "替代点应有输出");
        // 替代点的坐标应为上一有效点(p1)，时间戳为当前点
        assertEquals(p1.getLatitude(), rFly.getOutputPoint().getLatitude(), 1e-9,
                "替代点坐标应为上一有效点");

        // p3 正常点（回到 p1 附近 10m）—— 不应被误判为异常
        GPSPoint p3 = trackPoint(10, BASE_TS, 2);
        CleanResult r3 = run(stage, p3);
        assertEquals(CleanResult.Action.PASSTHROUGH, r3.getAction(),
                "级联防护：异常后的正常点不应被误判");
        assertTrue(r3.hasOutput(), "正常点应有输出");
    }

    @Test
    @DisplayName("drop 策略: 异常点被 DROPPED_ANOMALY 且无输出")
    void testDropStrategyNoOutput() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(5)
                .maxAccuracy(-1)
                .maxVelocity(41.67)
                .anomalyStrategy("drop")
                .build();
        AnomalyDetectionStage stage = new AnomalyDetectionStage(config);

        GPSPoint p1 = trackPoint(0, BASE_TS, 0);
        run(stage, p1);

        // 飞点 → DROPPED_ANOMALY
        GPSPoint fly = trackPoint(5000, BASE_TS, 1);
        CleanResult result = run(stage, fly);
        assertEquals(CleanResult.Action.DROPPED_ANOMALY, result.getAction(), "drop 策略应丢弃异常点");
        assertTrue(result.isDropped(), "应被丢弃");
        assertFalse(result.hasOutput(), "drop 策略不应有输出点");
    }

    @Test
    @DisplayName("getName: 返回 AnomalyDetection")
    void testGetName() {
        AnomalyDetectionStage stage = new AnomalyDetectionStage(
                new CleanerConfig.Builder().maxAccuracy(-1).build());
        assertEquals("AnomalyDetection", stage.getName());
    }
}
