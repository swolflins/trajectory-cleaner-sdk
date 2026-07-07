package com.example.cleaner.model;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * GPSPoint 数据模型单元测试
 */
class GPSPointTest {

    /** 北京天安门广场大致坐标 */
    private static final double LAT_TIANANMEN = 39.9087;
    private static final double LON_TIANANMEN = 116.3975;

    @Test
    @DisplayName("distanceTo: 天安门到正北方向约1km处距离应接近1000米")
    void testDistanceToBeijingLandmark() {
        // 天安门
        GPSPoint tianAnMen = new GPSPoint.Builder("dev-1", LAT_TIANANMEN, LON_TIANANMEN, 0L)
                .accuracy(10)
                .speed(5)
                .heading(0)
                .build();
        // 正北约 1km：纬度差 0.009 度 ≈ 1000 米
        GPSPoint north1km = new GPSPoint.Builder("dev-1", LAT_TIANANMEN + 0.009, LON_TIANANMEN, 1000L)
                .build();

        double distance = tianAnMen.distanceTo(north1km);
        // Haversine 计算结果约 1000 米，允许 10 米误差
        assertEquals(1000.0, distance, 10.0,
                () -> "天安门到正北1km处距离应接近1000米，实际=" + distance);
        assertTrue(distance > 990 && distance < 1010, "距离应在 990~1010 米之间");
    }

    @Test
    @DisplayName("distanceTo: 相同点距离为0")
    void testDistanceToSamePoint() {
        GPSPoint p = new GPSPoint.Builder("dev-1", 39.9, 116.4, 0L).build();
        assertEquals(0.0, p.distanceTo(p), 0.0001, "相同点距离应为0");
    }

    @Test
    @DisplayName("velocityTo: 1000米/60秒 = 16.67 m/s")
    void testVelocityTo() {
        GPSPoint start = new GPSPoint.Builder("dev-1", LAT_TIANANMEN, LON_TIANANMEN, 0L).build();
        // 60 秒后到达 1km 外
        GPSPoint end = new GPSPoint.Builder("dev-1", LAT_TIANANMEN + 0.009, LON_TIANANMEN, 60_000L).build();

        double velocity = start.velocityTo(end);
        // 1000m / 60s ≈ 16.67 m/s
        assertEquals(16.67, velocity, 0.2,
                () -> "1000米/60秒速度应接近16.67 m/s，实际=" + velocity);
    }

    @Test
    @DisplayName("velocityTo: 时间差为0时速度为0（避免除零）")
    void testVelocityToZeroTimeDiff() {
        GPSPoint p1 = new GPSPoint.Builder("dev-1", 39.9, 116.4, 1000L).build();
        GPSPoint p2 = new GPSPoint.Builder("dev-1", 39.91, 116.41, 1000L).build();
        assertEquals(0.0, p1.velocityTo(p2), 0.0001, "时间差为0时速度应为0");
    }

    @Test
    @DisplayName("timeDiffSeconds: 两个点时间差60秒")
    void testTimeDiffSeconds() {
        GPSPoint p1 = new GPSPoint.Builder("dev-1", 39.9, 116.4, 1_700_000_000_000L).build();
        GPSPoint p2 = new GPSPoint.Builder("dev-1", 39.91, 116.41, 1_700_000_060_000L).build();

        assertEquals(60.0, p1.timeDiffSeconds(p2), 0.001, "时间差应为60秒");
        // timeDiffSeconds 取绝对值，顺序无关
        assertEquals(60.0, p2.timeDiffSeconds(p1), 0.001, "时间差取绝对值，顺序无关");
    }

    @Test
    @DisplayName("hasAccuracy / hasSpeed: 设置有效值时返回 true")
    void testHasAccuracyAndSpeedPresent() {
        GPSPoint p = new GPSPoint.Builder("dev-1", 39.9, 116.4, 0L)
                .accuracy(10.0)
                .speed(5.0)
                .heading(90.0)
                .build();

        assertTrue(p.hasAccuracy(), "设置了 accuracy=10，应返回 true");
        assertTrue(p.hasSpeed(), "设置了 speed=5，应返回 true");
        assertEquals(10.0, p.getAccuracy(), 0.0001);
        assertEquals(5.0, p.getSpeed(), 0.0001);
        assertEquals(90.0, p.getHeading(), 0.0001);
    }

    @Test
    @DisplayName("hasAccuracy / hasSpeed: 未设置(默认-1)时返回 false")
    void testHasAccuracyAndSpeedAbsent() {
        GPSPoint p = new GPSPoint.Builder("dev-1", 39.9, 116.4, 0L).build();

        assertFalse(p.hasAccuracy(), "未设置 accuracy(默认-1)，应返回 false");
        assertFalse(p.hasSpeed(), "未设置 speed(默认-1)，应返回 false");
        assertEquals(-1.0, p.getAccuracy(), 0.0001, "默认 accuracy 应为 -1");
        assertEquals(-1.0, p.getSpeed(), 0.0001, "默认 speed 应为 -1");
        assertEquals(-1.0, p.getHeading(), 0.0001, "默认 heading 应为 -1");
    }

    @Test
    @DisplayName("withTimestamp: 新时间戳但坐标不变")
    void testWithTimestamp() {
        GPSPoint original = new GPSPoint.Builder("dev-1", 39.9087, 116.3975, 1_000L)
                .accuracy(8.0)
                .build();
        long newTs = 5_000L;
        GPSPoint replaced = original.withTimestamp(newTs);

        // 时间戳更新为新值
        assertEquals(newTs, replaced.getTimestamp(), "withTimestamp 后时间戳应为新值");
        // 坐标保持不变
        assertEquals(original.getLatitude(), replaced.getLatitude(), 0.0, "纬度不变");
        assertEquals(original.getLongitude(), replaced.getLongitude(), 0.0, "经度不变");
        assertEquals(original.getDeviceId(), replaced.getDeviceId(), "deviceId 不变");
        // 原对象不可变，时间戳仍为旧值
        assertEquals(1_000L, original.getTimestamp(), "原对象时间戳不变");
        assertNotSame(original, replaced, "withTimestamp 应返回新对象");
    }

    @Test
    @DisplayName("Builder: 必填字段正确赋值")
    void testBuilderRequiredFields() {
        GPSPoint p = new GPSPoint.Builder("device-007", 39.9163, 116.3972, 1_700_000_000_000L).build();

        assertEquals("device-007", p.getDeviceId());
        assertEquals(39.9163, p.getLatitude(), 0.0);
        assertEquals(116.3972, p.getLongitude(), 0.0);
        assertEquals(1_700_000_000_000L, p.getTimestamp());
    }

    @Test
    @DisplayName("distanceTo: 天安门到故宫(约960米)距离合理")
    void testDistanceToForbiddenCity() {
        // 天安门
        GPSPoint tianAnMen = new GPSPoint.Builder("dev-1", 39.9087, 116.3975, 0L).build();
        // 故宫神武门北侧大致坐标
        GPSPoint forbiddenCity = new GPSPoint.Builder("dev-1", 39.9163, 116.3972, 0L).build();

        double distance = tianAnMen.distanceTo(forbiddenCity);
        // 天安门到故宫约 850~960 米
        assertTrue(distance > 800 && distance < 1100,
                () -> "天安门到故宫距离应在 800~1100 米之间，实际=" + distance);
    }
}
