//go:build windows

package sysinfo

// Windows does not expose a per-process FD soft limit like unix RLIMIT_NOFILE.
// Handle counts are very high by default; we report 0 to mean "not applicable".
func fdLimit() uint64 { return 0 }

func RaiseFDLimit() (uint64, error) { return 0, nil }
