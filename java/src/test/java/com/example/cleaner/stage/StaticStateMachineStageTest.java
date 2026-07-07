package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * StaticStateMachineStage 伪静止状态机单元测试
 */
class StaticStateMachineStageTest {

    private static final double LAT = 39.9087;
    private static final double LON = 116.3975;
    private static final long BASE_TS = 1_700_000_000_000L;

    /** 纬度方向 1 米对应的度数（近似） */
    private static final double METER_TO_DEG = 1.0 / 111_320.0;

    private PipelineContext context;
    private CleanerConfig config;
    private StaticStateMachineStage stage;

    /**
     * 使用较小的队列与确认计数以便测试快速触发状态切换
     * staticQueueSize=3, motionConfirmCount=2, R=15m
     */
    @BeforeEach
    void setUp() {
        config = new CleanerConfig.Builder()
                .staticQueueSize(3)
                .staticDistanceThreshold(15)
                .motionConfirmCount(2)
                .maxAccuracy(-1) // 关闭精度过滤，避免干扰
                .build();
        context = new PipelineContext();
        stage = new StaticStateMachineStage(config);
    }

    /** 在起点附近向北移动 meters 米 */
    private GPSPoint moveNorth(double meters, long ts) {
        return new GPSPoint.Builder("dev-1", LAT + meters * METER_TO_DEG, LON, ts).build();
    }

    /** 在起点附近向北移动 meters 米（相对起点） */
    private GPSPoint pointAtMeters(double meters, long ts) {
        return new GPSPoint.Builder("dev-1", LAT + meters * METER_TO_DEG, LON, ts).build();
    }

    @Test
    @DisplayName("MOVING 状态下距离>R 的点保持 MOVING")
    void testMovingStateKeepsMoving() {
        // 第一个点初始化为 MOVING
        GPSPoint p1 = pointAtMeters(0, BASE_TS);
        CleanResult r1 = stage.process(p1, context);
        assertEquals(CleanResult.MotionState.MOVING, r1.getCurrentState());
        assertEquals(CleanResult.Action.PASSTHROUGH, r1.getAction());

        // 第二个点距离 20m > R=15，仍 MOVING
        GPSPoint p2 = pointAtMeters(20, BASE_TS + 1000);
        CleanResult r2 = stage.process(p2, context);
        assertEquals(CleanResult.MotionState.MOVING, r2.getCurrentState(), "距离>R 应保持 MOVING");
        assertEquals(CleanResult.Action.PASSTHROUGH, r2.getAction());
        assertTrue(r2.hasOutput());
    }

    @Test
    @DisplayName("MOVING 状态下连续 staticQueueSize 个距离<R 的点进入 STATIC")
    void testMovingToStatic() {
        // p1 初始化
        stage.process(pointAtMeters(0, BASE_TS), context);

        // 连续 3 个几乎不动的点（每次 0.1m，远小于 R=15）
        // 距离始终从 lastValidPoint(起点) 计算：0.1, 0.2, 0.3 m
        CleanResult r1 = stage.process(pointAtMeters(0.1, BASE_TS + 1000), context);
        assertEquals(CleanResult.Action.PASSTHROUGH, r1.getAction(), "第1个近距点应 PASSTHROUGH");
        assertEquals(CleanResult.MotionState.PENDING_STATIC, r1.getCurrentState(), "应进入 PENDING_STATIC");

        CleanResult r2 = stage.process(pointAtMeters(0.2, BASE_TS + 2000), context);
        assertEquals(CleanResult.Action.PASSTHROUGH, r2.getAction(), "第2个近距点应 PASSTHROUGH");

        // 第 3 个近距点触发 STATIC
        CleanResult r3 = stage.process(pointAtMeters(0.3, BASE_TS + 3000), context);
        assertEquals(CleanResult.Action.STATE_TRANSITION, r3.getAction(), "第3个近距点应触发状态切换");
        assertEquals(CleanResult.MotionState.STATIC, r3.getCurrentState(), "应进入 STATIC");
        // stopPoint 应为进入静止前的最后一个有效点（起点）
        assertNotNull(stage.getStopPoint(), "stopPoint 不应为空");
        assertEquals(LAT, stage.getStopPoint().getLatitude(), 1e-9, "stopPoint 应为起点");
    }

    @Test
    @DisplayName("STATIC 状态下的点被 DROPPED_STATIC")
    void testStaticStateDropped() {
        // 先建立 STATIC 状态
        stage.process(pointAtMeters(0, BASE_TS), context);
        stage.process(pointAtMeters(0.1, BASE_TS + 1000), context);
        stage.process(pointAtMeters(0.2, BASE_TS + 2000), context);
        CleanResult rStatic = stage.process(pointAtMeters(0.3, BASE_TS + 3000), context);
        assertEquals(CleanResult.MotionState.STATIC, rStatic.getCurrentState());

        // STATIC 状态下再发一个近距点 → DROPPED_STATIC
        GPSPoint holdPoint = pointAtMeters(0.4, BASE_TS + 4000);
        CleanResult result = stage.process(holdPoint, context);
        assertEquals(CleanResult.Action.DROPPED_STATIC, result.getAction(), "STATIC 下近距点应被丢弃");
        assertEquals(CleanResult.MotionState.STATIC, result.getCurrentState(), "状态应保持 STATIC");
        assertTrue(result.isDropped(), "应被丢弃");
        assertFalse(result.hasOutput(), "不应有输出点");
    }

    @Test
    @DisplayName("STATIC 状态下连续 motionConfirmCount 个距离>R 的点恢复 MOVING")
    void testStaticToMoving() {
        // 先建立 STATIC 状态
        stage.process(pointAtMeters(0, BASE_TS), context);
        stage.process(pointAtMeters(0.1, BASE_TS + 1000), context);
        stage.process(pointAtMeters(0.2, BASE_TS + 2000), context);
        stage.process(pointAtMeters(0.3, BASE_TS + 3000), context);
        assertEquals(CleanResult.MotionState.STATIC, context.getMotionState());

        // 第 1 个远距点(20m>R)：等待确认，DROPPED_STATIC
        GPSPoint far1 = pointAtMeters(20, BASE_TS + 4000);
        CleanResult r1 = stage.process(far1, context);
        assertEquals(CleanResult.Action.DROPPED_STATIC, r1.getAction(), "第1个远距点应等待确认被丢弃");
        assertEquals(CleanResult.MotionState.STATIC, r1.getCurrentState(), "确认期间保持 STATIC");

        // 第 2 个远距点(40m>R)：达到 motionConfirmCount=2，恢复 MOVING
        GPSPoint far2 = pointAtMeters(40, BASE_TS + 5000);
        CleanResult r2 = stage.process(far2, context);
        assertEquals(CleanResult.Action.STATE_TRANSITION, r2.getAction(), "第2个远距点应触发状态切换");
        assertEquals(CleanResult.MotionState.MOVING, r2.getCurrentState(), "应恢复 MOVING");
        assertTrue(r2.hasOutput(), "状态切换应有输出点");
        assertNull(stage.getStopPoint(), "恢复 MOVING 后 stopPoint 应清空");
    }

    @Test
    @DisplayName("PENDING_STATIC 状态下出现距离>R 的点回到 MOVING")
    void testPendingStaticBackToMoving() {
        // p1 初始化
        stage.process(pointAtMeters(0, BASE_TS), context);
        // 1 个近距点 → PENDING_STATIC
        CleanResult r1 = stage.process(pointAtMeters(0.1, BASE_TS + 1000), context);
        assertEquals(CleanResult.MotionState.PENDING_STATIC, r1.getCurrentState());

        // 立刻出现一个远距点(20m>R) → 回到 MOVING
        CleanResult r2 = stage.process(pointAtMeters(20, BASE_TS + 2000), context);
        assertEquals(CleanResult.Action.PASSTHROUGH, r2.getAction());
        assertEquals(CleanResult.MotionState.MOVING, r2.getCurrentState(), "PENDING 下远距点应回到 MOVING");
    }

    @Test
    @DisplayName("getName: 返回 StaticStateMachine")
    void testGetName() {
        assertEquals("StaticStateMachine", stage.getName());
    }
}
