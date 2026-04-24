package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vladimir-escoto/ejabberd-loadtest/internal/loadtest"
	"github.com/vladimir-escoto/ejabberd-loadtest/internal/sysinfo"
	"github.com/vladimir-escoto/ejabberd-loadtest/internal/tui"
)

func main() {
	cfg := loadtest.Config{}

	flag.StringVar(&cfg.Host, "host", "testqa.tripleenableverified.com", "ejabberd host (TCP c2s)")
	flag.IntVar(&cfg.Port, "port", 5222, "ejabberd c2s port (plain)")
	flag.StringVar(&cfg.Domain, "domain", "testqa.tripleenableverified.com", "XMPP domain")
	flag.StringVar(&cfg.UserPrefix, "user-prefix", "lt", "username prefix (will become <prefix><index>)")
	flag.StringVar(&cfg.Password, "password", "loadtest", "password for all load-test users")
	flag.IntVar(&cfg.TargetConnections, "target", 1000, "initial target number of connections")
	flag.IntVar(&cfg.RampPerSec, "ramp", 100, "ramp-up rate (new connections per second)")
	flag.DurationVar(&cfg.ConnectTimeout, "connect-timeout", 10*time.Second, "TCP+stream connect timeout")
	flag.DurationVar(&cfg.AuthTimeout, "auth-timeout", 10*time.Second, "auth/register timeout")
	flag.IntVar(&cfg.MessageIntervalMs, "msg-interval-ms", 5000, "per-user interval between messages (ms)")
	flag.StringVar(&cfg.MessageBody, "msg-body", "ping", "default message body")
	flag.BoolVar(&cfg.PairUsers, "pair", true, "pair users (even/odd) to exchange messages")
	flag.BoolVar(&cfg.SkipRegister, "skip-register", false, "do not register users (assume they exist)")
	flag.StringVar(&cfg.RegisterMode, "register-mode", "http", "registration mode: http or xmpp")
	flag.StringVar(&cfg.RegisterURL, "register-url", "https://testqa.tripleenableverified.com:5443/api/register", "ejabberd mod_http_api register endpoint")
	flag.BoolVar(&cfg.RegisterInsecure, "register-insecure", true, "skip TLS verify on register API")
	flag.StringVar(&cfg.RegisterAPIUser, "register-api-user", "", "admin user for register API (optional basic auth)")
	flag.StringVar(&cfg.RegisterAPIPass, "register-api-pass", "", "admin password for register API (optional)")
	flag.BoolVar(&cfg.RaiseFDLimit, "raise-fd", true, "raise RLIMIT_NOFILE to max at startup (unix)")
	flag.StringVar(&cfg.ReportCSVPath, "csv", "loadtest-report.csv", "final CSV path (empty to skip)")
	headless := flag.Bool("headless", false, "run without TUI, print final report only")
	duration := flag.Duration("duration", 0, "auto-stop after this duration (0 = run until q/Ctrl-C)")

	flag.Parse()

	if cfg.RaiseFDLimit && runtime.GOOS != "windows" {
		if lim, err := sysinfo.RaiseFDLimit(); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not raise FD limit: %v (soft=%d)\n", err, lim)
		}
	}

	// Sanity banner when headless (non-TUI mode will print this).
	r := loadtest.NewRunner(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		r.Stop()
		cancel()
	}()

	if *duration > 0 {
		go func() {
			time.Sleep(*duration)
			r.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	if *headless {
		fmt.Printf("loadtest -> %s:%d domain=%s target=%d ramp=%d/s mode=%s\n",
			cfg.Host, cfg.Port, cfg.Domain, cfg.TargetConnections, cfg.RampPerSec, cfg.RegisterMode)
		fmt.Println("press Ctrl-C to stop and print report")
		// Print periodic progress
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
	loop:
		for {
			select {
			case <-done:
				break loop
			case <-ticker.C:
				c := r.Metrics.Counters
				fmt.Printf("  live=%d authed=%d sent=%d recv=%d errs{refused=%d timeout=%d ports=%d fds=%d reset=%d auth=%d reg=%d stream=%d}\n",
					c.Connected.Load(), c.Authed.Load(), c.MessagesSent.Load(), c.MessagesReceived.Load(),
					c.ErrConnRefused.Load(), c.ErrConnTimeout.Load(), c.ErrPortExhausted.Load(),
					c.ErrTooManyFiles.Load(), c.ErrResetByPeer.Load(), c.ErrAuth.Load(),
					c.ErrRegister.Load(), c.ErrStreamClosed.Load())
			}
		}
	} else {
		m := tui.New(r)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui error:", err)
		}
		r.Stop()
		cancel()
		// give workers a moment to unwind
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}

	verdict := finalVerdict(r)
	loadtest.WriteReport(os.Stdout, r, verdict)
	if cfg.ReportCSVPath != "" {
		if err := loadtest.WriteCSV(cfg.ReportCSVPath, r, verdict); err != nil {
			fmt.Fprintln(os.Stderr, "csv error:", err)
		} else {
			fmt.Printf("csv written: %s\n", cfg.ReportCSVPath)
		}
	}
}

func finalVerdict(r *loadtest.Runner) loadtest.Bottleneck {
	c := r.Metrics.Counters
	sys := sysinfo.Take()
	var fdPct float64
	if sys.OpenFDLimit > 0 {
		fdPct = float64(c.Connected.Load()+20) / float64(sys.OpenFDLimit)
	}
	return loadtest.Diagnose(loadtest.DiagInput{
		RecentErrConnRefused:   c.ErrConnRefused.Load(),
		RecentErrConnTimeout:   c.ErrConnTimeout.Load(),
		RecentErrPortExhausted: c.ErrPortExhausted.Load(),
		RecentErrTooManyFiles:  c.ErrTooManyFiles.Load(),
		RecentErrResetByPeer:   c.ErrResetByPeer.Load(),
		RecentErrAuth:          c.ErrAuth.Load(),
		RecentErrStreamClosed:  c.ErrStreamClosed.Load(),
		RecentSuccess:          c.Authed.Load(),
		FDUsedPct:              fdPct,
	})
}
