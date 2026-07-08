package cleaner

import (
	"fmt"
	"math/rand"
	"testing"
)

func BenchmarkCleanSinglePoint(b *testing.B) {
	config := DefaultConfig()
	c := New(config)

	rng := rand.New(rand.NewSource(42))
	points := make([]GPSPoint, b.N)
	for i := 0; i < b.N; i++ {
		points[i] = GPSPoint{
			DeviceID:  "dev_001",
			Latitude:  47.6062 + rng.Float64()*0.0001,
			Longitude: -122.3321 + rng.Float64()*0.0001,
			Timestamp: int64(i) * 1000,
			Accuracy:  10.0,
			Speed:     5.0,
			Heading:   0,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Clean(points[i])
	}
}

func BenchmarkClean400Devices(b *testing.B) {
	config := DefaultConfig()
	rng := rand.New(rand.NewSource(42))

	// Pre-generate points for 400 devices
	type devPoint struct {
		devID string
		pt    GPSPoint
	}
	points := make([]devPoint, b.N)
	for i := 0; i < b.N; i++ {
		points[i] = devPoint{
			devID: fmt.Sprintf("device_%04d", i%400),
			pt: GPSPoint{
				DeviceID:  fmt.Sprintf("device_%04d", i%400),
				Latitude:  47.6062 + rng.Float64()*0.0001,
				Longitude: -122.3321 + rng.Float64()*0.0001,
				Timestamp: int64(i/400) * 1000,
				Accuracy:  10.0,
				Speed:     5.0,
				Heading:   0,
			},
		}
	}

	cleaners := make(map[string]*Cleaner)
	for i := 0; i < 400; i++ {
		cleaners[fmt.Sprintf("device_%04d", i)] = New(config)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cleaners[points[i].devID].Clean(points[i].pt)
	}
}

func BenchmarkDistanceCalc(b *testing.B) {
	p1 := GPSPoint{Latitude: 47.6062, Longitude: -122.3321}
	p2 := GPSPoint{Latitude: 47.6063, Longitude: -122.3322}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p1.DistanceTo(p2)
	}
}

func BenchmarkMemoryPerCleaner(b *testing.B) {
	config := DefaultConfig()
	cleaners := make([]*Cleaner, 0, 400)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if i < 400 {
			cleaners = append(cleaners, New(config))
		} else {
			cleaners[i%400] = New(config)
		}
	}
}
