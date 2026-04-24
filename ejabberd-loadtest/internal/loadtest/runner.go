package loadtest

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vladimir-escoto/ejabberd-loadtest/internal/xmpp"
)

// Runner drives the load test.
type Runner struct {
	cfg     Config
	Metrics *Metrics

	mu      sync.Mutex
	clients map[int]*xmpp.Client // keyed by user index
	// partnerOf maps user index -> partner index (for PairUsers mode).
	partnerOf map[int]int

	httpReg *xmpp.HTTPRegistrar

	// ramp control
	targetDelta atomic.Int64 // extra users to add beyond current target
	stopped     atomic.Bool
}

func NewRunner(cfg Config) *Runner {
	cfg.Defaults()
	r := &Runner{
		cfg:       cfg,
		Metrics:   NewMetrics(),
		clients:   make(map[int]*xmpp.Client),
		partnerOf: make(map[int]int),
	}
	if cfg.RegisterMode == "http" && cfg.RegisterURL != "" {
		hr := xmpp.NewHTTPRegistrar(cfg.RegisterURL, cfg.Domain, cfg.RegisterInsecure)
		hr.AdminUser = cfg.RegisterAPIUser
		hr.AdminPass = cfg.RegisterAPIPass
		hr.Timeout = cfg.AuthTimeout
		r.httpReg = hr
	}
	return r
}

func (r *Runner) Config() Config { return r.cfg }

// AddUsers increases the running target by n.
func (r *Runner) AddUsers(n int) { r.targetDelta.Add(int64(n)) }

// Stop signals all workers to disconnect gracefully.
func (r *Runner) Stop() { r.stopped.Store(true) }

// Run launches the ramp-up loop. It returns when ctx is cancelled or Stop
// is called and all workers exit.
func (r *Runner) Run(ctx context.Context) {
	var spawned int64
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// workers we've launched; tracked for shutdown.
	var wg sync.WaitGroup

	perTick := max(1, r.cfg.RampPerSec/10)

	for {
		if r.stopped.Load() {
			break
		}
		select {
		case <-ctx.Done():
			r.stopped.Store(true)
			goto shutdown
		case <-ticker.C:
		}
		target := int64(r.cfg.TargetConnections) + r.targetDelta.Load()
		if spawned >= target {
			continue
		}
		toSpawn := int64(perTick)
		if spawned+toSpawn > target {
			toSpawn = target - spawned
		}
		for i := int64(0); i < toSpawn; i++ {
			idx := int(spawned + i)
			wg.Add(1)
			go r.worker(ctx, idx, &wg)
		}
		spawned += toSpawn
	}

shutdown:
	wg.Wait()
}

// worker runs the lifecycle of a single simulated user.
func (r *Runner) worker(ctx context.Context, idx int, wg *sync.WaitGroup) {
	defer wg.Done()
	r.Metrics.Counters.Attempted.Add(1)

	user := fmt.Sprintf("%s%d", r.cfg.UserPrefix, idx)
	pass := r.cfg.Password

	// Registration (optional) uses its own short-lived connection.
	if !r.cfg.SkipRegister {
		if err := r.register(ctx, user, pass); err != nil {
			r.Metrics.Counters.ErrRegister.Add(1)
			// If registration fails we still try to authenticate (user may
			// already exist from a previous run); real failures will then
			// surface as auth errors.
		}
	}

	// Main session.
	client, err := r.connectAndAuth(ctx, user, pass)
	if err != nil {
		class := ClassifyNetErr(err)
		r.Metrics.Counters.RecordErr(class)
		return
	}
	r.Metrics.Counters.Connected.Add(1)
	r.Metrics.Counters.Authed.Add(1)

	r.mu.Lock()
	r.clients[idx] = client
	// pair even idx with idx+1
	if r.cfg.PairUsers {
		partner := idx ^ 1
		r.partnerOf[idx] = partner
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.clients, idx)
		delete(r.partnerOf, idx)
		r.mu.Unlock()
		_ = client.Close()
		r.Metrics.Counters.Connected.Add(-1)
		r.Metrics.Counters.Disconnected.Add(1)
	}()

	// incoming reader
	readCtx, readCancel := context.WithCancel(ctx)
	defer readCancel()
	readErr := make(chan error, 1)
	go func() { readErr <- client.ReadLoop(readCtx) }()

	// Pending outgoing messages for RTT (id -> send time).
	var pendMu sync.Mutex
	pending := make(map[string]time.Time)

	// Receive goroutine: match incoming body against pending map.
	go func() {
		for m := range client.Incoming() {
			r.Metrics.Counters.MessagesReceived.Add(1)
			// RTT is recorded if body is of the form "ping:<id>" that we sent.
			// Since this worker receives from peers (or from itself in solo
			// mode) we look up the id by body suffix.
			var id string
			if len(m.Body) > 5 && m.Body[:5] == "ping:" {
				id = m.Body[5:]
			}
			if id == "" {
				continue
			}
			pendMu.Lock()
			t0, ok := pending[id]
			if ok {
				delete(pending, id)
			}
			pendMu.Unlock()
			if ok {
				r.Metrics.MsgRTT.Add(time.Since(t0))
			}
		}
	}()

	if err := client.SendPresence(); err != nil {
		class := ClassifyNetErr(err)
		r.Metrics.Counters.RecordErr(class)
		return
	}

	interval := time.Duration(r.cfg.MessageIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Second
	}
	// Jitter so workers don't synchronize.
	jitter := time.Duration(rand.Int63n(int64(interval)))
	time.Sleep(jitter / 4)

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		if r.stopped.Load() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case err := <-readErr:
			if err != nil {
				r.Metrics.Counters.ErrStreamClosed.Add(1)
			}
			return
		case <-tick.C:
		}
		to := r.targetJID(idx)
		if to == "" {
			// No peer available yet; keep waiting.
			continue
		}
		id := fmt.Sprintf("%d-%d", idx, time.Now().UnixNano())
		body := fmt.Sprintf("ping:%s", id)
		pendMu.Lock()
		pending[id] = time.Now()
		pendMu.Unlock()
		if err := client.SendMessage(to, body, id); err != nil {
			r.Metrics.Counters.ErrStreamClosed.Add(1)
			return
		}
		r.Metrics.Counters.MessagesSent.Add(1)
	}
}

func (r *Runner) targetJID(idx int) string {
	if !r.cfg.PairUsers {
		// Send to self (server echoes through own session if allowed, else
		// lands in offline storage — still useful to generate traffic).
		return fmt.Sprintf("%s%d@%s", r.cfg.UserPrefix, idx, r.cfg.Domain)
	}
	partner := idx ^ 1
	// only send if partner is connected
	r.mu.Lock()
	_, ok := r.clients[partner]
	r.mu.Unlock()
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s%d@%s", r.cfg.UserPrefix, partner, r.cfg.Domain)
}

func (r *Runner) register(ctx context.Context, user, pass string) error {
	t0 := time.Now()
	if r.httpReg != nil {
		ctxD, cancel := context.WithTimeout(ctx, r.cfg.AuthTimeout)
		defer cancel()
		err := r.httpReg.Register(ctxD, user, pass)
		r.Metrics.Register.Add(time.Since(t0))
		if err == nil {
			r.Metrics.Counters.Registered.Add(1)
		}
		return err
	}
	ctxD, cancel := context.WithTimeout(ctx, r.cfg.ConnectTimeout)
	defer cancel()
	c, err := xmpp.Dial(ctxD, r.cfg.Host, r.cfg.Port, r.cfg.Domain, r.cfg.ConnectTimeout)
	if err != nil {
		return err
	}
	defer c.Close()
	err = c.Register(user, pass, r.cfg.AuthTimeout)
	r.Metrics.Register.Add(time.Since(t0))
	if err == nil {
		r.Metrics.Counters.Registered.Add(1)
	}
	return err
}

func (r *Runner) connectAndAuth(ctx context.Context, user, pass string) (*xmpp.Client, error) {
	t0 := time.Now()
	ctxD, cancel := context.WithTimeout(ctx, r.cfg.ConnectTimeout)
	defer cancel()
	c, err := xmpp.Dial(ctxD, r.cfg.Host, r.cfg.Port, r.cfg.Domain, r.cfg.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	tConn := time.Since(t0)
	t1 := time.Now()
	if err := c.Authenticate(user, pass, fmt.Sprintf("lt-%d", time.Now().UnixNano()), r.cfg.AuthTimeout); err != nil {
		_ = c.Close()
		return nil, err
	}
	r.Metrics.Connect.Add(tConn)
	r.Metrics.Auth.Add(time.Since(t1))
	return c, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
