package metrics

import (
	"runtime"
	"syscall"

	"github.com/monkci/miglet/pkg/events"
)

// Collector collects system metrics
type Collector struct{}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{}
}

// CollectVMHealth collects VM health metrics
func (c *Collector) CollectVMHealth() events.VMHealth {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Get CPU load (simplified - just use runtime stats)
	cpuLoad := float64(runtime.NumGoroutine()) / 100.0 // Simplified metric

	// Memory in MB
	memoryUsed := int64(m.Alloc / 1024 / 1024)
	memoryTotal := int64(m.Sys / 1024 / 1024)

	// Disk usage (simplified - would need to check actual disk)
	// For now, return 0 - can be enhanced with actual disk stats
	diskUsed := int64(0)
	diskTotal := int64(0)

	// Try to get actual disk stats from /proc or syscall
	if stat, err := getDiskStats(); err == nil {
		diskUsed = stat.Used
		diskTotal = stat.Total
	}

	return events.VMHealth{
		CPULoad:    cpuLoad,
		MemoryUsed: memoryUsed,
		MemoryTotal: memoryTotal,
		DiskUsed:   diskUsed,
		DiskTotal:  diskTotal,
	}
}

// DiskStats represents disk statistics
type DiskStats struct {
	Used  int64
	Total int64
}

// getDiskStats gets disk statistics (simplified implementation)
func getDiskStats() (*DiskStats, error) {
	// Try to get disk stats from syscall
	var stat syscall.Statfs_t
	err := syscall.Statfs("/", &stat)
	if err != nil {
		return nil, err
	}

	// Calculate disk space
	total := int64(stat.Blocks) * int64(stat.Bsize) / 1024 / 1024 / 1024 // GB
	available := int64(stat.Bavail) * int64(stat.Bsize) / 1024 / 1024 / 1024 // GB
	used := total - available

	return &DiskStats{
		Used:  used,
		Total: total,
	}, nil
}

