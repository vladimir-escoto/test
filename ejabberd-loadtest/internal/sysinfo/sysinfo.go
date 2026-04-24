package sysinfo

import (
	"runtime"
)

type Snapshot struct {
	Goroutines  int
	AllocBytes  uint64
	SysBytes    uint64
	NumCPU      int
	OpenFDLimit uint64
}

func Take() Snapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Snapshot{
		Goroutines:  runtime.NumGoroutine(),
		AllocBytes:  m.Alloc,
		SysBytes:    m.Sys,
		NumCPU:      runtime.NumCPU(),
		OpenFDLimit: fdLimit(),
	}
}
