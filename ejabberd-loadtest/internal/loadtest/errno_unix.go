//go:build !windows

package loadtest

import (
	"errors"
	"syscall"
)

func classifyErrno(err error) ErrorClass {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return ErrClassOther
	}
	switch en {
	case syscall.ECONNREFUSED:
		return ErrClassConnRefused
	case syscall.ECONNRESET:
		return ErrClassResetByPeer
	case syscall.EADDRNOTAVAIL, syscall.EADDRINUSE:
		return ErrClassPortExhausted
	case syscall.EMFILE, syscall.ENFILE:
		return ErrClassTooManyFiles
	case syscall.ETIMEDOUT:
		return ErrClassConnTimeout
	}
	return ErrClassOther
}
