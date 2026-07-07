# 轨迹清洗 SDK 方案设计文档

| 版本 | 日期 | 状态 | 作者 |
|------|------|------|------|
| v1.0 | 2026-07-07 | Draft | Trajectory Cleaner SDK Team |

---

## 目录

1. [设计概述](#1-设计概述)
2. [整体架构](#2-整体架构)
3. [三阶段管线设计详解](#3-三阶段管线设计详解)
4. [数据模型设计](#4-数据模型设计)
5. [Go 与 Java 实现差异说明](#5-go-与-java-实现差异说明)
6. [扩展性设计](#6-扩展性设计)
7. [错误处理与边界条件](#7-错误处理与边界条件)
8. [并发与线程安全](#8-并发与线程安全)
9. [完整参数表](#9-完整参数表)
10. [相关文档](#10-相关文档)

---

## 1. 设计概述

### 1.1 设计目标

本设计文档基于 [01-requirements.md](./01-requirements.md) 的需求与 [02-research.md](./02-research.md) 的调研结论，给出轨迹清洗 SDK 的详细技术方案，指导 Go 与 Java 双语言实现。

核心设计目标：

1. **三阶段串行管线**：精度过滤 → 伪静止状态机 → 异常检测，职责单一、可独立测试
2. **双语言对等**：Go 与 Java 行为一致、数值结果可对拍
3. **配置驱动**：所有阈值参数化，支持场景化调优
4. **流式友好**：支持单点增量与批量两种处理模式
5. **可扩展**：Stage 接口化，支持自定义 Stage 与异常检测器

### 1.2 设计原则

| 原则 | 说明 |
|------|------|
| 职责单一 | 每个 Stage 只做一件事，不跨职责 |
| 数据不可变 | Stage 间传递不可变数据，避免副作用 |
| 确定性 | 相同输入 + 相同配置 = 相同输出 |
| 显式优于隐式 | 边界条件显式处理，不做隐式默认 |
| 失败安全 | 异常输入不 panic，降级为丢弃或透传 |

### 1.3 不做事项

- 不做绑路（依赖路网，见 [02-research.md](./02-research.md) 第 5、10 节）
- 不做补偿（依赖绑路，见 [02-research.md](./02-research.md) 第 6、10 节）
- 不做卡尔曼滤波（改变原始点位置，见 [02-research.md](./02-research.md) 第 3.4 节）
- 不做坐标系转换
- 不做持久化

---

## 2. 整体架构

### 2.1 分层架构

```
┌─────────────────────────────────────────────────────────┐
│                    应用层（用户代码）                     │
│  构造 Config → 调用 Pipeline.Process → 读 CleanResult    │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                    API 层（Pipeline）                    │
│  编排 Stage 顺序、聚合统计、返回 CleanResult             │
└─────────────────────────────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        ▼                 ▼                 ▼
┌──────────────┐  ┌────────────────┐  ┌──────────────┐
│ Stage 1      │  │ Stage 2        │  │ Stage 3      │
│ 精度过滤     │  │ 伪静止状态机   │  │ 异常检测     │
│ AccuracyFilter│ │ PseudoStaticSM │  │ OutlierDetect│
└──────────────┘  └────────────────┘  └──────────────┘
        │                 │                 │
        ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────────┐
│                    工具层（Utils）                       │
│  Haversine 距离、速度计算、统计量计算、Z-score、IQR     │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                    数据模型层（Model）                   │
│  GPSPoint、CleanResult、Config、ProcessStats、DropDetail │
└─────────────────────────────────────────────────────────┘
```

### 2.2 模块划分

| 模块 | 职责 | Go 包 | Java 包 |
|------|------|-------|---------|
| Model | 数据结构定义 | `model` | `com.trajectorycleaner.model` |
| Utils | 数学工具 | `internal/util` | `...util` |
| Stage1 | 精度过滤 | `stage/accuracy` | `...stage.accuracy` |
| Stage2 | 伪静止状态机 | `stage/pseudostatic` | `...stage.pseudostatic` |
| Stage3 | 异常检测 | `stage/outlier` | `...stage.outlier` |
| Pipeline | 编排与统计 | `pipeline` | `...pipeline` |
| API | 对外入口 | `cleaner` | `...cleaner` |

### 2.3 处理流程

```
输入 []GPSPoint（已按 Timestamp 升序）
  │
  │ ① 校验输入（空 / 单点特殊处理）
  ▼
AccuracyFilter.Process
  │  逐点判定 accuracy，输出"精度合格"子集
  │  统计 byAccuracy 丢弃数
  ▼
PseudoStaticDetector.Process
  │  状态机扫描，静止段仅留锚点
  │  统计 byPseudoStatic 丢弃数
  ▼
OutlierDetector.Process
  │  冷启动校准 → 持续检测 → 飞点处理（replace/drop）
  │  统计 byOutlierDrop / byOutlierReplace
  ▼
聚合 ProcessStats + DropDetail
  │
  ▼
返回 CleanResult
```

### 2.4 Stage 接口定义

```go
// Go
type Stage interface {
    // Process 处理一批点，返回处理后的点集
    Process(points []GPSPoint) []GPSPoint
    // Name 返回 Stage 名称，用于统计与日志
    Name() string
}
```

```java
// Java
public interface Stage {
    List<GPSPoint> process(List<GPSPoint> points);
    String name();
}
```

Pipeline 持有 `[]Stage`，依次调用 `Process`，串联输出。

---

## 3. 三阶段管线设计详解

### 3.1 Stage 1 精度过滤（AccuracyFilter）

#### 3.1.1 处理逻辑

精度过滤是最简单的 Stage，根据 `accuracy` 字段过滤低质量点。

**判定规则**（按顺序）：

1. 若 `accuracy <= 0`（无效值）：
   - `StrictMode=true` → 丢弃
   - `StrictMode=false` → 保留（标记为未校准，但仍参与后续 Stage）
2. 若 `accuracy > AccuracyThreshold` → 丢弃
3. 否则保留

**伪代码**：
```
function AccuracyFilter.Process(points):
    result = []
    for p in points:
        if p.accuracy <= 0:
            if config.StrictMode:
                drop(p); continue
            else:
                result.append(p); continue
        if p.accuracy > config.AccuracyThreshold:
            drop(p); continue
        result.append(p)
    return result
```

#### 3.1.2 参数

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `AccuracyThreshold` | float64 | 50 | 精度阈值（米） |
| `StrictMode` | bool | false | accuracy 无效时是否丢弃 |

#### 3.1.3 边界条件

| 输入情况 | 处理 |
|---------|------|
| 空轨迹 | 返回空，统计 0 |
| 单点 | 正常判定 |
| 所有点不达标 | 返回空，统计全部丢弃 |
| accuracy 全为 0 且 StrictMode=false | 全部保留，透传 |

#### 3.1.4 实现要点

- O(n) 单次遍历
- 无状态，线程安全
- 不修改输入切片（返回新切片或视图）

```go
// Go 实现骨架
type AccuracyFilter struct {
    cfg Config
}

func (f *AccuracyFilter) Process(points []GPSPoint) []GPSPoint {
    out := make([]GPSPoint, 0, len(points))
    for _, p := range points {
        if p.Accuracy <= 0 {
            if f.cfg.StrictMode {
                continue // drop
            }
            out = append(out, p)
            continue
        }
        if p.Accuracy > f.cfg.AccuracyThreshold {
            continue // drop
        }
        out = append(out, p)
    }
    return out
}
```

```java
// Java 实现骨架
public class AccuracyFilter implements Stage {
    private final Config cfg;

    public AccuracyFilter(Config cfg) { this.cfg = cfg; }

    @Override
    public List<GPSPoint> process(List<GPSPoint> points) {
        List<GPSPoint> out = new ArrayList<>(points.size());
        for (GPSPoint p : points) {
            if (p.getAccuracy() <= 0) {
                if (cfg.isStrictMode()) continue; // drop
                out.add(p);
                continue;
            }
            if (p.getAccuracy() > cfg.getAccuracyThreshold()) continue; // drop
            out.add(p);
        }
        return out;
    }
}
```

---

### 3.2 Stage 2 伪静止状态机（PseudoStaticDetector）

#### 3.2.1 设计动机

设备静止时 GPS 持续输出抖动点（见 [02-research.md](./02-research.md) 第 2.2.3 节），导致：
- 静止段里程虚增
- 停留点识别困难
- 数据膨胀

伪静止状态机通过"队列计数 + 距离阈值 + 状态机"平滑处理，静止段仅保留锚点。

#### 3.2.2 状态定义

| 状态 | 含义 | 输出行为 |
|------|------|---------|
| `MOVING` | 运动中 | 正常输出每个点 |
| `PENDING_STATIC` | 待定静止 | 暂存队列，不立即输出 |
| `STATIC` | 确认静止 | 仅输出进入静止的锚点，后续丢弃 |

注：`MOVING` 既是初始状态，也是从 `STATIC` 恢复后的状态。

#### 3.2.3 状态转换图

```
                          位移 < MinDisplacement
                          连续 >= MinStaticPoints 次
              ┌──────────────────────────────────┐
              │                                   │
              ▼                                   │
         ┌─────────┐     位移 < MinDisplacement    │
         │ MOVING  │──────────────────────────▶┌──────────────┐
         │ (初始)  │                           │PENDING_STATIC │
         └─────────┘                           └──────────────┘
              ▲                                   │       │
              │                                   │       │ 位移 >= MinDisplacement
              │                                   │       │ (打破连续)
              │                                   │       ▼
              │                                   │   回退到 MOVING
              │                                   │   并补回暂存点
              │                       连续 >=     │
              │                     MinStaticPoints│
              │                                   ▼
              │                              ┌────────┐
              │  位移 >= ResumeDisplacement    │ STATIC │
              └──────────────────────────────│ (静止) │
                                             └────────┘
                                              │
                                              │ 静止期间：
                                              │ 仅保留锚点
                                              │ 后续点丢弃
```

#### 3.2.4 状态转换规则（详表）

| 当前状态 | 条件 | 目标状态 | 动作 |
|---------|------|---------|------|
| MOVING | 位移 < MinDisplacement | PENDING_STATIC | 当前点入暂存队列 |
| MOVING | 位移 >= MinDisplacement | MOVING | 输出当前点 |
| PENDING_STATIC | 位移 < MinDisplacement 且队列长度 < MinStaticPoints | PENDING_STATIC | 当前点入队列 |
| PENDING_STATIC | 位移 < MinDisplacement 且队列长度 >= MinStaticPoints | STATIC | 输出队列第一个点（锚点），丢弃其余 |
| PENDING_STATIC | 位移 >= MinDisplacement | MOVING | 补回队列所有点 + 当前点 |
| STATIC | 位移 < ResumeDisplacement | STATIC | 丢弃当前点 |
| STATIC | 位移 >= ResumeDisplacement | MOVING | 输出当前点（恢复运动起点） |

**位移定义**：当前点与"上一输出点"（lastOutputPoint）的大圆距离。

#### 3.2.5 参数含义

| 参数 | 类型 | 默认 | 含义 | 调参影响 |
|------|------|------|------|---------|
| `MinStaticPoints` | int | 5 | 确认静止所需连续低位移点数 | 增大→更难确认静止（减少误判静止）；减小→更易确认（可能误杀低速段） |
| `MinDisplacement` | float64 | 3 | 单点位移阈值（米） | 增大→更易判为静止；减小→更难判静止 |
| `ResumeDisplacement` | float64 | 10 | 恢复运动位移阈值（米） | 增大→更难恢复（静止段更长）；减小→更易恢复 |

**约束**：`ResumeDisplacement > MinDisplacement`，否则状态机无法正常恢复（永远卡在 STATIC）。

#### 3.2.6 边界条件

| 输入情况 | 处理 |
|---------|------|
| 空轨迹 | 返回空 |
| 单点 | 透传（状态机不触发） |
| 两点 | 按位移判定，可能进入 PENDING_STATIC 但不会到 STATIC（需 >= MinStaticPoints 点） |
| 全程静止 | 仅输出第一个锚点 |
| 全程运动 | 全部输出 |
| 静止 → 运动 → 静止 | 两个锚点 + 运动段 |

#### 3.2.7 实现要点

- **有状态**：状态机跨点保持状态，单实例非线程安全
- **暂存队列**：PENDING_STATIC 阶段缓存点，确认后丢弃或补回
- **流式支持**：可改造为逐点 `ProcessPoint(p)` 增量接口

```go
// Go 实现骨架
type pseudoStaticState int

const (
    stateMoving pseudoStaticState = iota
    statePendingStatic
    stateStatic
)

type PseudoStaticDetector struct {
    cfg             Config
    state           pseudoStaticState
    lastOutput      *GPSPoint
    pendingQueue    []GPSPoint
    anchorOutput    bool // STATIC 阶段是否已输出锚点
}

func (d *PseudoStaticDetector) Process(points []GPSPoint) []GPSPoint {
    out := make([]GPSPoint, 0, len(points))
    for i := range points {
        p := points[i]
        if d.lastOutput == nil {
            d.lastOutput = &p
            out = append(out, p)
            continue
        }
        disp := haversine(d.lastOutput, &p)
        switch d.state {
        case stateMoving:
            if disp < d.cfg.MinDisplacement {
                d.state = statePendingStatic
                d.pendingQueue = append(d.pendingQueue, p)
            } else {
                out = append(out, p)
                d.lastOutput = &p
            }
        case statePendingStatic:
            if disp < d.cfg.MinDisplacement {
                d.pendingQueue = append(d.pendingQueue, p)
                if len(d.pendingQueue) >= d.cfg.MinStaticPoints {
                    // 确认静止：输出队列首点为锚点
                    anchor := d.pendingQueue[0]
                    out = append(out, anchor)
                    d.lastOutput = &anchor
                    d.anchorOutput = true
                    d.state = stateStatic
                    d.pendingQueue = nil
                }
            } else {
                // 打破连续：补回暂存点 + 当前点
                out = append(out, d.pendingQueue...)
                d.pendingQueue = nil
                out = append(out, p)
                d.lastOutput = &p
                d.state = stateMoving
            }
        case stateStatic:
            if disp >= d.cfg.ResumeDisplacement {
                out = append(out, p)
                d.lastOutput = &p
                d.state = stateMoving
            }
            // else: 丢弃（静止抖动）
        }
    }
    // 收尾：PENDING_STATIC 未确认则补回
    if d.state == statePendingStatic && len(d.pendingQueue) > 0 {
        out = append(out, d.pendingQueue...)
    }
    return out
}
```

```java
// Java 实现骨架
public class PseudoStaticDetector implements Stage {
    private enum State { MOVING, PENDING_STATIC, STATIC }

    private final Config cfg;
    private State state = State.MOVING;
    private GPSPoint lastOutput;
    private final List<GPSPoint> pendingQueue = new ArrayList<>();

    public PseudoStaticDetector(Config cfg) { this.cfg = cfg; }

    @Override
    public List<GPSPoint> process(List<GPSPoint> points) {
        List<GPSPoint> out = new ArrayList<>(points.size());
        for (GPSPoint p : points) {
            if (lastOutput == null) {
                lastOutput = p;
                out.add(p);
                continue;
            }
            double disp = Util.haversine(lastOutput, p);
            switch (state) {
                case MOVING:
                    if (disp < cfg.getMinDisplacement()) {
                        state = State.PENDING_STATIC;
                        pendingQueue.add(p);
                    } else {
                        out.add(p);
                        lastOutput = p;
                    }
                    break;
                case PENDING_STATIC:
                    if (disp < cfg.getMinDisplacement()) {
                        pendingQueue.add(p);
                        if (pendingQueue.size() >= cfg.getMinStaticPoints()) {
                            GPSPoint anchor = pendingQueue.get(0);
                            out.add(anchor);
                            lastOutput = anchor;
                            state = State.STATIC;
                            pendingQueue.clear();
                        }
                    } else {
                        out.addAll(pendingQueue);
                        pendingQueue.clear();
                        out.add(p);
                        lastOutput = p;
                        state = State.MOVING;
                    }
                    break;
                case STATIC:
                    if (disp >= cfg.getResumeDisplacement()) {
                        out.add(p);
                        lastOutput = p;
                        state = State.MOVING;
                    }
                    break;
            }
        }
        if (state == State.PENDING_STATIC && !pendingQueue.isEmpty()) {
            out.addAll(pendingQueue);
        }
        return out;
    }
}
```

---

### 3.3 Stage 3 异常检测（OutlierDetector）

#### 3.3.1 设计动机

飞点（见 [02-research.md](./02-research.md) 第 2.2.2 节）导致里程暴增、速度失真。异常检测通过统计方法识别飞点，并按策略处理。

本设计包含三个关键机制：冷启动校准、持续检测、级联误报防护。

#### 3.3.2 整体流程

```
输入点序列
   │
   ▼
┌─────────────────────────────┐
│ 前置：速度约束硬过滤          │  v > MaxSpeed 或 a > MaxAcceleration → 飞点
└─────────────────────────────┘
   │
   ▼
┌─────────────────────────────┐
│ 冷启动校准（前 CalibrationSize 点）│
│  1. 计算速度序列              │
│  2. 3σ 清洗剔除明显飞点        │
│  3. 得到干净基线 μ, σ / Q1,Q3 │
└─────────────────────────────┘
   │
   ▼
┌─────────────────────────────┐
│ 持续检测（剩余点）             │
│  - 滑动窗口维护统计量         │
│  - Z-score 或 IQR 判定        │
│  - 命中飞点 → 处理            │
└─────────────────────────────┘
   │
   ▼
┌─────────────────────────────┐
│ 级联误报防护                  │
│  - lastActualPoint          │
│  - grace period              │
└─────────────────────────────┘
   │
   ▼
清洗后点序列
```

#### 3.3.3 前置：速度约束硬过滤

对每个点计算与上一有效点的速度：
- 位移 d = haversine(lastActualPoint, current)
- 时间差 Δt = (t_current - t_last) / 1000（秒）
- 速度 v = d / Δt

若 `v > MaxSpeed`（默认 50 m/s = 180 km/h）或加速度 `a > MaxAcceleration`（默认 10 m/s²），直接判为飞点，不进入统计判定。

#### 3.3.4 冷启动校准（Cold Start Calibration）

**目的**：建立速度基线，避免初始飞点污染统计量。

**步骤**：
1. 取轨迹前 N 个点（N = `CalibrationSize`，默认 50）
2. 计算相邻点速度序列 `v_1, v_2, ..., v_{N-1}`
3. 计算均值 μ_raw 与标准差 σ_raw
4. **3σ 清洗**：剔除 `|v_i - μ_raw| > 3·σ_raw` 的点
5. 对剩余点重算 μ、σ（Z-score）或 Q1、Q3、IQR（IQR）
6. 得到干净基线

**降级**：若 N > 输入点数，使用全部点；若清洗后点数 < 10，放宽 3σ 到 2σ 或直接用原始统计量。

#### 3.3.5 持续检测（Continuous Detection）

**滑动窗口**：维护大小 `SlidingWindowSize`（默认 100）的近期速度队列。

**Z-score 判定**：
- 计算窗口 μ、σ
- 对新点速度 v：`Z = |v - μ| / σ`
- 若 `Z > ZThreshold`（默认 2.0）→ 飞点

**IQR 判定**：
- 计算窗口 Q1、Q3、IQR
- 上界 = Q3 + IQRK · IQR，下界 = Q1 - IQRK · IQR
- 若 v 超出上下界 → 飞点

**窗口更新**：
- 正常点 → 加入窗口，淘汰最旧点
- 飞点 → 不加入窗口（避免污染基线）

#### 3.3.6 级联误报防护（Cascade False-Positive Guard）

**问题**：连续飞点会污染滑动窗口，导致后续正常点被误判，形成级联误报。

**方案**：

1. **lastActualPoint**：维护最后一个被认定为正常的点。飞点处理后，速度计算始终基于 lastActualPoint（而非上一个飞点），避免飞点累积位移。

2. **grace period（宽限期）**：
   - 飞点命中后进入 grace period（`GracePeriod` 点，默认 3）
   - 期间：
     - 不更新滑动窗口基线（保护统计量）
     - 仍按当前基线检测（但容忍度提高）
     - 正常点出现则退出 grace period 并更新基线
   - 若 grace period 内连续飞点数超过阈值，可能进入"轨迹异常"状态（可选告警）

**伪代码**：
```
state:
    lastActualPoint  # 最后正常点
    window           # 滑动窗口速度队列
    graceCount       # 当前 grace 剩余计数

for each point p:
    if lastActualPoint == nil:
        lastActualPoint = p
        window.add(speed(p))
        output(p)
        continue

    v = speed(lastActualPoint, p)
    if v > MaxSpeed:           # 硬约束
        handleOutlier(p)
        graceCount = GracePeriod
        continue

    if graceCount > 0:
        # grace 期间：仅检测不复位
        if isOutlier(v, window, method):
            handleOutlier(p)
            graceCount -= 1
        else:
            # grace 内出现正常点，恢复正常
            window.add(v)
            lastActualPoint = p
            output(p)
            graceCount = 0
    else:
        if isOutlier(v, window, method):
            handleOutlier(p)
            graceCount = GracePeriod
        else:
            window.add(v)
            lastActualPoint = p
            output(p)
```

#### 3.3.7 飞点处理策略

| 策略 | 行为 | 适用 |
|------|------|------|
| `replace` | 用 lastActualPoint 的经纬度替代飞点（保留飞点时间戳），输出替代点 | 需保持点数连续（如时间序列分析） |
| `drop` | 丢弃飞点，不输出 | 需精简点数（如存储优化） |

**replace 细节**：
```
replacedPoint = GPSPoint{
    Latitude:  lastActualPoint.Latitude,
    Longitude: lastActualPoint.Longitude,
    Timestamp: outlier.Timestamp,   // 保留时间戳
    Accuracy:  lastActualPoint.Accuracy,
}
output(replacedPoint)
```

**注意**：replace 后 lastActualPoint 不变（仍指向替换源），speed 仍基于 lastActualPoint 计算。

#### 3.3.8 参数表

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `DetectorMethod` | string | "zscore" | 检测方法：zscore / iqr |
| `ZThreshold` | float64 | 2.0 | Z-score 判定阈值 |
| `IQRK` | float64 | 1.5 | IQR 判定系数 |
| `CalibrationSize` | int | 50 | 冷启动样本数 |
| `SlidingWindowSize` | int | 100 | 持续检测窗口 |
| `MaxSpeed` | float64 | 50 | 速度上限（m/s） |
| `MaxAcceleration` | float64 | 10 | 加速度上限（m/s²） |
| `GracePeriod` | int | 3 | 级联误报防护宽限期 |
| `OutlierStrategy` | string | "replace" | 处理策略 |

#### 3.3.9 边界条件

| 输入情况 | 处理 |
|---------|------|
| 空轨迹 | 返回空 |
| 单点 | 透传（无法计算速度） |
| 两点 | 透传（无法建立统计基线，仅速度约束） |
| 点数 < CalibrationSize | 用全部点做校准 |
| 连续飞点 > GracePeriod | 持续 drop / replace，统计丢弃数 |

#### 3.3.10 实现要点

- **有状态**：滑动窗口、lastActualPoint、graceCount 跨点保持
- **流式支持**：可改造为逐点增量接口
- **双方法**：Z-score 与 IQR 通过 `DetectorMethod` 切换，内部用接口抽象

```go
// Go 检测器接口
type Detector interface {
    IsOutlier(v float64, window []float64) bool
}

type ZScoreDetector struct{ threshold float64 }
func (d *ZScoreDetector) IsOutlier(v float64, window []float64) bool {
    mu, sigma := meanStddev(window)
    if sigma == 0 { return false }
    return math.Abs(v-mu)/sigma > d.threshold
}

type IQRDetector struct{ k float64 }
func (d *IQRDetector) IsOutlier(v float64, window []float64) bool {
    q1, q3 := quantile(window, 0.25), quantile(window, 0.75)
    iqr := q3 - q1
    return v > q3+d.k*iqr || v < q1-d.k*iqr
}
```

```java
// Java 检测器接口
public interface Detector {
    boolean isOutlier(double v, List<Double> window);
}

public class ZScoreDetector implements Detector {
    private final double threshold;
    public ZScoreDetector(double threshold) { this.threshold = threshold; }
    @Override
    public boolean isOutlier(double v, List<Double> window) {
        double[] ms = Util.meanStddev(window);
        if (ms[1] == 0) return false;
        return Math.abs(v - ms[0]) / ms[1] > threshold;
    }
}

public class IQRDetector implements Detector {
    private final double k;
    public IQRDetector(double k) { this.k = k; }
    @Override
    public boolean isOutlier(double v, List<Double> window) {
        double q1 = Util.quantile(window, 0.25);
        double q3 = Util.quantile(window, 0.75);
        double iqr = q3 - q1;
        return v > q3 + k * iqr || v < q1 - k * iqr;
    }
}
```

---

## 4. 数据模型设计

### 4.1 GPSPoint

详见 [01-requirements.md](./01-requirements.md) 第 5.1 节。

设计要点：
- **不可变**：Go 中按值传递（struct），Java 中字段 final
- **WGS84 坐标**：不内置坐标系转换
- **时间戳毫秒**：统一 Unix 毫秒
- **accuracy 可选**：0 或负值表示无效

### 4.2 CleanResult

详见 [01-requirements.md](./01-requirements.md) 第 5.2 节。

设计要点：
- **Points**：清洗后点序列
- **Stats**：聚合统计（输入 / 输出 / 丢弃 / 替换 / 保留率）
- **DropDetail**：各 Stage 丢弃明细，便于调优与审计

### 4.3 Config

详见 [01-requirements.md](./01-requirements.md) 第 5.3 节与本文第 9 节。

设计要点：
- **只读**：传入 Pipeline 后不可变
- **Validate()**：校验参数合法性（范围、依赖关系）
- **DefaultConfig()**：推荐默认值

```go
// Go Config 校验
func (c Config) Validate() error {
    if c.AccuracyThreshold <= 0 {
        return errors.New("AccuracyThreshold must be positive")
    }
    if c.MinStaticPoints < 1 {
        return errors.New("MinStaticPoints must be >= 1")
    }
    if c.MinDisplacement <= 0 {
        return errors.New("MinDisplacement must be positive")
    }
    if c.ResumeDisplacement <= c.MinDisplacement {
        return errors.New("ResumeDisplacement must be > MinDisplacement")
    }
    if c.DetectorMethod != "zscore" && c.DetectorMethod != "iqr" {
        return errors.New("DetectorMethod must be zscore or iqr")
    }
    if c.ZThreshold < 1.0 || c.ZThreshold > 5.0 {
        return errors.New("ZThreshold must be in [1.0, 5.0]")
    }
    if c.IQRK < 1.0 || c.IQRK > 3.0 {
        return errors.New("IQRK must be in [1.0, 3.0]")
    }
    if c.CalibrationSize < 10 {
        return errors.New("CalibrationSize must be >= 10")
    }
    if c.SlidingWindowSize < 20 {
        return errors.New("SlidingWindowSize must be >= 20")
    }
    if c.MaxSpeed <= 0 || c.MaxAcceleration <= 0 {
        return errors.New("MaxSpeed and MaxAcceleration must be positive")
    }
    if c.GracePeriod < 0 {
        return errors.New("GracePeriod must be >= 0")
    }
    if c.OutlierStrategy != "replace" && c.OutlierStrategy != "drop" {
        return errors.New("OutlierStrategy must be replace or drop")
    }
    return nil
}
```

```java
// Java Config 校验
public class Config {
    public void validate() {
        if (accuracyThreshold <= 0)
            throw new IllegalArgumentException("AccuracyThreshold must be positive");
        if (minStaticPoints < 1)
            throw new IllegalArgumentException("MinStaticPoints must be >= 1");
        if (minDisplacement <= 0)
            throw new IllegalArgumentException("MinDisplacement must be positive");
        if (resumeDisplacement <= minDisplacement)
            throw new IllegalArgumentException("ResumeDisplacement must be > MinDisplacement");
        if (!detectorMethod.equals("zscore") && !detectorMethod.equals("iqr"))
            throw new IllegalArgumentException("DetectorMethod must be zscore or iqr");
        if (zThreshold < 1.0 || zThreshold > 5.0)
            throw new IllegalArgumentException("ZThreshold must be in [1.0, 5.0]");
        if (iqrK < 1.0 || iqrK > 3.0)
            throw new IllegalArgumentException("IQRK must be in [1.0, 3.0]");
        if (calibrationSize < 10)
            throw new IllegalArgumentException("CalibrationSize must be >= 10");
        if (slidingWindowSize < 20)
            throw new IllegalArgumentException("SlidingWindowSize must be >= 20");
        if (maxSpeed <= 0 || maxAcceleration <= 0)
            throw new IllegalArgumentException("MaxSpeed and MaxAcceleration must be positive");
        if (gracePeriod < 0)
            throw new IllegalArgumentException("GracePeriod must be >= 0");
        if (!outlierStrategy.equals("replace") && !outlierStrategy.equals("drop"))
            throw new IllegalArgumentException("OutlierStrategy must be replace or drop");
    }
}
```

### 4.4 内部数据结构

| 结构 | 用途 |
|------|------|
| `pseudoStaticState` | 状态机枚举（MOVING / PENDING_STATIC / STATIC） |
| `Detector` 接口 | 异常检测方法抽象（ZScore / IQR 实现） |
| `StatsAccumulator` | Pipeline 内部统计累加器 |
| `RingBuffer` | 滑动窗口环形缓冲（OutlierDetector 使用） |

### 4.5 工具函数

| 函数 | 说明 |
|------|------|
| `Haversine(p1, p2) float64` | 大圆距离（米），球面三角 |
| `Speed(p1, p2) float64` | 速度（m/s） |
| `Acceleration(v1, v2, dt) float64` | 加速度（m/s²） |
| `Mean(xs) float64` | 均值 |
| `Stddev(xs) float64` | 标准差（总体） |
| `Quantile(xs, q) float64` | 分位数（线性插值） |
| `MeanStddev(xs) (float64, float64)` | 一次遍历算均值与标准差（Welford 算法） |

**Haversine 实现**（双语言对齐）：

```go
// Go
func Haversine(p1, p2 *GPSPoint) float64 {
    const R = 6371000.0 // 地球半径（米）
    lat1 := p1.Latitude * math.Pi / 180
    lat2 := p2.Latitude * math.Pi / 180
    dlat := (p2.Latitude - p1.Latitude) * math.Pi / 180
    dlon := (p2.Longitude - p1.Longitude) * math.Pi / 180
    a := math.Sin(dlat/2)*math.Sin(dlat/2) +
        math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    return R * c
}
```

```java
// Java
public static double haversine(GPSPoint p1, GPSPoint p2) {
    final double R = 6371000.0;
    double lat1 = Math.toRadians(p1.getLatitude());
    double lat2 = Math.toRadians(p2.getLatitude());
    double dlat = Math.toRadians(p2.getLatitude() - p1.getLatitude());
    double dlon = Math.toRadians(p2.getLongitude() - p1.getLongitude());
    double a = Math.sin(dlat/2)*Math.sin(dlat/2) +
        Math.cos(lat1)*Math.cos(lat2)*Math.sin(dlon/2)*Math.sin(dlon/2);
    double c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1-a));
    return R * c;
}
```

**注意**：双语言必须使用相同的运算顺序与常量，保证浮点结果位级一致。

---

## 5. Go 与 Java 实现差异说明

### 5.1 数值一致性

| 方面 | Go | Java | 对齐策略 |
|------|----|----|---------|
| 浮点类型 | float64 | double | 均为 IEEE 754 double，天然一致 |
| 整数类型 | int64 | long | 均为 64 位 |
| 数学函数 | `math.Sin` | `Math.sin` | 实现可能微差，需对拍验证 |
| 运算顺序 | 显式控制 | 显式控制 | 代码保持相同表达式顺序 |
| 常量精度 | `const R = 6371000.0` | `final double R = 6371000.0` | 字面量一致 |

**风险**：Go 与 Java 的 `math.Sin` / `Math.sin` 底层实现可能不同（不同 C 库），在极端情况下最后几位 ULP 可能不同。对 Haversine 距离影响 < 1e-9 米，可忽略；对 Z-score 判定边界点需注意。

**缓解**：提供对拍测试套件，固定输入数据，断言输出一致（允许 1e-9 容差）。

### 5.2 集合与迭代

| 方面 | Go | Java |
|------|----|----|
| 列表 | `[]GPSPoint`（切片） | `List<GPSPoint>`（ArrayList） |
| 迭代 | `for _, p := range points` | `for (GPSPoint p : points)` |
| 容量预分配 | `make([]T, 0, cap)` | `new ArrayList<>(cap)` |
| 不可变 | 值传递 struct | final 字段 + 无 setter |

### 5.3 错误处理

| 方面 | Go | Java |
|------|----|----|
| 参数校验 | `Config.Validate() error` | `Config.validate()` throws |
| 返回错误 | `(result, error)` | 抛 `IllegalArgumentException` |
| Panic / 异常 | 避免 panic，返回 error | 避免 RuntimeException，抛受检异常 |

### 5.4 接口与多态

| 方面 | Go | Java |
|------|----|----|
| Stage 接口 | `interface { Process([]GPSPoint) []GPSPoint; Name() string }` | `interface Stage { ... }` |
| Detector 接口 | 隐式实现 | `implements Detector` |
| 工厂 | `NewPipeline(cfg) *Pipeline` | `Pipeline.create(cfg)` 静态工厂 |

### 5.5 流式处理

| 方面 | Go | Java |
|------|----|----|
| 有状态 Stage | 指针接收者 `*PseudoStaticDetector` | 实例字段 |
| 复制 | `Clone()` 方法返回深拷贝 | `clone()` 或拷贝构造 |
| 并发隔离 | 每 goroutine 一实例 | 每线程一实例 |

### 5.6 包结构与可见性

```
# Go
trajectory-cleaner/
├── cleaner.go          # 对外 API（Pipeline, Clean）
├── pipeline.go
├── model/
│   ├── point.go
│   ├── config.go
│   └── result.go
├── stage/
│   ├── accuracy/accuracy.go
│   ├── pseudostatic/detector.go
│   └── outlier/detector.go
└── internal/util/
    ├── haversine.go
    └── stats.go
```

```
# Java
src/main/java/com/trajectorycleaner/
├── Cleaner.java
├── pipeline/Pipeline.java
├── model/{GPSPoint,Config,CleanResult,ProcessStats,DropDetail}.java
├── stage/
│   ├── accuracy/AccuracyFilter.java
│   ├── pseudostatic/PseudoStaticDetector.java
│   └── outlier/{OutlierDetector,Detector,ZScoreDetector,IQRDetector}.java
└── util/{Haversine,Stats,Quantile}.java
```

---

## 6. 扩展性设计

### 6.1 Stage 接口扩展

新增 Stage 只需实现 `Stage` 接口，注册到 Pipeline：

```go
// Go - 自定义 Stage
type MyStage struct{}
func (s *MyStage) Process(points []GPSPoint) []GPSPoint { /* ... */ }
func (s *MyStage) Name() string { return "my-stage" }

// 注册
pipeline := NewPipeline(cfg).
    AddStage(&AccuracyFilter{cfg}).
    AddStage(&PseudoStaticDetector{cfg}).
    AddStage(&OutlierDetector{cfg}).
    AddStage(&MyStage{}) // 自定义
```

```java
// Java - 自定义 Stage
public class MyStage implements Stage {
    public List<GPSPoint> process(List<GPSPoint> points) { /* ... */ }
    public String name() { return "my-stage"; }
}

// 注册
Pipeline pipeline = Pipeline.create(cfg)
    .addStage(new AccuracyFilter(cfg))
    .addStage(new PseudoStaticDetector(cfg))
    .addStage(new OutlierDetector(cfg))
    .addStage(new MyStage());
```

### 6.2 异常检测器扩展

新增统计方法只需实现 `Detector` 接口：

```go
// Go - 修正 Z-score（基于 MAD）
type ModifiedZScoreDetector struct{ threshold float64 }
func (d *ModifiedZScoreDetector) IsOutlier(v float64, window []float64) bool {
    med := median(window)
    mad := medianAbsDeviation(window)
    if mad == 0 { return false }
    return 0.6745*math.Abs(v-med)/mad > d.threshold
}
```

```java
// Java - 修正 Z-score
public class ModifiedZScoreDetector implements Detector {
    private final double threshold;
    public boolean isOutlier(double v, List<Double> window) {
        double med = Util.median(window);
        double mad = Util.medianAbsDeviation(window);
        if (mad == 0) return false;
        return 0.6745 * Math.abs(v - med) / mad > threshold;
    }
}
```

### 6.3 预留扩展点

| 扩展点 | 接口 | 用途 | 当前状态 |
|--------|------|------|---------|
| DP 抽稀 | `Stage` | Douglas-Peucker 抽稀 | 预留（见 [02-research.md](./02-research.md) 4.1） |
| VW 抽稀 | `Stage` | Visvalingam-Whyatt 抽稀 | 预留（见 [02-research.md](./02-research.md) 4.2） |
| 距离阈值抽稀 | `Stage` | 简单距离阈值 | 预留 |
| 绑路适配器 | `Stage` | 封装 OSRM/GraphHopper 调用 | 预留（见 [02-research.md](./02-research.md) 第 8 节） |
| 补偿插值 | `Stage` | 线性 / 路径插值 | 预留 |
| 卡尔曼滤波 | `Stage` | 平滑 + 飞点检测 | 预留（见 [02-research.md](./02-research.md) 3.4） |

### 6.4 配置扩展

Config 可通过嵌套 / 组合扩展：

```go
// Go - 扩展配置
type ExtendedConfig struct {
    Config            // 嵌入基础配置
    DPThreshold  float64
    EnableDP      bool
}
```

---

## 7. 错误处理与边界条件

### 7.1 输入校验

| 校验项 | 失败处理 |
|--------|---------|
| Config.Validate() 失败 | 返回 error / 抛异常，不执行 |
| 轨迹点数 = 0 | 返回空 CleanResult |
| 轨迹点数 = 1 | 透传单点（异常检测需 ≥2 点） |
| 时间戳非升序 | 行为未定义（文档警示，不排序） |
| 经纬度越界 | 按 StrictMode 处理或透传 |
| NaN / Inf | 按 StrictMode 处理 |

### 7.2 数值边界

| 情况 | 处理 |
|------|------|
| Δt = 0（时间戳相同） | 速度视为无穷大，按 MaxSpeed 判飞点 |
| σ = 0（窗口内速度全相同） | Z-score 判定返回 false（不判飞点） |
| IQR = 0 | IQR 判定返回 false |
| 窗口为空 | 降级为仅速度约束 |

### 7.3 状态机边界

| 情况 | 处理 |
|------|------|
| 轨迹结束时仍 PENDING_STATIC | 补回暂存队列所有点 |
| 轨迹结束时 STATIC | 正常结束（已输出锚点） |
| ResumeDisplacement ≤ MinDisplacement | Config.Validate() 拒绝 |

---

## 8. 并发与线程安全

### 8.1 线程安全模型

| 组件 | 线程安全 | 说明 |
|------|---------|------|
| Config | 安全（只读） | 不可变，可多线程共享 |
| GPSPoint | 安全（值类型 / final） | 不可变 |
| AccuracyFilter | 安全（无状态） | 每次 Process 独立 |
| PseudoStaticDetector | **不安全**（有状态） | 单实例单线程使用 |
| OutlierDetector | **不安全**（有状态） | 单实例单线程使用 |
| Pipeline | 安全（无状态编排） | 每次 Process 独立 |

### 8.2 并发使用模式

**批量处理多轨迹**：每轨迹一实例（或 Clone）：

```go
// Go
for _, track := range tracks {
    detector := baseDetector.Clone() // 每轨迹独立
    go func() { detector.Process(track) }()
}
```

```java
// Java
for (List<GPSPoint> track : tracks) {
    PseudoStaticDetector detector = baseDetector.clone();
    executor.submit(() -> detector.process(track));
}
```

**单轨迹流式**：单实例逐点喂入：

```go
// Go
detector := NewPseudoStaticDetector(cfg)
for p := range pointStream {
    detector.ProcessPoint(p) // 增量接口
}
```

### 8.3 避免全局锁

- 设计上避免 `sync.Mutex`（Go）/ `synchronized`（Java）保护单点处理
- 依赖不可变数据流 + 实例隔离实现并发
- Pipeline 内部无共享可变状态

---

## 9. 完整参数表

### 9.1 全局参数

| 参数 | 类型 | 默认 | 范围 | Stage | 说明 |
|------|------|------|------|-------|------|
| `Debug` | bool | false | - | 全局 | 调试日志开关 |

### 9.2 Stage 1 精度过滤参数

| 参数 | 类型 | 默认 | 范围 | 说明 |
|------|------|------|------|------|
| `AccuracyThreshold` | float64 | 50 | (0, +∞) | 精度阈值（米） |
| `StrictMode` | bool | false | - | accuracy 无效时严格丢弃 |

### 9.3 Stage 2 伪静止状态机参数

| 参数 | 类型 | 默认 | 范围 | 说明 |
|------|------|------|------|------|
| `MinStaticPoints` | int | 5 | [1, 100] | 确认静止所需连续点数 |
| `MinDisplacement` | float64 | 3 | (0, +∞) | 单点位移阈值（米） |
| `ResumeDisplacement` | float64 | 10 | > MinDisplacement | 恢复运动位移阈值（米） |

**状态机转换速查**：

```
MOVING --位移<MinDisplacement--> PENDING_STATIC
PENDING_STATIC --连续>=MinStaticPoints--> STATIC
PENDING_STATIC --位移>=MinDisplacement--> MOVING(补回暂存)
STATIC --位移>=ResumeDisplacement--> MOVING
```

### 9.4 Stage 3 异常检测参数

| 参数 | 类型 | 默认 | 范围 | 说明 |
|------|------|------|------|------|
| `DetectorMethod` | string | "zscore" | zscore/iqr | 检测方法 |
| `ZThreshold` | float64 | 2.0 | [1.0, 5.0] | Z-score 阈值 |
| `IQRK` | float64 | 1.5 | [1.0, 3.0] | IQR 系数 |
| `CalibrationSize` | int | 50 | [10, 500] | 冷启动样本数 |
| `SlidingWindowSize` | int | 100 | [20, 1000] | 滑动窗口 |
| `MaxSpeed` | float64 | 50 | (0, +∞) | 速度上限（m/s） |
| `MaxAcceleration` | float64 | 10 | (0, +∞) | 加速度上限（m/s²） |
| `GracePeriod` | int | 3 | [0, 20] | 级联误报防护宽限期 |
| `OutlierStrategy` | string | "replace" | replace/drop | 飞点处理策略 |

### 9.5 参数依赖关系图

```
AccuracyThreshold ─┐
StrictMode ────────┴─> Stage 1

MinStaticPoints ─┐
MinDisplacement ─┤─> Stage 2
ResumeDisplacement ─┘ (必须 > MinDisplacement)

DetectorMethod ─┬─> ZThreshold (当 method=zscore)
                └─> IQRK (当 method=iqr)
CalibrationSize ─┐
SlidingWindowSize ┤
MaxSpeed ────────┤─> Stage 3
MaxAcceleration ─┤
GracePeriod ─────┤
OutlierStrategy ─┘
```

### 9.6 场景化推荐配置

| 场景 | AccuracyThreshold | MinStaticPoints | MinDisplacement | ZThreshold | OutlierStrategy |
|------|-------------------|-----------------|-----------------|------------|-----------------|
| 车载（高速） | 30 | 5 | 3 | 2.5 | drop |
| 步行（低速） | 50 | 8 | 3 | 2.0 | replace |
| 共享单车 | 40 | 6 | 3 | 2.0 | replace |
| 室内外混合 | 80 | 10 | 5 | 1.5 | replace |
| 高精度（RTK） | 10 | 3 | 1 | 3.0 | drop |
| 外勤巡检 | 50 | 8 | 3 | 2.0 | replace |

参数调优的实验设计与结果详见 [04-validation.md](./04-validation.md)。

---

## 10. 相关文档

- [01-requirements.md](./01-requirements.md) - 需求文档（功能需求、输入输出、约束）
- [02-research.md](./02-research.md) - 调研文档（算法选型理由、大厂对比、开源方案）
- [04-validation.md](./04-validation.md) - 数据验证文档（参数调优、多维度验证）
