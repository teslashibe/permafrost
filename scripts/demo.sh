#!/usr/bin/env bash
# scripts/demo.sh — one-command launcher for the Permafrost demo experience.
#
# Run via `make demo`. Idempotent — re-running attaches to the existing
# demo agent rather than duplicating it.
#
# What it does (in order):
#   1.  Preflight: confirm docker + go are present, ports are free.
#   2.  Build the CLI + daemon binaries into ./bin if missing or stale.
#   3.  Bring up the docker-compose stack (Postgres + permafrostd).
#   4.  Run `permafrost init --non-interactive` if no config exists.
#   5.  Source the generated env file (passphrase) for this shell.
#   6.  Run `permafrost doctor` — bail out early if any check fails hard.
#   7.  Recruit a paper-mode `noop` agent named "Pip" if none exists.
#   8.  Mark Pip runnable and tail decisions until SIGINT.
#
# Exit codes:
#   0  — demo running, decisions tailing
#   1  — preflight failed (clear message printed)
#   2  — build / docker / migration failed
#   3  — config / agent setup failed

set -euo pipefail

# ─── colour ────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RED=$'\033[31m'; BOLD=$'\033[1m'; OFF=$'\033[0m'
else
  GREEN= YELLOW= RED= BOLD= OFF=
fi
ok()   { echo "  ${GREEN}✓${OFF} $*"; }
warn() { echo "  ${YELLOW}⚠${OFF} $*"; }
fail() { echo "  ${RED}✗${OFF} $*" >&2; }
step() { echo "${BOLD}❄  $*${OFF}"; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BIN_DIR="$ROOT_DIR/bin"
PATH="$BIN_DIR:$PATH"

# Where init writes its outputs. Kept under the repo so `make demo-clean`
# can wipe them without touching the operator's ~/.permafrost.
DEMO_DIR="$ROOT_DIR/.permafrost-demo"
DEMO_CONFIG="$DEMO_DIR/config.yaml"
DEMO_KEYSTORE="$DEMO_DIR/keystore.json"
DEMO_ENV="$DEMO_DIR/env"

# ─── 1. preflight ─────────────────────────────────────────────────────────
step "Permafrost demo — launching expedition…"

if ! command -v docker >/dev/null; then
  fail "docker not found on PATH. Install Docker Desktop or equivalent and retry."
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  fail "docker is installed but the daemon isn't running. Start Docker and retry."
  exit 1
fi
if ! command -v go >/dev/null; then
  fail "go not found on PATH. Install Go 1.25+ and retry."
  exit 1
fi
ok "preflight: docker + go present"

# ─── 2. build binaries ────────────────────────────────────────────────────
needs_build=0
for bin in permafrost permafrostd; do
  if [[ ! -x "$BIN_DIR/$bin" ]]; then
    needs_build=1; break
  fi
  # Rebuild if any Go source is newer than the binary. Cheap heuristic.
  if find ./cmd ./internal ./pkg ./strategies -name '*.go' -newer "$BIN_DIR/$bin" -print -quit 2>/dev/null | grep -q .; then
    needs_build=1; break
  fi
done
if [[ "$needs_build" -eq 1 ]]; then
  echo "  → building binaries…"
  make build > /dev/null
fi
ok "binaries built ($BIN_DIR/{permafrost,permafrostd})"

# ─── 3. compose stack ─────────────────────────────────────────────────────
if ! docker compose -f deploy/compose/docker-compose.yml ps --status running --quiet permafrost-daemon 2>/dev/null | grep -q .; then
  echo "  → bringing up Postgres + permafrostd…"
  if ! make up > /dev/null 2>&1; then
    # Surface the error properly on failure.
    fail "make up failed. Common cause: another Postgres on :5432. Try \`docker ps\` to confirm."
    docker compose -f deploy/compose/docker-compose.yml up -d 2>&1 | tail -5 >&2
    exit 2
  fi
fi
ok "stack up (Postgres + permafrostd)"

# Wait up to 30s for the daemon to be healthy. The compose healthcheck
# already does this, but `compose up -d` returns before health is green.
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/v1/health > /dev/null 2>&1; then
    break
  fi
  sleep 1
done

# ─── 4. init wizard (idempotent) ─────────────────────────────────────────
if [[ ! -f "$DEMO_CONFIG" ]]; then
  echo "  → first run: generating config + keystore passphrase…"
  permafrost init \
    --non-interactive \
    --theme arctic \
    --config-out "$DEMO_CONFIG" \
    --keystore-out "$DEMO_KEYSTORE" \
    --env-out "$DEMO_ENV" \
    > /dev/null
fi
ok "config: $DEMO_CONFIG"

# ─── 5. source the env file ──────────────────────────────────────────────
# shellcheck disable=SC1090
set -a; source "$DEMO_ENV"; set +a
ok "keystore unlocked (passphrase from $DEMO_ENV)"

# ─── 6. doctor preflight ─────────────────────────────────────────────────
# Expect one warning (no inference configured) and several skips
# (no Solana / EVM RPCs configured for the demo).
if ! permafrost --config "$DEMO_CONFIG" doctor; then
  fail "doctor failed. Fix the errors above and retry."
  exit 3
fi

# ─── 7. recruit Pip ──────────────────────────────────────────────────────
# If a noop demo agent already exists, reuse it. We tag it via name so
# re-runs find it without depending on remembering the generated id.
PIP_NAME="Pip"
existing=$(permafrost --config "$DEMO_CONFIG" agent list 2>/dev/null | awk -v n="$PIP_NAME" '$0 ~ "name="n {print $1; exit}' || true)
if [[ -z "$existing" ]]; then
  echo "  → recruiting Pip (paper-mode noop agent)…"
  create_out=$(permafrost --config "$DEMO_CONFIG" agent create \
    --name "$PIP_NAME" \
    --strategy noop \
    --perp hyperliquid \
    --alloc 1000 \
    --tick-secs 5 2>&1) || { fail "agent create failed: $create_out"; exit 3; }
  # Output line shape: "created agent id=ag-... name=Pip strategy=noop ..."
  AGENT_ID=$(echo "$create_out" | grep -oE 'id=[^ ]+' | head -1 | cut -d= -f2)
else
  AGENT_ID="$existing"
  ok "found existing agent $AGENT_ID (re-using)"
fi

if [[ -z "$AGENT_ID" ]]; then
  fail "could not determine agent id from creation output"
  exit 3
fi
ok "agent ready: $AGENT_ID"

# Mark runnable so the supervisor picks it up.
permafrost --config "$DEMO_CONFIG" agent start "$AGENT_ID" > /dev/null
ok "Pip is on the ice"

# ─── 8. tail decisions ───────────────────────────────────────────────────
echo
step "🐧  The Floe — funding rates"
echo "  (SIGINT to stop the demo; the daemon keeps running. \`make demo-clean\` to teardown.)"
echo

# Loop until SIGINT, polling for new decisions. We don't have a long-poll
# / SSE endpoint yet (#41 will add WebSocket); a 2s poll is fine for demo.
LAST_SEEN=""
trap 'echo; echo "  (demo stopped — daemon still running; \`make demo-clean\` to teardown)"; exit 0' INT TERM
while true; do
  output=$(permafrost --config "$DEMO_CONFIG" agent decisions "$AGENT_ID" --since 1m --limit 5 2>/dev/null || true)
  if [[ -n "$output" && "$output" != "$LAST_SEEN" ]]; then
    echo "$output" | tail -5
    LAST_SEEN="$output"
  fi
  sleep 2
done
