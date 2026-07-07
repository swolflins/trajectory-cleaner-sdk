package com.example.cleaner.config;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * CleanerConfig 配置单元测试
 */
class CleanerConfigTest {

    @Test
    @DisplayName("defaultConfig: 默认配置值正确")
    void testDefaultConfigValues() {
        CleanerConfig config = CleanerConfig.defaultConfig();

        // Stage 1: 精度过滤
        assertEquals(50, config.getMaxAccuracy(), 0.0001, "默认 maxAccuracy=50");
        assertTrue(config.isAccuracyFilterEnabled(), "默认应启用精度过滤");

        // Stage 2: 伪静止状态机
        assertEquals(10, config.getStaticQueueSize(), "默认 staticQueueSize=10");
        assertEquals(15, config.getStaticDistanceThreshold(), 0.0001, "默认 staticDistanceThreshold=15");
        assertEquals(3, config.getMotionConfirmCount(), "默认 motionConfirmCount=3");

        // Stage 3: 异常检测
        assertEquals(10, config.getStatsWindowSize(), "默认 statsWindowSize=10");
        assertEquals("iqr", config.getAnomalyMethod(), "默认 anomalyMethod=iqr");
        assertTrue(config.isIqrMethod(), "默认应使用 IQR 方法");
        assertEquals(3.0, config.getInitialSigma(), 0.0001, "默认 initialSigma=3.0");
        assertEquals(2.0, config.getContinuousSigma(), 0.0001, "默认 continuousSigma=2.0");
        assertEquals(1.5, config.getIqrMultiplier(), 0.0001, "默认 iqrMultiplier=1.5");
        assertEquals(41.67, config.getMaxVelocity(), 0.001, "默认 maxVelocity=41.67");

        // Stage 4: 异常处理
        assertEquals("replace", config.getAnomalyStrategy(), "默认 anomalyStrategy=replace");
    }

    @Test
    @DisplayName("Builder: 链式调用可覆盖所有默认值")
    void testBuilderChaining() {
        CleanerConfig config = new CleanerConfig.Builder()
                .maxAccuracy(30)
                .staticQueueSize(5)
                .staticDistanceThreshold(10)
                .motionConfirmCount(2)
                .statsWindowSize(8)
                .anomalyMethod("zscore")
                .initialSigma(3.5)
                .continuousSigma(2.5)
                .iqrMultiplier(2.0)
                .maxVelocity(33.33)
                .anomalyStrategy("drop")
                .build();

        assertEquals(30, config.getMaxAccuracy(), 0.0001);
        assertEquals(5, config.getStaticQueueSize());
        assertEquals(10, config.getStaticDistanceThreshold(), 0.0001);
        assertEquals(2, config.getMotionConfirmCount());
        assertEquals(8, config.getStatsWindowSize());
        assertEquals("zscore", config.getAnomalyMethod());
        assertEquals(3.5, config.getInitialSigma(), 0.0001);
        assertEquals(2.5, config.getContinuousSigma(), 0.0001);
        assertEquals(2.0, config.getIqrMultiplier(), 0.0001);
        assertEquals(33.33, config.getMaxVelocity(), 0.001);
        assertEquals("drop", config.getAnomalyStrategy());
    }

    @Test
    @DisplayName("useZScore: 切换后 anomalyMethod=zscore, isIqrMethod=false")
    void testUseZScore() {
        CleanerConfig config = new CleanerConfig.Builder()
                .useZScore()
                .build();

        assertEquals("zscore", config.getAnomalyMethod(), "useZScore 后 anomalyMethod=zscore");
        assertFalse(config.isIqrMethod(), "useZScore 后 isIqrMethod 应为 false");
    }

    @Test
    @DisplayName("useIQR: 切换后 anomalyMethod=iqr, isIqrMethod=true")
    void testUseIQR() {
        // 先切换到 zscore，再切回 iqr
        CleanerConfig config = new CleanerConfig.Builder()
                .useZScore()
                .useIQR()
                .build();

        assertEquals("iqr", config.getAnomalyMethod(), "useIQR 后 anomalyMethod=iqr");
        assertTrue(config.isIqrMethod(), "useIQR 后 isIqrMethod 应为 true");
    }

    @Test
    @DisplayName("isAccuracyFilterEnabled: maxAccuracy>0 时为 true")
    void testIsAccuracyFilterEnabled() {
        CleanerConfig enabled = new CleanerConfig.Builder().maxAccuracy(50).build();
        assertTrue(enabled.isAccuracyFilterEnabled(), "maxAccuracy=50 应启用过滤");

        CleanerConfig disabled = new CleanerConfig.Builder().maxAccuracy(-1).build();
        assertFalse(disabled.isAccuracyFilterEnabled(), "maxAccuracy=-1 应关闭过滤");

        CleanerConfig zero = new CleanerConfig.Builder().maxAccuracy(0).build();
        assertFalse(zero.isAccuracyFilterEnabled(), "maxAccuracy=0 应关闭过滤");
    }

    @Test
    @DisplayName("isIqrMethod: 大小写不敏感匹配 iqr")
    void testIsIqrMethodCaseInsensitive() {
        CleanerConfig upper = new CleanerConfig.Builder().anomalyMethod("IQR").build();
        assertTrue(upper.isIqrMethod(), "anomalyMethod=IQR 应匹配 iqr（大小写不敏感）");

        CleanerConfig mixed = new CleanerConfig.Builder().anomalyMethod("Iqr").build();
        assertTrue(mixed.isIqrMethod(), "anomalyMethod=Iqr 应匹配 iqr");

        CleanerConfig zscore = new CleanerConfig.Builder().anomalyMethod("zscore").build();
        assertFalse(zscore.isIqrMethod(), "anomalyMethod=zscore 时 isIqrMethod=false");
    }

    @Test
    @DisplayName("Builder 返回的每个方法均返回 Builder 自身以支持链式调用")
    void testBuilderReturnsSelf() {
        CleanerConfig.Builder builder = new CleanerConfig.Builder();
        assertSame(builder, builder.maxAccuracy(10), "maxAccuracy 应返回 this");
        assertSame(builder, builder.staticQueueSize(5), "staticQueueSize 应返回 this");
        assertSame(builder, builder.staticDistanceThreshold(5), "staticDistanceThreshold 应返回 this");
        assertSame(builder, builder.motionConfirmCount(2), "motionConfirmCount 应返回 this");
        assertSame(builder, builder.statsWindowSize(5), "statsWindowSize 应返回 this");
        assertSame(builder, builder.anomalyMethod("zscore"), "anomalyMethod 应返回 this");
        assertSame(builder, builder.initialSigma(3), "initialSigma 应返回 this");
        assertSame(builder, builder.continuousSigma(2), "continuousSigma 应返回 this");
        assertSame(builder, builder.iqrMultiplier(1.5), "iqrMultiplier 应返回 this");
        assertSame(builder, builder.maxVelocity(40), "maxVelocity 应返回 this");
        assertSame(builder, builder.anomalyStrategy("drop"), "anomalyStrategy 应返回 this");
        assertSame(builder, builder.useZScore(), "useZScore 应返回 this");
        assertSame(builder, builder.useIQR(), "useIQR 应返回 this");

        // 链式调用结束后能正常构建
        assertNotNull(builder.build(), "build 后应返回非 null 配置");
    }

    @Test
    @DisplayName("toString: 包含关键配置信息")
    void testToString() {
        CleanerConfig config = CleanerConfig.defaultConfig();
        String str = config.toString();
        assertNotNull(str);
        assertTrue(str.contains("iqr"), "toString 应包含 anomalyMethod");
        assertTrue(str.contains("maxAcc"), "toString 应包含 maxAccuracy 信息");
    }
}
