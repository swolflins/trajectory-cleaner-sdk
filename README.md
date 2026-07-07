# Trajectory Cleaner SDK

GNSS 轨迹数据清洗 SDK，提供 **Go** 和 **Java (Maven)** 双语言实现，开箱即用。

## 概述

本 SDK 实现了简化版轨迹数据清洗流水线，包含三个核心处理阶段：

1. **精度过滤** (Accuracy Filter) — 根据定位精度阈值丢弃低质量 GPS 点
2. **伪静止状态机** (Static State Machine) — 识别并处理伪静止点（如等红灯、堵车）
3. **异常检测** (Anomaly Detection) — 基于 Z-score 或 IQR 方法检测漂移异常点

### 处理流程

```
原始GPS点 → 精度过滤 → 伪静止状态机 → 异常检测 → 清洗后轨迹
```

## 项目结构

```
trajectory-cleaner-sdk/
├── docs/                           # 完整文档
│   ├── 01-requirements.md          # 需求文档
│   ├── 02-research.md              # 调研文档（全链路）
│   ├── 03-design.md                # 方案设计文档
│   └── 04-validation.md            # 数据验证文档
├── go/                             # Go 实现
│   ├── cleaner.go                  # 核心清洗器
│   ├── config.go                   # 配置参数
│   ├── pipeline.go                 # 流水线
│   ├── math.go                     # 数学工具
│   ├── result.go                   # 结果定义
│   ├── eval/                       # 评估框架
│   ├── cmd/                        # 命令行工具
│   │   ├── eval/                   # 评估命令
│   │   ├── gendata/                # 数据生成
│   │   ├── tuning/                 # 参数调优
│   │   ├── report/                 # 报告生成
│   │   └── visualize/              # 可视化
│   ├── Makefile
│   └── go.mod
├── java/                           # Java Maven 实现
│   ├── src/main/java/com/example/cleaner/
│   │   ├── config/                 # 配置
│   │   ├── model/                  # 数据模型
│   │   ├── stage/                  # 处理阶段
│   │   └── TrajectoryCleaner.java  # 主入口
│   ├── src/test/java/             # 单元测试（46个）
│   └── pom.xml
├── data/                           # 测试数据集
│   ├── seattle/                    # 西雅图驾驶模拟
│   ├── geolife/                    # 北京混合出行
│   ├── synthetic/                  # 合成轨迹
│   └── optimal_params.json         # 最优参数
└── reports/
    └── validation-report.html      # 多维度验证报告
```

## 快速开始

### Go

```bash
cd go

# 编译
make build

# 运行测试
make test

# 运行评估
make eval

# 生成数据
go run cmd/gendata/main.go

# 参数调优
go run cmd/tuning/main.go

# 生成验证报告
go run cmd/report/main.go
```

#### 代码示例

```go
package main

import (
    "fmt"
    "cleaner"
)

func main() {
    config := cleaner.DefaultConfig()
    c := cleaner.NewCleaner(config)

    points := []cleaner.GPSPoint{
        {Lat: 47.6062, Lng: -122.3321, Accuracy: 10, Timestamp: 1000},
        {Lat: 47.6063, Lng: -122.3322, Accuracy: 12, Timestamp: 2000},
        // ...
    }

    results := c.Clean(points)
    for _, r := range results {
        fmt.Printf("Point: %v, Action: %v\n", r.Point, r.Action)
    }
}
```

### Java

```bash
cd java

# 编译
mvn clean compile

# 运行测试
mvn test

# 打包
mvn package
```

#### 代码示例

```java
import com.example.cleaner.TrajectoryCleaner;
import com.example.cleaner.config.CleanerConfig;
import com.example.cleaner.model.GPSPoint;
import com.example.cleaner.model.CleanResult;

public class Main {
    public static void main(String[] args) {
        CleanerConfig config = CleanerConfig.builder()
            .maxAccuracy(50.0)
            .staticQueueSize(10)
            .staticDistanceThreshold(15.0)
            .motionConfirmCount(3)
            .statsWindowSize(10)
            .anomalyMethod("iqr")
            .iqrMultiplier(1.5)
            .maxVelocity(41.67)
            .anomalyStrategy("replace")
            .build();

        TrajectoryCleaner cleaner = new TrajectoryCleaner(config);

        List<GPSPoint> points = List.of(
            GPSPoint.builder().lat(47.6062).lng(-122.3321).accuracy(10).timestamp(1000).build(),
            GPSPoint.builder().lat(47.6063).lng(-122.3322).accuracy(12).timestamp(2000).build()
        );

        List<CleanResult> results = cleaner.clean(points);
        results.forEach(r -> System.out.println(r));
    }
}
```

## 配置参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `MaxAccuracy` | 50.0 | 定位精度阈值(米)，超过此值的点直接丢弃 |
| `StaticQueueSize` | 10 | 伪静止判断队列长度 N |
| `StaticDistanceThreshold` | 15.0 | 静止判定距离阈值 R (米) |
| `MotionConfirmCount` | 3 | 静止→运动的确认计数 |
| `StatsWindowSize` | 10 | 统计窗口大小 |
| `AnomalyMethod` | "iqr" | 异常检测方法: "zscore" 或 "iqr" |
| `InitialSigma` | 3.0 | 首次校准置信度 (σ 倍数) |
| `ContinuousSigma` | 2.0 | 持续检测置信度 (σ 倍数) |
| `IqrMultiplier` | 1.5 | IQR 乘数因子 |
| `MaxVelocity` | 41.67 | 速度上限 (m/s)，即 150 km/h |
| `AnomalyStrategy` | "replace" | 异常处理策略: "replace" 或 "drop" |

## 最优参数（基于数据集调优）

| 数据集 | 异常方法 | 队列大小 | 距离阈值 | F1 | Recall | RMSE |
|--------|----------|----------|----------|-----|--------|------|
| Seattle | IQR | 5 | 10m | 0.385 | 1.00 | 37.8 |
| GeoLife | Z-score | 15 | 10m | 0.286 | 1.00 | 51.2 |
| Synthetic | Z-score | 5 | 10m | 0.412 | 1.00 | 196.5 |
| **推荐** | **Z-score** | **15** | **10m** | **0.286** | **1.00** | **51.2** |

详细验证报告见 `reports/validation-report.html`。

## 测试

- **Java**: 46 个单元测试全部通过 (`mvn test`)
- **Go**: 全部测试通过 (`go test ./...`)

## 文档

- [需求文档](docs/01-requirements.md)
- [调研文档](docs/02-research.md) — 完整轨迹数据处理全链路（去噪、抽稀、绑路、补偿）
- [方案设计文档](docs/03-design.md)
- [数据验证文档](docs/04-validation.md)

## 技术栈

- **Go**: Go 1.18+
- **Java**: JDK 17, Maven 3.x, JUnit 5, Jackson
- **可视化**: ECharts

## License

MIT
