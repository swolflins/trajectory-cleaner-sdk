# 轨迹清洗 SDK 需求文档

| 版本 | 日期 | 状态 | 作者 |
|------|------|------|------|
| v1.0 | 2026-07-07 | Draft | Trajectory Cleaner SDK Team |

---

## 目录

1. [项目背景与目标](#1-项目背景与目标)
2. [术语表](#2-术语表)
3. [功能需求](#3-功能需求)
4. [非功能需求](#4-非功能需求)
5. [输入输出定义](#5-输入输出定义)
6. [约束条件](#6-约束条件)
7. [配置参数表](#7-配置参数表)
8. [验收标准](#8-验收标准)
9. [相关文档](#9-相关文档)

---

## 1. 项目背景与目标

### 1.1 背景

GNSS（Global Navigation Satellite System，全球导航卫星系统）轨迹数据在采集过程中，受多径效应、信号遮挡、设备噪声等因素影响，普遍存在以下质量问题：

- **漂移（Drift）**：定位点缓慢偏离真实位置
- **飞点（Outlier / Spike）**：速度或加速度异常的孤立点
- **静止抖动（Static Jitter）**：设备停止时 GPS 持续输出小幅抖动点
- **信号中断（Signal Loss）**：遮挡环境下定位丢失导致时间断档
- **冗余点（Redundant Points）**：直线段内大量重复近距点

这些问题若不清洗，将直接影响下游的里程统计、轨迹回放、地理围栏、行为分析等业务的准确性。

业界大厂（腾讯轨迹云、高德猎鹰、百度鹰眼等）均提供完整的轨迹后处理服务，覆盖去噪、抽稀、绑路、补偿四大能力（详见 [02-research.md](./02-research.md)）。然而，这些云端服务存在数据外发、计费、延迟、定制化能力受限等约束，无法满足所有场景需求（例如边缘端离线清洗、本地化部署、敏感数据不出域等）。

### 1.2 目标

本项目旨在提供一个**轻量级、可嵌入式、离线可运行**的轨迹清洗 SDK，提供 Go 和 Java 双语言实现，覆盖轨迹清洗全链路四大能力中的前两项：

| 能力 | 全链路覆盖 | 本项目实现 | 说明 |
|------|-----------|-----------|------|
| 去噪（Denoising） | 是 | 是 | 精度过滤 + 伪静止剔除 + 异常检测 |
| 抽稀（Simplification / Compression） | 是 | 是 | 通过静止段合并与冗余点剔除实现轻量抽稀 |
| 绑路（Map Matching） | 是 | 否 | 依赖路网数据，属大厂能力，不在本项目范围 |
| 补偿（Supplement / Interpolation） | 是 | 否 | 需路网或运动学模型，不在本项目范围 |

### 1.3 目标用户与场景

- **车载 / 物流轨迹**：里程结算、路径回放、驾驶行为分析
- **共享出行（单车 / 电单车）**：骑行轨迹还原、计费校准
- **人员定位（外勤 / 巡检）**：停留点识别、考勤轨迹
- **IoT 资产追踪**：宠物、贵重资产、可穿戴设备轨迹

### 1.4 设计原则

1. **零外部依赖**：不依赖路网数据、地图服务、数据库，可离线运行
2. **双语言对等**：Go 与 Java 实现行为一致、参数对齐、结果可对拍
3. **配置驱动**：所有阈值参数化，支持不同场景调优
4. **流式友好**：支持单点增量处理与批量处理两种模式
5. **确定性**：相同输入 + 相同配置 = 相同输出，便于测试与回归

---

## 2. 术语表

| 术语 | 英文 | 释义 |
|------|------|------|
| 轨迹 | Trajectory / Track | 按时间排序的 GPS 定位点序列 |
| 精度 | Accuracy | GPS 定位的水平精度半径（米），数值越小越准 |
| 飞点 | Outlier / Spike | 速度或加速度异常的孤立定位点 |
| 漂移 | Drift | 定位点缓慢偏离真实位置 |
| 静止抖动 | Static Jitter | 设备停止时 GPS 持续输出小幅噪声点 |
| 抽稀 | Simplification / Compression | 在保持轨迹形态前提下减少点数 |
| 绑路 | Map Matching | 将 GPS 点投影到路网拓扑上 |
| 补偿 | Supplement / Interpolation | 在信号中断段插入合理点 |
| 伪静止 | Pseudo-Static | 设备实际静止但 GPS 仍有输出的状态 |
| Z-score | Z 分数 | 数据点与均值的标准差倍数 |
| IQR | Interquartile Range | 四分位距，Q3 - Q1 |

---

## 3. 功能需求

### 3.1 需求总览

SDK 提供一条三阶段串行处理管线（Pipeline）：

```
输入轨迹
   │
   ▼
┌────────────────────┐
│ Stage 1 精度过滤   │  按 accuracy 字段过滤低质量点
└────────────────────┘
   │
   ▼
┌────────────────────┐
│ Stage 2 伪静止状态机│  识别静止段并剔除抖动点
└────────────────────┘
   │
   ▼
┌────────────────────┐
│ Stage 3 异常检测   │  统计方法检测飞点，replace 或 drop
└────────────────────┘
   │
   ▼
清洗后轨迹
```

### 3.2 FR-1 精度过滤

**需求编号**：FR-1

**描述**：根据 GPS 点的 `accuracy`（水平精度半径，单位米）字段，过滤掉精度不达标的低质量点。

**详细规则**：

1. 若 `accuracy` 大于配置阈值 `AccuracyThreshold`（默认 50 米），则丢弃该点。
2. 若 `accuracy` 为 0 或负数（无效值），按配置策略处理：
   - `StrictMode=true`：丢弃
   - `StrictMode=false`：保留并标记为未校准
3. 若 `accuracy` 字段缺失（语言层 nil / null），按 `StrictMode` 同上处理。

**边界条件**：

- 输入轨迹为空 → 返回空轨迹
- 输入轨迹仅含 1 个点 → 直接透传该点（异常检测需 ≥2 点）
- 所有点均不达标 → 返回空轨迹，并在 `CleanResult` 中记录被丢弃数量

**接口契约**：

```go
// Go
func (f *AccuracyFilter) Process(points []GPSPoint) []GPSPoint
```

```java
// Java
public List<GPSPoint> process(List<GPSPoint> points);
```

### 3.3 FR-2 伪静止状态机

**需求编号**：FR-2

**描述**：识别设备实际停止但 GPS 仍输出抖动点的伪静止段，通过队列计数 + 距离阈值 + 状态机将其平滑处理。

**状态机定义**：

状态机包含 4 个状态：

| 状态 | 含义 |
|------|------|
| `MOVING` | 运动中，正常输出点 |
| `PENDING_STATIC` | 待定静止，连续 N 个点位移小于阈值，但尚未确认 |
| `STATIC` | 确认静止，仅保留静止段第一个点（锚点），丢弃后续抖动点 |
| `MOVING`（恢复） | 静止结束后重新进入运动状态 |

**状态转换**：

```
        位移 < MinDisplacement，连续 >= MinStaticPoints
        ┌──────────────────────────┐
        ▼                          │
     ┌─────────┐                ┌──────────────┐
     │ MOVING  │──────────────▶│ PENDING_STATIC│
     └─────────┘                └──────────────┘
        ▲                          │   │
        │ 位移 >= ResumeDisplacement│   │ 连续 >= MinStaticPoints
        │                          ▼   ▼
        │                      ┌────────┐
        └──────────────────────│ STATIC │
                  位移 >= ResumeDisplacement
                               └────────┘
```

**处理规则**：

1. 处于 `STATIC` 状态时，仅保留进入静止的第一个点（锚点 anchor），后续抖动点全部丢弃。
2. 当位移 ≥ `ResumeDisplacement`（默认 10 米）时，从 `STATIC` 恢复到 `MOVING`，并将该恢复点作为新的运动起点输出。
3. `PENDING_STATIC` 阶段的点暂存于队列，确认静止后丢弃，确认运动后补回输出。

**参数**：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `MinStaticPoints` | 5 | 进入 PENDING_STATIC 后需连续多少个点位移小于阈值才确认静止 |
| `MinDisplacement` | 3 米 | 单点位移小于此值视为未移动 |
| `ResumeDisplacement` | 10 米 | 静止状态下位移大于此值视为恢复运动 |

### 3.4 FR-3 异常检测

**需求编号**：FR-3

**描述**：使用 Z-score 或 IQR 统计方法检测速度 / 加速度异常的飞点，并按 `replace`（用上一有效值替代）或 `drop`（丢弃）策略处理。

**两阶段机制**：

1. **冷启动校准（Cold Start Calibration）**：
   - 输入轨迹前 N 个点（默认 50）用于建立速度 / 加速度基线。
   - 使用 3σ 原则剔除明显飞点，得到"干净基线"。
   - 计算基线均值 μ 与标准差 σ（或四分位距 IQR）。

2. **持续检测（Continuous Detection）**：
   - 对每个新点计算速度 v、加速度 a。
   - 若使用 Z-score：当 `|v - μ| / σ > ZThreshold`（默认 2.0）视为飞点。
   - 若使用 IQR：当 `v > Q3 + k·IQR` 或 `v < Q1 - k·IQR`（默认 k=1.5）视为飞点。
   - 滑动窗口更新基线（窗口大小默认 100）。

3. **级联误报防护（Cascade False-Positive Guard）**：
   - 维护 `lastActualPoint`（最后一个被认定为正常的点）。
   - 飞点检测命中后，进入 grace period（宽限期，默认 3 点），期间仅检测不复位基线，避免连续飞点污染统计量。
   - grace period 结束后重新评估基线。

**处理策略**：

| 策略 | 行为 |
|------|------|
| `replace` | 用 `lastActualPoint` 的值替代飞点（保留时间戳，替换经纬度） |
| `drop` | 直接丢弃飞点 |

**参数**：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `Method` | `zscore` | 检测方法：`zscore` 或 `iqr` |
| `ZThreshold` | 2.0 | Z-score 判定阈值 |
| `IQRK` | 1.5 | IQR 判定系数 |
| `CalibrationSize` | 50 | 冷启动校准样本数 |
| `SlidingWindowSize` | 100 | 持续检测滑动窗口 |
| `MaxSpeed` | 50 m/s | 物理上限，超过直接判飞点（180 km/h） |
| `MaxAcceleration` | 10 m/s² | 加速度物理上限 |
| `GracePeriod` | 3 | 级联误报防护宽限期点数 |
| `OutlierStrategy` | `replace` | 处理策略：`replace` 或 `drop` |

---

## 4. 非功能需求

### 4.1 NFR-1 性能

| 指标 | 要求 |
|------|------|
| 单点处理延迟 | < 100 μs（P99） |
| 1 万点批量处理 | < 50 ms（单核） |
| 10 万点批量处理 | < 500 ms（单核） |
| 内存占用 | 与输入点数成线性关系，常量因子 < 5x |
| 无锁并发 | 单 Stage 内无全局锁，依赖不可变数据流 |

### 4.2 NFR-2 线程安全

1. **Config 对象只读**：一旦传入 Pipeline，运行期间不可变，多 goroutine / 多线程共享安全。
2. **Pipeline 实例无状态**：每次 `Process` 调用独立，不持有跨调用可变状态。
3. **流式处理器（如 PseudoStaticDetector）有状态**：单实例非线程安全，需通过 `Clone()` 或每流一实例方式使用；文档需明确警示。
4. **Go 实现**：不使用 `sync.Mutex` 保护单点处理，依赖 channel + 不可变切片。
5. **Java 实现**：处理器实例非线程安全，调用方负责并发隔离。

### 4.3 NFR-3 可配置性

1. 所有阈值通过 `Config` 结构体集中管理。
2. 提供 `DefaultConfig()` 工厂方法返回推荐配置。
3. 支持 `Config.Validate()` 校验参数合法性（范围、互斥关系）。
4. 支持通过环境变量 / 配置文件覆盖默认值（可选，由上层应用决定）。
5. 关键参数允许运行时热更新（仅适用于流式处理器的部分参数）。

### 4.4 NFR-4 可观测性

1. `CleanResult` 返回处理统计：输入点数、输出点数、各 Stage 丢弃 / 替换数。
2. 支持 Debug 模式输出逐点处理日志（默认关闭）。
3. 不引入日志框架依赖，通过回调接口（`Logger` / `LogCallback`）由上层注入。

### 4.5 NFR-5 可移植性

1. Go：兼容 Go 1.18+，仅依赖标准库。
2. Java：兼容 JDK 8+，仅依赖标准库（避免 Android 兼容性问题）。
3. 双语言实现行为对等，提供对拍测试套件。

### 4.6 NFR-6 可扩展性

1. Stage 以接口形式定义，支持注册自定义 Stage。
2. Pipeline 支持动态组装 Stage 顺序。
3. 异常检测支持扩展新的统计方法（实现 `Detector` 接口）。

---

## 5. 输入输出定义

### 5.1 GPSPoint 结构

GPSPoint 表示一个带时间戳的 GPS 定位点。

```go
// Go
type GPSPoint struct {
    Latitude  float64 // 纬度，WGS84，有效范围 [-90, 90]
    Longitude float64 // 经度，WGS84，有效范围 [-180, 180]
    Timestamp int64   // Unix 毫秒时间戳
    Accuracy  float64 // 水平精度半径（米），0 或负值表示无效
    Speed     float64 // 可选，瞬时速度（m/s），若设备未提供则 SDK 计算
    Heading   float64 // 可选，航向角（度），[0, 360)
}
```

```java
// Java
public class GPSPoint {
    private final double latitude;   // 纬度
    private final double longitude;  // 经度
    private final long timestamp;    // Unix 毫秒时间戳
    private final double accuracy;   // 水平精度半径（米）
    private final double speed;       // 可选，瞬时速度（m/s）
    private final double heading;    // 可选，航向角（度）

    // 构造器、getter 省略
}
```

**字段约束**：

| 字段 | 类型 | 必填 | 取值范围 | 说明 |
|------|------|------|----------|------|
| Latitude | float64 / double | 是 | [-90, 90] | WGS84 纬度 |
| Longitude | float64 / double | 是 | [-180, 180] | WGS84 经度 |
| Timestamp | int64 / long | 是 | > 0 | Unix 毫秒时间戳 |
| Accuracy | float64 / double | 否 | > 0 | 0 或负值表示无效 |
| Speed | float64 / double | 否 | ≥ 0 | 缺失时由 SDK 根据相邻点计算 |
| Heading | float64 / double | 否 | [0, 360) | 缺失时不影响清洗 |

### 5.2 CleanResult 结构

CleanResult 表示一次清洗的输出与统计。

```go
// Go
type CleanResult struct {
    Points       []GPSPoint // 清洗后的轨迹点
    Stats        ProcessStats
    DroppedDetail DropDetail // 各 Stage 丢弃详情
}

type ProcessStats struct {
    InputCount   int // 输入点数
    OutputCount  int // 输出点数
    DroppedCount int // 总丢弃数
    ReplacedCount int // 替换数（异常检测 replace 策略）
    RetentionRate float64 // 保留率 = OutputCount / InputCount
}

type DropDetail struct {
    ByAccuracy      int // 精度过滤丢弃
    ByPseudoStatic  int // 伪静止丢弃
    ByOutlierDrop   int // 异常检测 drop 策略丢弃
    ByOutlierReplace int // 异常检测 replace 替换
}
```

```java
// Java
public class CleanResult {
    private final List<GPSPoint> points;
    private final ProcessStats stats;
    private final DropDetail droppedDetail;

    // getter 省略
}

public class ProcessStats {
    private final int inputCount;
    private final int outputCount;
    private final int droppedCount;
    private final int replacedCount;
    private final double retentionRate;
}

public class DropDetail {
    private final int byAccuracy;
    private final int byPseudoStatic;
    private final int byOutlierDrop;
    private final int byOutlierReplace;
}
```

### 5.3 Config 结构

```go
// Go
type Config struct {
    // Stage 1 精度过滤
    AccuracyThreshold float64
    StrictMode        bool

    // Stage 2 伪静止状态机
    MinStaticPoints    int
    MinDisplacement    float64
    ResumeDisplacement float64

    // Stage 3 异常检测
    DetectorMethod      string  // "zscore" | "iqr"
    ZThreshold          float64
    IQRK                float64
    CalibrationSize     int
    SlidingWindowSize   int
    MaxSpeed            float64
    MaxAcceleration     float64
    GracePeriod         int
    OutlierStrategy     string  // "replace" | "drop"

    // 全局
    Debug bool
}

func DefaultConfig() Config {
    return Config{
        AccuracyThreshold:   50,
        StrictMode:          false,
        MinStaticPoints:     5,
        MinDisplacement:     3,
        ResumeDisplacement:  10,
        DetectorMethod:      "zscore",
        ZThreshold:          2.0,
        IQRK:                1.5,
        CalibrationSize:     50,
        SlidingWindowSize:   100,
        MaxSpeed:            50,
        MaxAcceleration:     10,
        GracePeriod:         3,
        OutlierStrategy:     "replace",
        Debug:               false,
    }
}
```

```java
// Java
public class Config {
    private double accuracyThreshold;
    private boolean strictMode;
    private int minStaticPoints;
    private double minDisplacement;
    private double resumeDisplacement;
    private String detectorMethod;
    private double zThreshold;
    private double iqrK;
    private int calibrationSize;
    private int slidingWindowSize;
    private double maxSpeed;
    private double maxAcceleration;
    private int gracePeriod;
    private String outlierStrategy;
    private boolean debug;

    public static Config defaultConfig() { /* 同 Go 默认值 */ }
}
```

---

## 6. 约束条件

### 6.1 实现范围约束

| 能力 | 是否实现 | 原因 |
|------|---------|------|
| 精度过滤 | 是 | 纯数值判断，无外部依赖 |
| 伪静止剔除 | 是 | 状态机 + 几何计算，无外部依赖 |
| 异常检测（去噪） | 是 | 统计方法，无外部依赖 |
| 静止段合并（轻量抽稀） | 是 | 伪静止剔除的副产物 |
| 距离阈值抽稀 | 否（暂不实现独立 Stage） | 后续可扩展，当前需求聚焦去噪 |
| Douglas-Peucker 抽稀 | 否 | 算法已调研（见 02-research.md），未列入本期实现 |
| 绑路 | 否 | 需路网数据（OSM / 大厂路网），属大厂能力，不在本项目范围 |
| 补偿 | 否 | 需路网或运动学模型，不在本项目范围 |

### 6.2 输入数据约束

1. 输入轨迹点必须按 `Timestamp` 升序排列（SDK 不负责排序，乱序行为未定义）。
2. 时间戳必须为 Unix 毫秒。
3. 经纬度坐标系默认 WGS84，不进行坐标系转换（GCJ-02 / BD-09 由上层处理）。
4. 单条轨迹建议点数 < 100 万；超出时建议分段处理。

### 6.3 语言实现约束

1. Go 与 Java 实现的数值计算结果必须位级对齐（使用相同浮点运算顺序）。
2. 不允许使用语言特有的高精度库（如 BigDecimal）以保证一致性，统一使用 IEEE 754 double。
3. 随机性来源禁用（本 SDK 所有逻辑必须确定性）。

### 6.4 不做事项（Non-Goals）

1. 不做实时流式订阅 / 推送（SDK 是函数库，非服务）。
2. 不做可视化（轨迹回放由上层应用负责）。
3. 不做持久化（不写文件 / 数据库）。
4. 不做坐标系转换。
5. 不做里程计算（可作为上层衍生功能，但 SDK 不内置）。

---

## 7. 配置参数表

### 7.1 完整参数表

| 参数 | 类型 | 默认值 | 取值范围 | 所属 Stage | 说明 |
|------|------|--------|----------|------------|------|
| `AccuracyThreshold` | float64 | 50 | (0, +∞) | Stage 1 | 精度过滤阈值（米） |
| `StrictMode` | bool | false | true/false | Stage 1 | accuracy 无效时是否严格丢弃 |
| `MinStaticPoints` | int | 5 | [1, 100] | Stage 2 | 进入静止待定所需连续点数 |
| `MinDisplacement` | float64 | 3 | (0, +∞) | Stage 2 | 单点位移阈值（米） |
| `ResumeDisplacement` | float64 | 10 | > MinDisplacement | Stage 2 | 恢复运动位移阈值（米） |
| `DetectorMethod` | string | "zscore" | zscore/iqr | Stage 3 | 异常检测方法 |
| `ZThreshold` | float64 | 2.0 | [1.0, 5.0] | Stage 3 | Z-score 判定阈值 |
| `IQRK` | float64 | 1.5 | [1.0, 3.0] | Stage 3 | IQR 判定系数 |
| `CalibrationSize` | int | 50 | [10, 500] | Stage 3 | 冷启动校准样本数 |
| `SlidingWindowSize` | int | 100 | [20, 1000] | Stage 3 | 持续检测滑动窗口 |
| `MaxSpeed` | float64 | 50 | (0, +∞) | Stage 3 | 速度物理上限（m/s） |
| `MaxAcceleration` | float64 | 10 | (0, +∞) | Stage 3 | 加速度物理上限（m/s²） |
| `GracePeriod` | int | 3 | [0, 20] | Stage 3 | 级联误报防护宽限期 |
| `OutlierStrategy` | string | "replace" | replace/drop | Stage 3 | 飞点处理策略 |
| `Debug` | bool | false | true/false | 全局 | 是否输出逐点调试日志 |

### 7.2 参数依赖关系

1. `ResumeDisplacement` 必须 > `MinDisplacement`，否则状态机无法正常恢复。
2. `DetectorMethod` 决定使用 `ZThreshold` 还是 `IQRK`，二者中另一个被忽略。
3. `CalibrationSize` 必须 ≤ 输入轨迹长度，否则降级为使用全部点。
4. `GracePeriod=0` 表示禁用级联误报防护。

### 7.3 推荐场景配置

| 场景 | AccuracyThreshold | MinStaticPoints | ZThreshold | OutlierStrategy |
|------|-------------------|-----------------|------------|-----------------|
| 车载（高速） | 30 | 5 | 2.5 | drop |
| 步行（低速） | 50 | 8 | 2.0 | replace |
| 共享单车 | 40 | 6 | 2.0 | replace |
| 室内外混合 | 80 | 10 | 1.5 | replace |
| 高精度设备（RTK） | 10 | 3 | 3.0 | drop |

---

## 8. 验收标准

### 8.1 功能验收

1. 给定包含已知飞点的合成轨迹，SDK 能正确识别 ≥ 90% 的飞点（Recall ≥ 0.9）。
2. 给定已知静止段，SDK 能正确识别并剔除抖动点（保留率符合预期）。
3. 给定正常轨迹，SDK 误杀率 < 5%（Precision ≥ 0.95）。
4. Go 与 Java 实现对相同输入产生相同输出（对拍测试通过）。

### 8.2 性能验收

1. 10 万点批量处理 < 500 ms（单核，参照机型待定，详见 [04-validation.md](./04-validation.md)）。
2. 内存峰值 < 输入数据量的 5 倍。

### 8.3 健壮性验收

1. 输入空轨迹不 panic / 不抛异常。
2. 输入单点轨迹不 panic。
3. 输入含 NaN / Inf 的字段按 `StrictMode` 处理。
4. 时间戳重复点按定义行为处理（不 panic）。

---

## 9. 相关文档

- [02-research.md](./02-research.md) - 调研文档（大厂方案、开源方案、算法选型理由）
- [03-design.md](./03-design.md) - 方案设计文档（架构、状态机、数据模型）
- [04-validation.md](./04-validation.md) - 数据验证文档（参数调优、多维度验证结果）
