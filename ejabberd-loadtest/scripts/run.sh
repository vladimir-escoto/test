#!/usr/bin/env bash
# run.sh - start the loadtest against the default ejabberd server.
# Override any flag with environment variables, e.g.:
#   TARGET=5000 RAMP=500 ./scripts/run.sh
#   HOST=other.example.com PORT=5222 ./scripts/run.sh
#   HEADLESS=1 DURATION=5m ./scripts/run.sh

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -x ./loadtest ]]; then
  echo "binary not found, running setup..."
  ./scripts/setup.sh
fi

# raise ulimit for this shell session
ulimit -n 1048576 2>/dev/null || ulimit -n 262144 2>/dev/null || true

HOST="${HOST:-testqa.tripleenableverified.com}"
PORT="${PORT:-5222}"
DOMAIN="${DOMAIN:-testqa.tripleenableverified.com}"
REG_URL="${REG_URL:-https://${HOST}:5443/api/register}"
USER_PREFIX="${USER_PREFIX:-lt}"
PASSWORD="${PASSWORD:-loadtest}"
TARGET="${TARGET:-1000}"
RAMP="${RAMP:-100}"
MSG_INTERVAL_MS="${MSG_INTERVAL_MS:-5000}"
PAIR="${PAIR:-true}"
SKIP_REGISTER="${SKIP_REGISTER:-false}"
REG_INSECURE="${REG_INSECURE:-true}"
REG_API_USER="${REG_API_USER:-}"
REG_API_PASS="${REG_API_PASS:-}"
CSV="${CSV:-loadtest-report-$(date +%Y%m%d-%H%M%S).csv}"

EXTRA=()
if [[ "${HEADLESS:-0}" == "1" ]]; then EXTRA+=(-headless); fi
if [[ -n "${DURATION:-}" ]]; then EXTRA+=(-duration "$DURATION"); fi
if [[ "$SKIP_REGISTER" == "true" ]]; then EXTRA+=(-skip-register); fi
if [[ "$PAIR" != "true" ]]; then EXTRA+=(-pair=false); fi

exec ./loadtest \
  -host "$HOST" \
  -port "$PORT" \
  -domain "$DOMAIN" \
  -user-prefix "$USER_PREFIX" \
  -password "$PASSWORD" \
  -target "$TARGET" \
  -ramp "$RAMP" \
  -msg-interval-ms "$MSG_INTERVAL_MS" \
  -register-mode "http" \
  -register-url "$REG_URL" \
  -register-insecure="$REG_INSECURE" \
  -register-api-user "$REG_API_USER" \
  -register-api-pass "$REG_API_PASS" \
  -csv "$CSV" \
  ${EXTRA[@]+"${EXTRA[@]}"}
