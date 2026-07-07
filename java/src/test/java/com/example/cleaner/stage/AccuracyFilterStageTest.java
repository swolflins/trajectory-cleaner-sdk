package com.example.cleaner.stage;

import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * AccuracyFilterStage 精度过滤阶段单元测试
 */
class AccuracyFilterStageTest {

    private static final double LAT = 39.9087;
    private static final double LON = 116.3975;
    private static final long BASE_TS = 1_700_000_000_000L;

    private PipelineContext context;

    @BeforeEach
    void setUp() {
        context = new PipelineContext();
    }

    private GPSPoint pointWithAccuracy(double accuracy) {
        return new GPSPoint.Builder("dev-1", LAT, LON, BASE_TS).accuracy(accuracy).build();
    }

    private GPSPoint pointWithoutAccuracy() {
        return new GPSPoint.Builder("dev-1", LAT, LON, BASE_TS).build();
    }

    @Test
    @DisplayName("精度合格(10<50)的点应 PASSTHROUGH")
    void testGoodAccuracyPassthrough() {
        CleanerConfig config = CleanerConfig.defaultConfig();
        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        GPSPoint point = pointWithAccuracy(10.0);
        CleanResult result = stage.process(point, context);

        assertEquals(CleanResult.Action.PASSTHROUGH, result.getAction(), "accuracy=10<50 应通过");
        assertFalse(result.isDropped(), "不应被丢弃");
        assertTrue(result.hasOutput(), "应有输出点");
        assertSame(point, result.getOutputPoint(), "输出点应为原始点");
    }

    @Test
    @DisplayName("精度不合格(100>50)的点应 DROPPED_ACCURACY")
    void testBadAccuracyDropped() {
        CleanerConfig config = CleanerConfig.defaultConfig();
        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        GPSPoint point = pointWithAccuracy(100.0);
        CleanResult result = stage.process(point, context);

        assertEquals(CleanResult.Action.DROPPED_ACCURACY, result.getAction(), "accuracy=100>50 应被丢弃");
        assertTrue(result.isDropped(), "应被丢弃");
        assertFalse(result.hasOutput(), "不应有输出点");
        assertNotNull(result.getReason(), "丢弃原因不应为空");
        assertTrue(result.getReason().contains("accuracy"), "原因应包含 accuracy 信息");
    }

    @Test
    @DisplayName("无精度字段(accuracy=-1)的点应 PASSTHROUGH")
    void testNoAccuracyPassthrough() {
        CleanerConfig config = CleanerConfig.defaultConfig();
        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        GPSPoint point = pointWithoutAccuracy();
        // 确认无精度字段
        assertFalse(point.hasAccuracy(), "测试点应无精度字段");

        CleanResult result = stage.process(point, context);

        assertEquals(CleanResult.Action.PASSTHROUGH, result.getAction(), "无精度字段应通过");
        assertFalse(result.isDropped(), "不应被丢弃");
        assertTrue(result.hasOutput(), "应有输出点");
    }

    @Test
    @DisplayName("maxAccuracy=-1(关闭过滤)时所有点通过，包括精度100的点")
    void testFilterDisabledAllPass() {
        CleanerConfig config = new CleanerConfig.Builder().maxAccuracy(-1).build();
        assertFalse(config.isAccuracyFilterEnabled(), "maxAccuracy=-1 应关闭过滤");

        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        // 精度超大的点也能通过
        GPSPoint badPoint = pointWithAccuracy(100.0);
        CleanResult result = stage.process(badPoint, context);

        assertEquals(CleanResult.Action.PASSTHROUGH, result.getAction(), "关闭过滤后 accuracy=100 也应通过");
        assertFalse(result.isDropped(), "不应被丢弃");
        assertTrue(result.hasOutput(), "应有输出点");

        // 无精度字段同样通过
        GPSPoint noAccPoint = pointWithoutAccuracy();
        CleanResult result2 = stage.process(noAccPoint, context);
        assertEquals(CleanResult.Action.PASSTHROUGH, result2.getAction(), "关闭过滤后无精度字段也应通过");
    }

    @Test
    @DisplayName("边界值: accuracy 恰好等于 maxAccuracy 时通过(未超过)")
    void testBoundaryEqualThreshold() {
        CleanerConfig config = CleanerConfig.defaultConfig(); // maxAccuracy=50
        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        GPSPoint point = pointWithAccuracy(50.0);
        CleanResult result = stage.process(point, context);

        // accuracy > threshold 才丢弃，等于不丢
        assertEquals(CleanResult.Action.PASSTHROUGH, result.getAction(),
                "accuracy=50 等于阈值(未超过)应通过");
        assertFalse(result.isDropped());
    }

    @Test
    @DisplayName("边界值: accuracy 略大于 maxAccuracy 时丢弃")
    void testBoundarySlightlyOverThreshold() {
        CleanerConfig config = CleanerConfig.defaultConfig(); // maxAccuracy=50
        AccuracyFilterStage stage = new AccuracyFilterStage(config);

        GPSPoint point = pointWithAccuracy(50.1);
        CleanResult result = stage.process(point, context);

        assertEquals(CleanResult.Action.DROPPED_ACCURACY, result.getAction(),
                "accuracy=50.1>50 应被丢弃");
        assertTrue(result.isDropped());
    }

    @Test
    @DisplayName("getName: 返回 AccuracyFilter")
    void testGetName() {
        AccuracyFilterStage stage = new AccuracyFilterStage(CleanerConfig.defaultConfig());
        assertEquals("AccuracyFilter", stage.getName());
    }
}
