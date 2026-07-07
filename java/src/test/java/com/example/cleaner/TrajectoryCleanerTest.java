package com.example.cleaner;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

/**
 * TrajectoryCleaner 端到端 Pipeline 单元测试（核心）
 */
class TrajectoryCleanerTest {

    private static final double START_LAT = 39.90;
    private static final double START_LON = 116.40;
    private static final long BASE_TS = 1_700_000_000_000L;
    private static final double METER_TO_DEG = 1.0 / 111_320.0;

    // ===== 辅助构造方法 =====

    /** 构造线性轨迹点：从起点向北，每步 stepMeters 米，间隔 1 秒 */
    private GPSPoint linearPoint(int index, double stepMeters, double accuracy) {
        return new GPSPoint.Builder("dev-1",
                START_LAT + index * stepMeters * METER_TO_DEG,
                START_LON,
                BASE_TS + index * 1000L)
                .accuracy(accuracy)
                .build();
    }

    /** 构造指定偏移(米)的点 */
    private GPSPoint pointAt(double latOffsetMeters, long ts, double accuracy) {
        return new GPSPoint.Builder("dev-1",
                START_LAT + latOffsetMeters * METER_TO_DEG,
                START_LON, ts)
                .accuracy(accuracy)
                .build();
    }

    /** 在某点基础上向北跳跃 jumpMeters 米 */
    private GPSPoint jumpFrom(GPSPoint from, double jumpMeters, long ts) {
        return new GPSPoint.Builder("dev-1",
                from.getLatitude() + jumpMeters * METER_TO_DEG,
                from.getLongitude(), ts)
                .accuracy(10)
                .build();
    }

    /** 批量清洗并返回结果列表 */
    private List<CleanResult> feedAll(TrajectoryCleaner cleaner, List<GPSPoint> points) {
        List<CleanResult> results = new ArrayList<>();
        for (GPSPoint p : points) {
            results.add(cleaner.clean(p));
        }
        return results;
    }

    private List<GPSPoint> buildLinearTrack(int count, double stepMeters, double accuracy) {
        List<GPSPoint> track = new ArrayList<>();
        for (int i = 0; i < count; i++) {
            track.add(linearPoint(i, stepMeters, accuracy));
        }
        return track;
    }

    // ===== 测试用例 =====

    @Test
    @DisplayName("正常轨迹全部 PASSTHROUGH: 10个点每秒移动约10m(10m/s)全部通过")
    void testNormalTrajectoryAllPassthrough() {
        TrajectoryCleaner cleaner = new TrajectoryCleaner();
        List<GPSPoint> track = buildLinearTrack(10, 10.0, 10.0);
        List<CleanResult> results = feedAll(cleaner, track);

        assertEquals(10, results.size());
        for (int i = 0; i < results.size(); i++) {
            assertEquals(CleanResult.Action.PASSTHROUGH, results.get(i).getAction(),
                    "第 " + (i + 1) + " 个正常点应 PASSTHROUGH");
            assertTrue(results.get(i).hasOutput(), "第 " + (i + 1) + " 个点应有输出");
            assertFalse(results.get(i).isDropped(), "正常点不应被丢弃");
        }
    }

    @Test
    @DisplayName("精度过滤: accuracy=100(>50)的点被 DROPPED_ACCURACY")
    void testAccuracyFilterDropsBadPoint() {
        TrajectoryCleaner cleaner = new TrajectoryCleaner();
        // 第一个点精度正常
        CleanResult r1 = cleaner.clean(linearPoint(0, 10.0, 10.0));
        assertEquals(CleanResult.Action.PASSTHROUGH, r1.getAction());

        // 第二个点精度 100 > 50 → 丢弃
        GPSPoint badPoint = linearPoint(1, 10.0, 100.0);
        CleanResult result = cleaner.clean(badPoint);

        assertEquals(CleanResult.Action.DROPPED_ACCURACY, result.getAction(),
                "accuracy=100>50 应被精度过滤丢弃");
        assertTrue(result.isDropped(), "应被丢弃");
        assertFalse(result.hasOutput(), "不应有输出点");
    }

    @Test
    @DisplayName("飞点检测: 先发10个正常点填充窗口，再发速度超限点(跳5km)检测到异常")
    void testFlyPointDetection() {
        TrajectoryCleaner cleaner = new TrajectoryCleaner();
        // 10 个正常点填充窗口
        List<GPSPoint> normal = buildLinearTrack(10, 10.0, 10.0);
        List<CleanResult> normalResults = feedAll(cleaner, normal);
        for (CleanResult r : normalResults) {
            assertEquals(CleanResult.Action.PASSTHROUGH, r.getAction(), "正常点应通过");
        }

        // 飞点：从最后一个正常点跳 5km，1 秒到达 → 5000 m/s 远超 41.67
        GPSPoint lastNormal = normal.get(normal.size() - 1);
        GPSPoint fly = jumpFrom(lastNormal, 5000.0, lastNormal.getTimestamp() + 1000L);
        CleanResult result = cleaner.clean(fly);

        assertNotEquals(CleanResult.Action.PASSTHROUGH, result.getAction(), "飞点应被检测为异常");
        // 默认 replace 策略 → REPLACED_ANOMALY 且有输出
        assertEquals(CleanResult.Action.REPLACED_ANOMALY, result.getAction(),
                "默认 replace 策略下飞点应为 REPLACED_ANOMALY");
        assertTrue(result.hasOutput(), "替代策略应有输出点");
    }

    @Test
    @DisplayName("伪静止检测: 正常移动后连续发10个几乎不动(0.1m)的点进入 STATIC 状态")
    void testPseudoStaticDetection() {
        // 使用较大统计窗口，避免异常检测窗口满后干扰状态机判定
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(20)
                .maxAccuracy(50)
                .build();
        TrajectoryCleaner cleaner = new TrajectoryCleaner(config);

        // 先正常移动 3 个点（每步 20m > R=15）
        for (int i = 0; i < 3; i++) {
            cleaner.clean(linearPoint(i, 20.0, 10.0));
        }

        // 然后连续发 10 个几乎不动的点（每步 0.1m，相对上一点）
        // 注意：状态机在距离<R 时不会更新 lastValidPoint，因此距离始终从最近一个运动点计算
        // 3 个运动点把 lastValidPoint 带到 40m 处(m3)，近距点须放在 40m 附近
        CleanResult lastResult = null;
        for (int i = 0; i < 10; i++) {
            // 这些点在 lastValidPoint(m3=40m) 附近小幅摆动
            GPSPoint near = pointAt(40.0 + i * 0.1, BASE_TS + (3 + i) * 1000L, 10.0);
            lastResult = cleaner.clean(near);
        }
        // 第 10 个近距点应触发进入 STATIC
        assertNotNull(lastResult);
        assertEquals(CleanResult.MotionState.STATIC, lastResult.getCurrentState(),
                "连续10个近距点后应进入 STATIC 状态");

        // 再发一个近距点 → DROPPED_STATIC（确认处于静止状态）
        GPSPoint holdPoint = pointAt(40.5, BASE_TS + 13_000L, 10.0);
        CleanResult holdResult = cleaner.clean(holdPoint);
        assertEquals(CleanResult.Action.DROPPED_STATIC, holdResult.getAction(),
                "STATIC 状态下近距点应被 DROPPED_STATIC");
        assertTrue(holdResult.isDropped());
    }

    @Test
    @DisplayName("状态切换: STATIC 后连续 motionConfirmCount 个远距离点恢复 MOVING")
    void testStaticToMovingTransition() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(20) // 大窗口避免异常检测干扰
                .maxAccuracy(50)
                .build();
        TrajectoryCleaner cleaner = new TrajectoryCleaner(config);

        // 正常移动 3 点
        for (int i = 0; i < 3; i++) {
            cleaner.clean(linearPoint(i, 20.0, 10.0));
        }
        // 10 个近距点 → STATIC
        // 3 个运动点把 lastValidPoint 带到 40m 处(m3)，近距点须放在 40m 附近
        for (int i = 0; i < 10; i++) {
            cleaner.clean(pointAt(40.0 + i * 0.1, BASE_TS + (3 + i) * 1000L, 10.0));
        }

        // 连续发 motionConfirmCount(默认3) 个远距离点
        CleanResult transitionResult = null;
        for (int i = 0; i < config.getMotionConfirmCount(); i++) {
            GPSPoint far = pointAt(80.0 + i * 20.0, BASE_TS + (13 + i) * 1000L, 10.0);
            transitionResult = cleaner.clean(far);
        }
        assertNotNull(transitionResult);
        assertEquals(CleanResult.MotionState.MOVING, transitionResult.getCurrentState(),
                "经过 motionConfirmCount 次确认后应恢复 MOVING");

        // 再发一个正常运动点，应正常通过（非静止丢弃）
        GPSPoint moving = pointAt(160.0, BASE_TS + 17_000L, 10.0);
        CleanResult movingResult = cleaner.clean(moving);
        assertNotEquals(CleanResult.Action.DROPPED_STATIC, movingResult.getAction(),
                "恢复 MOVING 后正常点不应被静止丢弃");
    }

    @Test
    @DisplayName("异常替代策略: anomalyStrategy=replace 时异常点返回 REPLACED_ANOMALY 且有输出")
    void testReplaceAnomalyStrategy() {
        CleanerConfig config = new CleanerConfig.Builder()
                .anomalyStrategy("replace")
                .maxAccuracy(50)
                .build();
        TrajectoryCleaner cleaner = new TrajectoryCleaner(config);

        // 2 个正常点建立 lastValidOutput
        GPSPoint p1 = linearPoint(0, 10.0, 10.0);
        GPSPoint p2 = linearPoint(1, 10.0, 10.0);
        cleaner.clean(p1);
        cleaner.clean(p2);

        // 飞点：从 p2 跳 5km → 速度超限 → 替代
        GPSPoint fly = jumpFrom(p2, 5000.0, p2.getTimestamp() + 1000L);
        CleanResult result = cleaner.clean(fly);

        assertEquals(CleanResult.Action.REPLACED_ANOMALY, result.getAction(),
                "replace 策略下异常应为 REPLACED_ANOMALY");
        assertTrue(result.hasOutput(), "应有替代输出点");
        assertNotNull(result.getOutputPoint());
        // 替代点坐标 = 上一有效点(p2) 的坐标
        assertEquals(p2.getLatitude(), result.getOutputPoint().getLatitude(), 1e-9,
                "替代点坐标应为上一有效点");
        // 替代点时间戳 = 当前异常点的时间戳
        assertEquals(fly.getTimestamp(), result.getOutputPoint().getTimestamp(),
                "替代点时间戳应为当前异常点时间戳");
    }

    @Test
    @DisplayName("Reset: reset() 后状态清零，首个点按初始状态处理")
    void testResetClearsState() {
        CleanerConfig config = new CleanerConfig.Builder()
                .statsWindowSize(20)
                .maxAccuracy(50)
                .build();
        TrajectoryCleaner cleaner = new TrajectoryCleaner(config);

        // 正常移动 + 近距点进入 STATIC
        // 3 个运动点把 lastValidPoint 带到 40m 处(m3)，近距点须放在 40m 附近
        for (int i = 0; i < 3; i++) {
            cleaner.clean(linearPoint(i, 20.0, 10.0));
        }
        for (int i = 0; i < 10; i++) {
            cleaner.clean(pointAt(40.0 + i * 0.1, BASE_TS + (3 + i) * 1000L, 10.0));
        }
        // 此时处于 STATIC，发近距点应被丢弃
        CleanResult beforeReset = cleaner.clean(pointAt(40.5, BASE_TS + 13_000L, 10.0));
        assertEquals(CleanResult.Action.DROPPED_STATIC, beforeReset.getAction(),
                "reset 前应处于 STATIC，近距点被丢弃");

        // reset
        cleaner.reset();

        // reset 后发一个点，应像首个点一样 PASSTHROUGH 且状态为 MOVING
        GPSPoint newPoint = pointAt(0, BASE_TS + 20_000L, 10.0);
        CleanResult afterReset = cleaner.clean(newPoint);
        assertEquals(CleanResult.Action.PASSTHROUGH, afterReset.getAction(),
                "reset 后首个点应 PASSTHROUGH");
        assertEquals(CleanResult.MotionState.MOVING, afterReset.getCurrentState(),
                "reset 后状态应恢复为 MOVING");
        assertTrue(afterReset.hasOutput());
    }

    @Test
    @DisplayName("默认构造器与指定配置构造器均可用")
    void testConstructors() {
        TrajectoryCleaner defaultCleaner = new TrajectoryCleaner();
        assertNotNull(defaultCleaner.getConfig());
        assertEquals(50, defaultCleaner.getConfig().getMaxAccuracy(), 0.0001);

        CleanerConfig custom = new CleanerConfig.Builder().maxAccuracy(30).build();
        TrajectoryCleaner customCleaner = new TrajectoryCleaner(custom);
        assertEquals(30, customCleaner.getConfig().getMaxAccuracy(), 0.0001);

        // 两者均能正常清洗
        CleanResult r1 = defaultCleaner.clean(linearPoint(0, 10.0, 10.0));
        assertEquals(CleanResult.Action.PASSTHROUGH, r1.getAction());

        CleanResult r2 = customCleaner.clean(linearPoint(0, 10.0, 10.0));
        assertEquals(CleanResult.Action.PASSTHROUGH, r2.getAction());
    }
}
