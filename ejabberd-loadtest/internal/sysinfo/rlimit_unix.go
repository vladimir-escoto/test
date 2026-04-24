//go:build !windows

package sysinfo

import "syscall"

func fdLimit() uint64 {
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		return 0
	}
	return rl.Cur
}

// RaiseFDLimit tries to raise the soft limit to the hard limit and returns
// the resulting soft limit.
func RaiseFDLimit() (uint64, error) {
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		return 0, err
	}
	rl.Cur = rl.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		return rl.Cur, err
	}
	return rl.Cur, nil
}
