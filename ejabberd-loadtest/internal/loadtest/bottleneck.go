package loadtest

import (
	"errors"
	"net"
	"strings"
)

// ErrorClass classifies a dial/auth error into a failure bucket.
type ErrorClass int

const (
	ErrClassOther ErrorClass = iota
	ErrClassConnRefused
	ErrClassConnTimeout
	ErrClassPortExhausted
	ErrClassTooManyFiles
	ErrClassResetByPeer
	ErrClassAuth
	ErrClassRegister
	ErrClassStreamClosed
)

// ClassifyNetErr maps a connect/dial error to a bucket.
func ClassifyNetErr(err error) ErrorClass {
	if err == nil {
		return ErrClassOther
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrClassConnTimeout
	}
	if c := classifyErrno(err); c != ErrClassOther {
		return c
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "cannot assign requested address"),
		strings.Contains(msg, "only one usage of each socket address"):
		return ErrClassPortExhausted
	case strings.Contains(msg, "too many open files"):
		return ErrClassTooManyFiles
	case strings.Contains(msg, "connection refused"):
		return ErrClassConnRefused
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "forcibly closed"):
		return ErrClassResetByPeer
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
		return ErrClassConnTimeout
	case strings.Contains(msg, "sasl"):
		return ErrClassAuth
	}
	return ErrClassOther
}

func (c *Counters) RecordErr(class ErrorClass) {
	switch class {
	case ErrClassConnRefused:
		c.ErrConnRefused.Add(1)
	case ErrClassConnTimeout:
		c.ErrConnTimeout.Add(1)
	case ErrClassPortExhausted:
		c.ErrPortExhausted.Add(1)
	case ErrClassTooManyFiles:
		c.ErrTooManyFiles.Add(1)
	case ErrClassResetByPeer:
		c.ErrResetByPeer.Add(1)
	case ErrClassAuth:
		c.ErrAuth.Add(1)
	case ErrClassRegister:
		c.ErrRegister.Add(1)
	case ErrClassStreamClosed:
		c.ErrStreamClosed.Add(1)
	default:
		c.ErrOther.Add(1)
	}
}

// Bottleneck is the runner's verdict about why the test can't grow further.
type Bottleneck int

const (
	BottleneckNone Bottleneck = iota
	BottleneckLocalFD
	BottleneckLocalPorts
	BottleneckLocalCPU
	BottleneckServerRefusing
	BottleneckServerTimeout
	BottleneckServerResetting
	BottleneckServerAuth
	BottleneckUnknown
)

func (b Bottleneck) String() string {
	switch b {
	case BottleneckNone:
		return "none (system still has headroom)"
	case BottleneckLocalFD:
		return "LOCAL: file descriptors exhausted (EMFILE)"
	case BottleneckLocalPorts:
		return "LOCAL: ephemeral ports exhausted (EADDRNOTAVAIL)"
	case BottleneckLocalCPU:
		return "LOCAL: client CPU / goroutine saturation"
	case BottleneckServerRefusing:
		return "SERVER: refusing connections (ECONNREFUSED / backlog full)"
	case BottleneckServerTimeout:
		return "SERVER: slow or unresponsive (connect/auth timeouts)"
	case BottleneckServerResetting:
		return "SERVER: closing/resetting streams mid-session"
	case BottleneckServerAuth:
		return "SERVER: auth rejections (possibly module overload)"
	default:
		return "unknown"
	}
}

type DiagInput struct {
	RecentErrConnRefused   int64
	RecentErrConnTimeout   int64
	RecentErrPortExhausted int64
	RecentErrTooManyFiles  int64
	RecentErrResetByPeer   int64
	RecentErrAuth          int64
	RecentErrStreamClosed  int64
	RecentSuccess          int64
	LocalCPUPct            float64
	FDUsedPct              float64
}

func Diagnose(d DiagInput) Bottleneck {
	total := d.RecentErrConnRefused + d.RecentErrConnTimeout + d.RecentErrPortExhausted +
		d.RecentErrTooManyFiles + d.RecentErrResetByPeer + d.RecentErrAuth + d.RecentErrStreamClosed
	if total == 0 && d.RecentSuccess > 0 {
		if d.LocalCPUPct > 90 {
			return BottleneckLocalCPU
		}
		return BottleneckNone
	}
	if d.RecentErrTooManyFiles > 0 || d.FDUsedPct > 0.95 {
		return BottleneckLocalFD
	}
	if d.RecentErrPortExhausted > 0 {
		return BottleneckLocalPorts
	}
	if total == 0 {
		if d.LocalCPUPct > 90 {
			return BottleneckLocalCPU
		}
		return BottleneckNone
	}
	top := d.RecentErrConnRefused
	class := BottleneckServerRefusing
	if d.RecentErrConnTimeout > top {
		top = d.RecentErrConnTimeout
		class = BottleneckServerTimeout
	}
	if d.RecentErrResetByPeer+d.RecentErrStreamClosed > top {
		top = d.RecentErrResetByPeer + d.RecentErrStreamClosed
		class = BottleneckServerResetting
	}
	if d.RecentErrAuth > top {
		top = d.RecentErrAuth
		class = BottleneckServerAuth
	}
	if top == 0 {
		return BottleneckUnknown
	}
	return class
}
