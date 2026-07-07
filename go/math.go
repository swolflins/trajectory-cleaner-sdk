package cleaner

import "math"

// 数学常量及辅助函数（避免每次重复导入 math）

const pi = math.Pi

var (
	sin  = math.Sin
	cos  = math.Cos
	atan2 = math.Atan2
	sqrt = math.Sqrt
	abs  = math.Abs
	floor = math.Floor
	ceil = math.Ceil
)

// mean 计算均值
func mean(arr []float64) float64 {
	if len(arr) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range arr {
		sum += v
	}
	return sum / float64(len(arr))
}

// std 计算样本标准差
func std(arr []float64, m float64) float64 {
	n := len(arr)
	if n <= 1 {
		return 0
	}
	sum := 0.0
	for _, v := range arr {
		d := v - m
		sum += d * d
	}
	return sqrt(sum / float64(n-1))
}

// percentile 计算百分位数
func percentile(arr []float64, p float64) float64 {
	n := len(arr)
	if n == 0 {
		return 0
	}
	sorted := make([]float64, n)
	copy(sorted, arr)
	sortFloat64s(sorted)

	index := p / 100.0 * float64(n-1)
	lower := int(floor(index))
	upper := int(ceil(index))
	if lower == upper {
		return sorted[lower]
	}
	return sorted[lower] + (index-float64(lower))*(sorted[upper]-sorted[lower])
}

// sortFloat64s 对 float64 切片排序（插入排序，数据量小足够）
func sortFloat64s(arr []float64) {
	for i := 1; i < len(arr); i++ {
		key := arr[i]
		j := i - 1
		for j >= 0 && arr[j] > key {
			arr[j+1] = arr[j]
			j--
		}
		arr[j+1] = key
	}
}
