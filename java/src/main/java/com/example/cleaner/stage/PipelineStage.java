package com.example.cleaner.stage;

import com.example.cleaner.model.CleanResult;
import com.example.cleaner.model.GPSPoint;

/**
 * Pipeline 处理阶段接口
 * 每个 Stage 接收一个 GPS 点和上下文，返回清洗结果
 */
public interface PipelineStage {

    /**
     * 处理单个 GPS 点
     *
     * @param point    待处理的 GPS 点
     * @param context  Pipeline 上下文（跨 Stage 共享状态）
     * @return         清洗结果
     */
    CleanResult process(GPSPoint point, PipelineContext context);

    /**
     * Stage 名称
     */
    String getName();
}
