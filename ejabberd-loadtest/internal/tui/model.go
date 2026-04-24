package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vladimir-escoto/ejabberd-loadtest/internal/loadtest"
	"github.com/vladimir-escoto/ejabberd-loadtest/internal/sysinfo"
)

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type Model struct {
	Runner *loadtest.Runner

	width  int
	height int

	lastCounters snapshot
	history      []ratePoint
	bottleneck   loadtest.Bottleneck
	sys          sysinfo.Snapshot
	quitting     bool
	finalReport  string
}

type snapshot struct {
	at                                                                                   time.Time
	attempted, connected, authed, registered                                             int64
	sent, received                                                                       int64
	errConnRefused, errTimeout, errPorts, errFiles, errReset, errAuth, errReg, errOther,
	errStream int64
}

type ratePoint struct {
	at          time.Time
	connected   int64
	connectsPs  float64
	msgsPs      float64
}

func New(r *loadtest.Runner) Model {
	return Model{Runner: r, history: make([]ratePoint, 0, 600)}
}

func (m Model) Init() tea.Cmd { return tickCmd() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			m.Runner.Stop()
			return m, tea.Quit
		case "+", "=":
			m.Runner.AddUsers(100)
		case "*":
			m.Runner.AddUsers(1000)
		case "-", "_":
			m.Runner.AddUsers(-100)
		case "s":
			m.Runner.Stop()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		m.refresh()
		return m, tickCmd()
	}
	return m, nil
}

func (m *Model) refresh() {
	c := m.Runner.Metrics.Counters
	now := time.Now()
	cur := snapshot{
		at:             now,
		attempted:      c.Attempted.Load(),
		connected:      c.Connected.Load(),
		authed:         c.Authed.Load(),
		registered:     c.Registered.Load(),
		sent:           c.MessagesSent.Load(),
		received:       c.MessagesReceived.Load(),
		errConnRefused: c.ErrConnRefused.Load(),
		errTimeout:     c.ErrConnTimeout.Load(),
		errPorts:       c.ErrPortExhausted.Load(),
		errFiles:       c.ErrTooManyFiles.Load(),
		errReset:       c.ErrResetByPeer.Load(),
		errAuth:        c.ErrAuth.Load(),
		errReg:         c.ErrRegister.Load(),
		errStream:      c.ErrStreamClosed.Load(),
		errOther:       c.ErrOther.Load(),
	}
	var connectsPs, msgsPs float64
	if !m.lastCounters.at.IsZero() {
		dt := cur.at.Sub(m.lastCounters.at).Seconds()
		if dt > 0 {
			connectsPs = float64(cur.authed-m.lastCounters.authed) / dt
			msgsPs = float64(cur.sent-m.lastCounters.sent) / dt
		}
	}
	m.history = append(m.history, ratePoint{
		at: now, connected: cur.connected, connectsPs: connectsPs, msgsPs: msgsPs,
	})
	if len(m.history) > 600 {
		m.history = m.history[len(m.history)-600:]
	}

	m.sys = sysinfo.Take()

	// diag window: deltas over last ~2 seconds
	var recent loadtest.DiagInput
	cut := now.Add(-2 * time.Second)
	var baseIdx int
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].at.Before(cut) {
			baseIdx = i
			break
		}
	}
	_ = baseIdx
	// use deltas from lastCounters as the "recent" window (every tick)
	recent.RecentErrConnRefused = cur.errConnRefused - m.lastCounters.errConnRefused
	recent.RecentErrConnTimeout = cur.errTimeout - m.lastCounters.errTimeout
	recent.RecentErrPortExhausted = cur.errPorts - m.lastCounters.errPorts
	recent.RecentErrTooManyFiles = cur.errFiles - m.lastCounters.errFiles
	recent.RecentErrResetByPeer = cur.errReset - m.lastCounters.errReset
	recent.RecentErrAuth = cur.errAuth - m.lastCounters.errAuth
	recent.RecentErrStreamClosed = cur.errStream - m.lastCounters.errStream
	recent.RecentSuccess = cur.authed - m.lastCounters.authed
	if m.sys.OpenFDLimit > 0 {
		// approximate used FDs as connected sockets + overhead
		used := float64(cur.connected + 20)
		recent.FDUsedPct = used / float64(m.sys.OpenFDLimit)
	}
	m.bottleneck = loadtest.Diagnose(recent)

	m.lastCounters = cur
}

var (
	titleSty = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	keySty   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	bottleSty = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	okSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dimSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	boxSty   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func (m Model) View() string {
	if m.quitting {
		if m.finalReport != "" {
			return m.finalReport
		}
		return "shutting down..."
	}
	c := m.lastCounters
	cfg := m.Runner.Config()
	elapsed := time.Since(m.Runner.Metrics.Started).Round(time.Second)

	var bott string
	if m.bottleneck == loadtest.BottleneckNone {
		bott = okSty.Render(m.bottleneck.String())
	} else {
		bott = bottleSty.Render(m.bottleneck.String())
	}

	header := titleSty.Render(fmt.Sprintf(
		"ejabberd-loadtest  target=%s:%d  domain=%s  elapsed=%s",
		cfg.Host, cfg.Port, cfg.Domain, elapsed,
	))

	left := fmt.Sprintf(
		"attempted:   %d\nconnected:   %d  (live)\nauthed:      %d\nregistered:  %d\nsent:        %d\nreceived:    %d",
		c.attempted, c.connected, c.authed, c.registered, c.sent, c.received,
	)
	errs := fmt.Sprintf(
		"conn-refused: %d\ntimeout:      %d\nport-exhaust: %d\nfd-limit:     %d\nreset:        %d\nauth-fail:    %d\nreg-fail:     %d\nstream-drop:  %d\nother:        %d",
		c.errConnRefused, c.errTimeout, c.errPorts, c.errFiles, c.errReset, c.errAuth, c.errReg, c.errStream, c.errOther,
	)
	sys := fmt.Sprintf(
		"goroutines:  %d\nalloc:       %s\nrss:         %s\ncpu-cores:   %d\nfd-soft-lim: %s",
		m.sys.Goroutines, humanBytes(m.sys.AllocBytes), humanBytes(m.sys.SysBytes), m.sys.NumCPU, fdStr(m.sys.OpenFDLimit),
	)

	// rates from last tick
	var connRate, msgRate float64
	if len(m.history) >= 2 {
		last := m.history[len(m.history)-1]
		connRate = last.connectsPs
		msgRate = last.msgsPs
	}
	rates := fmt.Sprintf(
		"connects/s:  %.1f\nmessages/s:  %.1f",
		connRate, msgRate,
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		boxSty.Render(titleSty.Render("flow")+"\n"+left),
		boxSty.Render(titleSty.Render("errors")+"\n"+errs),
		boxSty.Render(titleSty.Render("client")+"\n"+sys),
		boxSty.Render(titleSty.Render("rates")+"\n"+rates),
	)

	graph := m.renderGraph(60, 8)

	help := dimSty.Render(fmt.Sprintf(
		"[%s] +100   [%s] +1000   [%s] -100   [%s] stop ramp   [%s] quit",
		keySty.Render("+"), keySty.Render("*"), keySty.Render("-"),
		keySty.Render("s"), keySty.Render("q"),
	))

	verdict := fmt.Sprintf("bottleneck: %s", bott)

	return strings.Join([]string{header, body, titleSty.Render("live connections"), graph, verdict, help}, "\n")
}

func (m Model) renderGraph(w, h int) string {
	if len(m.history) < 2 {
		return dimSty.Render("  (gathering data...)")
	}
	var maxV int64 = 1
	for _, p := range m.history {
		if p.connected > maxV {
			maxV = p.connected
		}
	}
	// take last w points
	pts := m.history
	if len(pts) > w {
		pts = pts[len(pts)-w:]
	}
	// build char grid
	rows := make([][]rune, h)
	for i := range rows {
		rows[i] = make([]rune, len(pts))
		for j := range rows[i] {
			rows[i][j] = ' '
		}
	}
	for j, p := range pts {
		height := int(float64(p.connected) / float64(maxV) * float64(h))
		if height > h {
			height = h
		}
		for i := 0; i < height; i++ {
			rows[h-1-i][j] = '█'
		}
	}
	var b strings.Builder
	for i, r := range rows {
		if i == 0 {
			b.WriteString(fmt.Sprintf("%6d ", maxV))
		} else if i == h-1 {
			b.WriteString("     0 ")
		} else {
			b.WriteString("       ")
		}
		b.WriteString(string(r))
		b.WriteByte('\n')
	}
	return b.String()
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func fdStr(n uint64) string {
	if n == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d", n)
}
