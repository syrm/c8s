package docker

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/internal/model"
)

// BenchmarkContainerAppendLog tests the performance of log appending.
// This is a hot path in the application as logs are continuously streamed.
func BenchmarkContainerAppendLog(b *testing.B) {
	c := &Container{
		logs:        make([]string, 0, 1000),
		maxLogLines: 1000,
	}

	line := "2024-03-29T10:00:00.000Z [INFO] This is a sample log line that might be typical in a container output"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.AppendLog(line)
	}
}

// BenchmarkContainerSnapshot tests the performance of creating container snapshots.
// Snapshots are created frequently to update the TUI.
func BenchmarkContainerSnapshot(b *testing.B) {
	c := &Container{
		ID:                  model.ContainerID("test-container-id"),
		Service:             "test-service",
		Name:                "test-container",
		Project:             model.ContainerProject{ID: model.ProjectID("test"), Name: "test"},
		CPUPercentage:       50.5,
		MemoryPercentage:    75.2,
		Status:              model.StatusRunning,
		PendingAction:       "",
		LogCollectionActive: true,
		logs:                make([]string, 1000),
	}

	// Fill logs with sample data
	for i := 0; i < 1000; i++ {
		c.logs[i] = fmt.Sprintf("Log line %d", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = c.Snapshot()
	}
}

// BenchmarkCalculateStats tests the performance of CPU/memory calculations.
// These calculations are performed for each container on every stats update.
func BenchmarkCalculateStats(b *testing.B) {
	c := &Container{}

	// Create a realistic stats response
	stats := apiContainer.StatsResponse{
		CPUStats: apiContainer.CPUStats{
			CPUUsage: apiContainer.CPUUsage{
				TotalUsage:        1000000,
				PercpuUsage:       []uint64{500000, 500000},
				UsageInKernelmode: 100000,
				UsageInUsermode:   900000,
			},
			SystemUsage: 2000000,
			OnlineCPUs:  2,
		},
		PreCPUStats: apiContainer.CPUStats{
			CPUUsage: apiContainer.CPUUsage{
				TotalUsage:        900000,
				PercpuUsage:       []uint64{450000, 450000},
				UsageInKernelmode: 90000,
				UsageInUsermode:   810000,
			},
			SystemUsage: 1800000,
			OnlineCPUs:  2,
		},
		MemoryStats: apiContainer.MemoryStats{
			Usage:    1000000000, // 1GB
			MaxUsage: 1500000000, // 1.5GB
			Limit:    4000000000, // 4GB
			Stats: map[string]uint64{
				"active_anon":         500000000,
				"inactive_anon":       300000000,
				"active_file":         150000000,
				"inactive_file":       50000000,
				"total_inactive_file": 50000000,
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.Update(stats)
	}
}

// BenchmarkStatusFromAction tests the performance of status conversion.
// This is called for every container event.
func BenchmarkStatusFromAction(b *testing.B) {
	actions := []events.Action{
		events.ActionStart,
		events.ActionStop,
		events.ActionRestart,
		events.ActionPause,
		events.ActionUnPause,
		events.ActionDie,
		events.ActionRemove,
		events.ActionCreate,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		action := actions[i%len(actions)]
		_ = statusFromAction(action)
	}
}

// BenchmarkContainerLogTruncate tests the performance of log buffer truncation.
// This happens when the log buffer reaches its maximum size.
func BenchmarkContainerLogTruncate(b *testing.B) {
	c := &Container{
		logs:        make([]string, 0, 1000),
		maxLogLines: 1000,
	}

	// Fill to near capacity
	for i := 0; i < 999; i++ {
		c.logs = append(c.logs, fmt.Sprintf("Log line %d", i))
	}

	line := "2024-03-29T10:00:00.000Z [INFO] This is a sample log line that triggers truncation"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.AppendLog(line)
		// Reset for next iteration
		if len(c.logs) > 1000 {
			c.logs = c.logs[:999]
		}
	}
}

// BenchmarkParallelContainerAccess tests concurrent access to container data.
// Multiple goroutines may access container data simultaneously.
func BenchmarkParallelContainerAccess(b *testing.B) {
	c := &Container{
		ID:                  model.ContainerID("test"),
		Service:             "test",
		Name:                "test",
		Project:             model.ContainerProject{ID: model.ProjectID("test"), Name: "test"},
		CPUPercentage:       50.0,
		MemoryPercentage:    50.0,
		Status:              model.StatusRunning,
		PendingAction:       "",
		LogCollectionActive: true,
		logs:                make([]string, 100),
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.Snapshot()
		}
	})
}

// BenchmarkStringJoinForLogs benchmarks different ways to join log strings.
// This is relevant for the TUI log display.
func BenchmarkStringJoinForLogs(b *testing.B) {
	logs := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		logs[i] = fmt.Sprintf("Log line %d with some content\n", i)
	}

	b.Run("strings.Join", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = strings.Join(logs, "")
		}
	})

	b.Run("StringBuilder", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var builder strings.Builder
			builder.Grow(len(logs) * 50) // Estimate capacity
			for _, log := range logs {
				builder.WriteString(log)
			}
			_ = builder.String()
		}
	})
}

// BenchmarkMemoryUsage reports memory usage for container tracking.
func BenchmarkMemoryUsage(b *testing.B) {
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Create many containers as would happen in a real deployment
	containers := make([]*Container, 1000)
	for i := 0; i < 1000; i++ {
		containers[i] = &Container{
			ID:                  model.ContainerID(fmt.Sprintf("container-%d", i)),
			Service:             fmt.Sprintf("service-%d", i%10),
			Name:                fmt.Sprintf("name-%d", i),
			Project:             model.ContainerProject{ID: model.ProjectID(fmt.Sprintf("project-%d", i/100)), Name: fmt.Sprintf("project-%d", i/100)},
			CPUPercentage:       float64(i % 100),
			MemoryPercentage:    float64(i % 100),
			Status:              model.StatusRunning,
			PendingAction:       "",
			LogCollectionActive: i%2 == 0,
			logs:                make([]string, i%1000),
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.Alloc-m1.Alloc)/1024, "KB/container")
	b.ReportMetric(float64(m2.Alloc-m1.Alloc)/float64(len(containers)), "bytes/each")
}

// BenchmarkConcurrentLogProcessing simulates concurrent log processing.
func BenchmarkConcurrentLogProcessing(b *testing.B) {
	c := &Container{
		logs:        make([]string, 0, 1000),
		maxLogLines: 1000,
	}

	const numGoroutines = 10
	const logsPerGoroutine = 100

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for j := 0; j < numGoroutines; j++ {
			go func(id int) {
				defer wg.Done()
				for k := 0; k < logsPerGoroutine; k++ {
					c.AppendLog(fmt.Sprintf("Goroutine %d log %d", id, k))
				}
			}(j)
		}

		wg.Wait()
	}
}
