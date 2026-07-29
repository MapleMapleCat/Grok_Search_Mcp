package panel

import (
	"runtime"
	"time"
)

var processStartedAt = time.Now()

// runtimeMetricsSnapshot exposes non-sensitive process and Go runtime state.
// Values are collected only when an administrator requests enabled metrics.
type runtimeMetricsSnapshot struct {
	UptimeMs          float64                      `json:"uptime_ms"`
	GoVersion         string                       `json:"go_version"`
	GoOperatingSystem string                       `json:"go_os"`
	GoArchitecture    string                       `json:"go_arch"`
	CPUCount          int                          `json:"cpu_count"`
	GOMAXPROCS        int                          `json:"gomaxprocs"`
	Goroutines        int                          `json:"goroutines"`
	CGOCalls          int64                        `json:"cgo_calls"`
	Memory            runtimeMemoryMetricsSnapshot `json:"memory"`
}

// runtimeMemoryMetricsSnapshot contains the most useful cumulative allocation,
// heap, stack, and garbage-collection counters from runtime.MemStats.
type runtimeMemoryMetricsSnapshot struct {
	AllocatedBytes                uint64     `json:"allocated_bytes"`
	TotalAllocatedBytes           uint64     `json:"total_allocated_bytes"`
	SystemBytes                   uint64     `json:"system_bytes"`
	MallocCount                   uint64     `json:"malloc_count"`
	FreeCount                     uint64     `json:"free_count"`
	HeapAllocatedBytes            uint64     `json:"heap_allocated_bytes"`
	HeapSystemBytes               uint64     `json:"heap_system_bytes"`
	HeapIdleBytes                 uint64     `json:"heap_idle_bytes"`
	HeapInUseBytes                uint64     `json:"heap_in_use_bytes"`
	HeapReleasedBytes             uint64     `json:"heap_released_bytes"`
	HeapObjectCount               uint64     `json:"heap_object_count"`
	StackInUseBytes               uint64     `json:"stack_in_use_bytes"`
	StackSystemBytes              uint64     `json:"stack_system_bytes"`
	MetadataInUseBytes            uint64     `json:"metadata_in_use_bytes"`
	NextGCBytes                   uint64     `json:"next_gc_bytes"`
	LastGarbageCollectionAt       *time.Time `json:"last_garbage_collection_at"`
	GarbageCollectionCount        uint32     `json:"garbage_collection_count"`
	ForcedGarbageCollectionCount  uint32     `json:"forced_garbage_collection_count"`
	GarbageCollectionPauseTotalMs float64    `json:"garbage_collection_pause_total_ms"`
	LastGarbageCollectionPauseMs  float64    `json:"last_garbage_collection_pause_ms"`
	GarbageCollectionCPUFraction  float64    `json:"garbage_collection_cpu_fraction"`
}

func collectRuntimeMetrics(capturedAt time.Time) runtimeMetricsSnapshot {
	var memoryStatistics runtime.MemStats
	runtime.ReadMemStats(&memoryStatistics)

	lastGarbageCollectionPauseNanoseconds := uint64(0)
	var lastGarbageCollectionAt *time.Time
	if memoryStatistics.NumGC > 0 {
		lastPauseIndex := (memoryStatistics.NumGC - 1) % uint32(len(memoryStatistics.PauseNs))
		lastGarbageCollectionPauseNanoseconds = memoryStatistics.PauseNs[lastPauseIndex]
		lastGarbageCollectionTime := time.Unix(0, int64(memoryStatistics.LastGC)).UTC()
		lastGarbageCollectionAt = &lastGarbageCollectionTime
	}

	return runtimeMetricsSnapshot{
		UptimeMs:          float64(capturedAt.Sub(processStartedAt)) / float64(time.Millisecond),
		GoVersion:         runtime.Version(),
		GoOperatingSystem: runtime.GOOS,
		GoArchitecture:    runtime.GOARCH,
		CPUCount:          runtime.NumCPU(),
		GOMAXPROCS:        runtime.GOMAXPROCS(0),
		Goroutines:        runtime.NumGoroutine(),
		CGOCalls:          runtime.NumCgoCall(),
		Memory: runtimeMemoryMetricsSnapshot{
			AllocatedBytes:                memoryStatistics.Alloc,
			TotalAllocatedBytes:           memoryStatistics.TotalAlloc,
			SystemBytes:                   memoryStatistics.Sys,
			MallocCount:                   memoryStatistics.Mallocs,
			FreeCount:                     memoryStatistics.Frees,
			HeapAllocatedBytes:            memoryStatistics.HeapAlloc,
			HeapSystemBytes:               memoryStatistics.HeapSys,
			HeapIdleBytes:                 memoryStatistics.HeapIdle,
			HeapInUseBytes:                memoryStatistics.HeapInuse,
			HeapReleasedBytes:             memoryStatistics.HeapReleased,
			HeapObjectCount:               memoryStatistics.HeapObjects,
			StackInUseBytes:               memoryStatistics.StackInuse,
			StackSystemBytes:              memoryStatistics.StackSys,
			MetadataInUseBytes:            memoryStatistics.MSpanInuse + memoryStatistics.MCacheInuse,
			NextGCBytes:                   memoryStatistics.NextGC,
			LastGarbageCollectionAt:       lastGarbageCollectionAt,
			GarbageCollectionCount:        memoryStatistics.NumGC,
			ForcedGarbageCollectionCount:  memoryStatistics.NumForcedGC,
			GarbageCollectionPauseTotalMs: float64(memoryStatistics.PauseTotalNs) / float64(time.Millisecond),
			LastGarbageCollectionPauseMs:  float64(lastGarbageCollectionPauseNanoseconds) / float64(time.Millisecond),
			GarbageCollectionCPUFraction:  memoryStatistics.GCCPUFraction,
		},
	}
}
