package loadtest

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Counters holds atomic counters updated by worker goroutines.
type Counters struct {
	Attempted    atomic.Int64
	Connected    atomic.Int64 // currently live XMPP sessions
	Registered   atomic.Int64
	Authed       atomic.Int64
	Disconnected atomic.Int64

	MessagesSent     atomic.Int64
	MessagesReceived atomic.Int64

	// Failure buckets for bottleneck classification
	ErrConnRefused    atomic.Int64 // server not accepting
	ErrConnTimeout    atomic.Int64 // server slow / not accepting fast enough
	ErrPortExhausted  atomic.Int64 // local: EADDRNOTAVAIL / EADDRINUSE
	ErrTooManyFiles   atomic.Int64 // local: EMFILE / ENFILE
	ErrResetByPeer    atomic.Int64 // server dropped
	ErrAuth           atomic.Int64 // server rejected auth
	ErrRegister       atomic.Int64 // server rejected registration
	ErrStreamClosed   atomic.Int64 // server closed stream mid-session
	ErrOther          atomic.Int64
}

// Sample is a timed observation.
type Sample struct {
	At    time.Time
	Value time.Duration
}

// Hist is a thread-safe histogram that stores all samples.
// For loads under a few million samples this is fine.
type Hist struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (h *Hist) Add(v time.Duration) {
	h.mu.Lock()
	h.samples = append(h.samples, v)
	h.mu.Unlock()
}

func (h *Hist) Snapshot() HistStats {
	h.mu.Lock()
	n := len(h.samples)
	cp := make([]time.Duration, n)
	copy(cp, h.samples)
	h.mu.Unlock()
	return computeStats(cp)
}

type HistStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Mean  time.Duration
	P50   time.Duration
	P90   time.Duration
	P95   time.Duration
	P99   time.Duration
	StdDev time.Duration
}

func computeStats(xs []time.Duration) HistStats {
	s := HistStats{Count: len(xs)}
	if len(xs) == 0 {
		return s
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	s.Min = xs[0]
	s.Max = xs[len(xs)-1]
	var sum float64
	for _, x := range xs {
		sum += float64(x)
	}
	mean := sum / float64(len(xs))
	s.Mean = time.Duration(mean)
	var sq float64
	for _, x := range xs {
		d := float64(x) - mean
		sq += d * d
	}
	s.StdDev = time.Duration(math.Sqrt(sq / float64(len(xs))))
	pct := func(p float64) time.Duration {
		if len(xs) == 0 {
			return 0
		}
		idx := int(math.Ceil(p/100.0*float64(len(xs)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(xs) {
			idx = len(xs) - 1
		}
		return xs[idx]
	}
	s.P50 = pct(50)
	s.P90 = pct(90)
	s.P95 = pct(95)
	s.P99 = pct(99)
	return s
}

// Metrics aggregates counters and histograms for the whole run.
type Metrics struct {
	Counters *Counters
	Connect  *Hist // TCP + stream open
	Auth     *Hist // Full authenticate (post-connect)
	Register *Hist // Registration (post-connect)
	MsgRTT   *Hist // Echo RTT: send -> receive on peer
	Started  time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		Counters: &Counters{},
		Connect:  &Hist{},
		Auth:     &Hist{},
		Register: &Hist{},
		MsgRTT:   &Hist{},
		Started:  time.Now(),
	}
}
