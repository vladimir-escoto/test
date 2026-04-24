#!/usr/bin/env bash
# setup.sh - install Go (if missing), build the loadtest binary, raise OS limits.
# Works on macOS and Linux. Run from the repo root.

set -euo pipefail

cd "$(dirname "$0")/.."

log() { printf "\033[1;36m[setup]\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[warn]\033[0m  %s\n" "$*"; }

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "mac" ;;
    Linux)  echo "linux" ;;
    *)      echo "unknown" ;;
  esac
}

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    log "go found: $(go version)"
    return
  fi
  log "go not found, attempting install..."
  case "$(detect_os)" in
    mac)
      if command -v brew >/dev/null 2>&1; then
        brew install go
      else
        warn "Homebrew not installed. Install from https://brew.sh and re-run, or install Go manually from https://go.dev/dl/"
        exit 1
      fi
      ;;
    linux)
      if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get update && sudo apt-get install -y golang-go
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y golang
      else
        warn "No supported package manager. Install Go manually from https://go.dev/dl/"
        exit 1
      fi
      ;;
    *)
      warn "unsupported OS — install Go manually from https://go.dev/dl/"
      exit 1
      ;;
  esac
}

raise_limits_mac() {
  log "raising macOS limits (may prompt for sudo)"
  sudo sysctl -w kern.ipc.somaxconn=2048 >/dev/null 2>&1 || true
  sudo sysctl -w net.inet.ip.portrange.first=10000 >/dev/null 2>&1 || true
  sudo sysctl -w net.inet.ip.portrange.last=65535  >/dev/null 2>&1 || true
  sudo launchctl limit maxfiles 1048576 1048576 >/dev/null 2>&1 || true
  # User-level ulimit must be set in the calling shell; handled by run.sh.
}

raise_limits_linux() {
  log "raising Linux limits (may prompt for sudo)"
  sudo sysctl -w net.ipv4.ip_local_port_range="10000 65535" >/dev/null 2>&1 || true
  sudo sysctl -w net.core.somaxconn=4096 >/dev/null 2>&1 || true
  sudo sysctl -w net.ipv4.tcp_tw_reuse=1 >/dev/null 2>&1 || true
}

build() {
  log "building loadtest binary"
  go mod tidy
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o loadtest ./cmd/loadtest
  log "built: $(pwd)/loadtest"
}

main() {
  ensure_go
  case "$(detect_os)" in
    mac)   raise_limits_mac ;;
    linux) raise_limits_linux ;;
  esac
  build
  log "done. run ./scripts/run.sh to start."
}

main "$@"
