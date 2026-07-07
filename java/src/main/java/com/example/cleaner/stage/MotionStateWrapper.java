package com.example.cleaner.stage;

import com.example.cleaner.model.CleanResult;

/**
 * 运动状态包装器
 * 用于在 PipelineContext 中封装运动状态的读写
 */
public class MotionStateWrapper {

    public static final int STATE_MOVING = 0;
    public static final int STATE_STATIC = 1;
    public static final int STATE_PENDING_STATIC = 2;

    private final PipelineContext context;

    public MotionStateWrapper(PipelineContext context) {
        this.context = context;
    }

    public int getValue() {
        return context.motionStateValue;
    }

    public void set(int value) {
        context.motionStateValue = value;
    }

    public CleanResult.MotionState get() {
        return toEnum(getValue());
    }

    public static CleanResult.MotionState toEnum(int value) {
        switch (value) {
            case STATE_MOVING: return CleanResult.MotionState.MOVING;
            case STATE_STATIC: return CleanResult.MotionState.STATIC;
            case STATE_PENDING_STATIC: return CleanResult.MotionState.PENDING_STATIC;
            default: return CleanResult.MotionState.MOVING;
        }
    }
}
