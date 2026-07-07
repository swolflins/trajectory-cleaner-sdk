package com.example.cleaner.stage;

import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;

/**
 * Pipeline 上下文
 * 在各 Stage 之间共享状态信息
 *
 * 内部封装运动状态，避免暴露给外部
 */
public class PipelineContext {

    /** 运动状态值（包级可见，供同包 MotionStateWrapper 封装读写） */
    int motionStateValue = MotionStateWrapper.STATE_MOVING;

    /** 上一个有效输出点 (供后续 Stage 使用) */
    private GPSPoint lastOutputPoint = null;

    /**
     * 获取运动状态枚举
     */
    public CleanResult.MotionState getMotionState() {
        return MotionStateWrapper.toEnum(motionStateValue);
    }

    /**
     * 设置运动状态值
     */
    public void setMotionState(int value) {
        this.motionStateValue = value;
    }

    /**
     * 获取运动状态包装器
     */
    public MotionStateWrapper getMotionStateWrapper() {
        return new MotionStateWrapper(this);
    }

    /**
     * 获取上一个输出点
     */
    public GPSPoint getLastOutputPoint() {
        return lastOutputPoint;
    }

    /**
     * 设置上一个输出点
     */
    public void setLastOutputPoint(GPSPoint point) {
        this.lastOutputPoint = point;
    }
}
