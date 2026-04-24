package loadtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// WriteReport prints a human-readable summary to w.
func WriteReport(w io.Writer, r *Runner, verdict Bottleneck) {
	m := r.Metrics
	cfg := r.Config()
	c := m.Counters

	elapsed := time.Since(m.Started)
	connect := m.Connect.Snapshot()
	auth := m.Auth.Snapshot()
	reg := m.Register.Snapshot()
	rtt := m.MsgRTT.Snapshot()

	fmt.Fprintln(w, "==========================================================")
	fmt.Fprintln(w, "  ejabberd load-test — final report")
	fmt.Fprintln(w, "==========================================================")
	fmt.Fprintf(w, "target            : %s:%d (%s)\n", cfg.Host, cfg.Port, cfg.Domain)
	fmt.Fprintf(w, "elapsed           : %s\n", elapsed.Round(time.Second))
	fmt.Fprintf(w, "target connections: %d\n", cfg.TargetConnections)
	fmt.Fprintf(w, "ramp              : %d/s\n", cfg.RampPerSec)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "--- counts ---")
	fmt.Fprintf(w, "attempted         : %d\n", c.Attempted.Load())
	fmt.Fprintf(w, "registered        : %d (reg errors: %d)\n", c.Registered.Load(), c.ErrRegister.Load())
	fmt.Fprintf(w, "authed (total)    : %d\n", c.Authed.Load())
	fmt.Fprintf(w, "peak live conns   : %d\n", c.Connected.Load()+c.Disconnected.Load()) // approximate upper bound
	fmt.Fprintf(w, "live at end       : %d\n", c.Connected.Load())
	fmt.Fprintf(w, "messages sent     : %d\n", c.MessagesSent.Load())
	fmt.Fprintf(w, "messages received : %d\n", c.MessagesReceived.Load())
	if s := c.MessagesSent.Load(); s > 0 {
		pct := float64(c.MessagesReceived.Load()) * 100 / float64(s)
		fmt.Fprintf(w, "delivery          : %.2f%%\n", pct)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "--- timings (ms) ---")
	writeHist(w, "TCP+stream connect ", connect)
	writeHist(w, "SASL+bind auth     ", auth)
	writeHist(w, "in-band register   ", reg)
	writeHist(w, "message RTT (echo) ", rtt)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "--- error breakdown ---")
	fmt.Fprintf(w, "conn-refused      : %d\n", c.ErrConnRefused.Load())
	fmt.Fprintf(w, "conn-timeout      : %d\n", c.ErrConnTimeout.Load())
	fmt.Fprintf(w, "port-exhausted    : %d   (local: EADDRNOTAVAIL)\n", c.ErrPortExhausted.Load())
	fmt.Fprintf(w, "too-many-files    : %d   (local: EMFILE)\n", c.ErrTooManyFiles.Load())
	fmt.Fprintf(w, "reset-by-peer     : %d\n", c.ErrResetByPeer.Load())
	fmt.Fprintf(w, "auth-failures     : %d\n", c.ErrAuth.Load())
	fmt.Fprintf(w, "register-failures : %d\n", c.ErrRegister.Load())
	fmt.Fprintf(w, "stream-drops      : %d\n", c.ErrStreamClosed.Load())
	fmt.Fprintf(w, "other             : %d\n", c.ErrOther.Load())
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "--- verdict ---")
	fmt.Fprintf(w, "%s\n", verdict)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "interpretation:")
	switch verdict {
	case BottleneckLocalFD, BottleneckLocalPorts, BottleneckLocalCPU:
		fmt.Fprintln(w, "  the CLIENT MACHINE hit its limit before the server did.")
		fmt.Fprintln(w, "  increase ulimit -n, widen ephemeral port range, or split load across machines.")
	case BottleneckServerRefusing, BottleneckServerTimeout, BottleneckServerResetting, BottleneckServerAuth:
		fmt.Fprintln(w, "  the SERVER is the limiting factor at this load.")
		fmt.Fprintln(w, "  check ejabberd logs, max_fsm_queue, c2s listener backlog, and mod_register rate limits.")
	case BottleneckNone:
		fmt.Fprintln(w, "  no saturation observed — the test can be pushed higher.")
	}
	fmt.Fprintln(w, "==========================================================")
}

func writeHist(w io.Writer, label string, s HistStats) {
	if s.Count == 0 {
		fmt.Fprintf(w, "%s n=0\n", label)
		return
	}
	fmt.Fprintf(w, "%s n=%d  mean=%.1f  p50=%.1f  p90=%.1f  p95=%.1f  p99=%.1f  max=%.1f\n",
		label, s.Count,
		float64(s.Mean)/float64(time.Millisecond),
		float64(s.P50)/float64(time.Millisecond),
		float64(s.P90)/float64(time.Millisecond),
		float64(s.P95)/float64(time.Millisecond),
		float64(s.P99)/float64(time.Millisecond),
		float64(s.Max)/float64(time.Millisecond),
	)
}

// WriteCSV writes a row-per-metric CSV for post-analysis.
func WriteCSV(path string, r *Runner, verdict Bottleneck) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	defer cw.Flush()
	cw.Write([]string{"metric", "count", "mean_ms", "p50_ms", "p90_ms", "p95_ms", "p99_ms", "max_ms"})
	rows := []struct {
		name string
		h    HistStats
	}{
		{"connect", r.Metrics.Connect.Snapshot()},
		{"auth", r.Metrics.Auth.Snapshot()},
		{"register", r.Metrics.Register.Snapshot()},
		{"msg_rtt", r.Metrics.MsgRTT.Snapshot()},
	}
	for _, row := range rows {
		cw.Write([]string{
			row.name,
			strconv.Itoa(row.h.Count),
			fmtMs(row.h.Mean), fmtMs(row.h.P50), fmtMs(row.h.P90),
			fmtMs(row.h.P95), fmtMs(row.h.P99), fmtMs(row.h.Max),
		})
	}
	cw.Write([]string{})
	c := r.Metrics.Counters
	cw.Write([]string{"counter", "value"})
	pairs := [][2]string{
		{"attempted", strconv.FormatInt(c.Attempted.Load(), 10)},
		{"registered", strconv.FormatInt(c.Registered.Load(), 10)},
		{"authed", strconv.FormatInt(c.Authed.Load(), 10)},
		{"live_at_end", strconv.FormatInt(c.Connected.Load(), 10)},
		{"messages_sent", strconv.FormatInt(c.MessagesSent.Load(), 10)},
		{"messages_received", strconv.FormatInt(c.MessagesReceived.Load(), 10)},
		{"err_conn_refused", strconv.FormatInt(c.ErrConnRefused.Load(), 10)},
		{"err_timeout", strconv.FormatInt(c.ErrConnTimeout.Load(), 10)},
		{"err_port_exhausted", strconv.FormatInt(c.ErrPortExhausted.Load(), 10)},
		{"err_too_many_files", strconv.FormatInt(c.ErrTooManyFiles.Load(), 10)},
		{"err_reset_by_peer", strconv.FormatInt(c.ErrResetByPeer.Load(), 10)},
		{"err_auth", strconv.FormatInt(c.ErrAuth.Load(), 10)},
		{"err_register", strconv.FormatInt(c.ErrRegister.Load(), 10)},
		{"err_stream_closed", strconv.FormatInt(c.ErrStreamClosed.Load(), 10)},
		{"err_other", strconv.FormatInt(c.ErrOther.Load(), 10)},
		{"verdict", verdict.String()},
	}
	for _, p := range pairs {
		cw.Write([]string{p[0], p[1]})
	}
	return nil
}

func fmtMs(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 2, 64)
}
