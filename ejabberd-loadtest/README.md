# ejabberd-loadtest

Interactive console tool that saturates an ejabberd server from one Mac/Linux/Windows
machine. Registers users through the HTTP API, opens plain-text XMPP sessions,
exchanges messages between paired users, and live-detects whether the bottleneck is
the **client machine** or the **server**.

Default target: `testqa.tripleenableverified.com` (c2s `5222`, admin API `5443`).

---

## Quick start

### macOS / Linux

```bash
./scripts/setup.sh      # installs Go if missing, builds ./loadtest, tunes sysctls
./scripts/run.sh        # launches the TUI against the default server
```

### Windows (PowerShell, as Administrator for port-range tuning)

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\run.ps1
```

Both scripts honor environment overrides, e.g.:

```bash
TARGET=10000 RAMP=500 MSG_INTERVAL_MS=2000 ./scripts/run.sh
HEADLESS=1 DURATION=10m ./scripts/run.sh
```

---

## What it does

1. **Register** `N` users against `https://HOST:5443/api/register` (mod_http_api).
   If a user already exists the error is tolerated.
2. **Connect** each user over plain TCP on `5222`, SASL PLAIN, resource bind.
3. **Pair** users (even ↔ odd) and have them exchange `ping:<id>` messages at a
   configurable interval, measuring round-trip time.
4. **Ramp** connections at `RAMP` per second up to `TARGET`; live keys `+`, `*`, `-`
   adjust the target during the run.
5. **Diagnose** every 500 ms whether the system is saturated by the **local**
   machine (file descriptors, ephemeral ports, CPU) or the **server**
   (`ECONNREFUSED`, connect timeouts, stream resets, auth failures).
6. **Report** a final summary to stdout + a CSV file with percentiles for
   connect / auth / register / message-RTT.

---

## TUI controls

| key | action |
|-----|--------|
| `+` | add 100 users to the target |
| `*` | add 1000 users |
| `-` | reduce target by 100 |
| `s` | stop ramping (keep current connections) |
| `q` | quit + print report |

---

## Bottleneck verdicts

At the bottom of the TUI you see one of:

- `none (system still has headroom)` — keep pushing
- `LOCAL: file descriptors exhausted (EMFILE)` — raise `ulimit -n`
- `LOCAL: ephemeral ports exhausted (EADDRNOTAVAIL)` — widen port range,
  add server-side listeners on 5223/5224…
- `LOCAL: client CPU / goroutine saturation` — client-side
- `SERVER: refusing connections` — server's listen backlog full or crashed
- `SERVER: slow or unresponsive` — server overloaded, handshakes timing out
- `SERVER: closing/resetting streams mid-session` — ejabberd dropping sessions
  (check `max_fsm_queue`, mnesia pressure)
- `SERVER: auth rejections` — mod_register / SASL module overloaded

This is the answer to "did my Mac stop growing, or did the server fall over?"

---

## Relevant ejabberd config

The tool needs two things enabled on the server:

### 1) Plain-text c2s on 5222

```yaml
listen:
  - port: 5222
    module: ejabberd_c2s
    starttls: true            # optional; tool uses plain without STARTTLS
    starttls_required: false  # MUST be false for this tool
    access: c2s
    shaper: c2s_shaper
    max_stanza_size: 262144
```

### 2) mod_http_api for registration

```yaml
listen:
  - port: 5443
    module: ejabberd_http
    tls: true
    request_handlers:
      /api: mod_http_api

acl:
  loopback:
    ip:
      - 127.0.0.0/8
      - ::1/128

api_permissions:
  "loadtest register":
    who:
      - ip: 0.0.0.0/0     # tighten this in production
    what:
      - register
```

If your `/api/register` requires admin basic-auth, pass:

```bash
REG_API_USER=admin@yourdomain REG_API_PASS=... ./scripts/run.sh
```

### 3) Generous limits while you run the test

```yaml
modules:
  mod_register:
    ip_access: all
    access: register

# In the VM (Linux, 2 vCPU / 16 GB)
# /etc/security/limits.conf
ejabberd soft nofile 1048576
ejabberd hard nofile 1048576
```

---

## Expected capacity from a single Mac M4 Pro (32 GB)

With default sysctl, a single client machine can open **~25,000–50,000** sessions
to one server IP on one port. To go higher, expose additional c2s ports
(`5222`, `5223`, `5224`...) on the server and rerun the tool pointing at each —
the 2 vCPU / 16 GB VM will almost certainly saturate long before the Mac does.

See the final report's `verdict:` line for the definitive answer.

---

## CSV output

One file per run, e.g. `loadtest-report-20260424-154512.csv`:

- section 1 — latency histograms (connect / auth / register / msg_rtt)
- section 2 — counters (attempted, live_at_end, messages, per-error counts,
  final verdict)

Import into any spreadsheet / notebook for analysis.

---

## Project layout

```
cmd/loadtest/            — main entry point (flags + TUI/headless mode)
internal/xmpp/           — minimal XMPP client + HTTP registrar
internal/loadtest/       — runner, metrics, error classifier, bottleneck diagnosis, report
internal/tui/            — Bubble Tea UI
internal/sysinfo/        — runtime / rlimit snapshot
scripts/                 — setup + run helpers (bash + PowerShell)
```
