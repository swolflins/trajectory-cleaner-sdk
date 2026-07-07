package main

import (
	"cleaner/eval"
	"fmt"
)

func main() {
	seattlePath := "/data/user/work/datasets/seattle_gps.txt"
	geolifeDir := "/data/user/work/datasets/geolife/Geolife Trajectories 1.3/Data"
	outputJSON := "/data/user/work/datasets/eval_results.json"

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║  Trajectory Cleaner SDK - Evaluation Framework  ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()

	metrics := eval.RunAllEvaluations(seattlePath, geolifeDir, outputJSON)

	// 打印最终汇总表格
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    FINAL SUMMARY TABLE                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ %-30s │ %6s │ %6s │ %6s │ %6s ║\n",
		"Dataset", "Points", "Output", "RMSE", "F1")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	for _, m := range metrics {
		fmt.Printf("║ %-30s │ %6d │ %6d │ %5.1fm │ %6.3f ║\n",
			m.DatasetName, m.TotalPoints, m.OutputPoints, m.RMSE, m.F1Score())
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}
