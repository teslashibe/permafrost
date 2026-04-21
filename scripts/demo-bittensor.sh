#!/usr/bin/env bash
# scripts/demo-bittensor.sh — one-command launcher for the Permafrost
# *Bittensor alpha trading* demo.
#
# Run via `make demo-bittensor`. Spins up:
#   - Postgres (Timescale)
#   - permafrostd (the daemon)
#   - subtensor-localnet (the Bittensor chain, in a Docker container)
#
# Then bootstraps a tradeable subnet on the local subtensor and starts
# three trading agents (alpha_dca, alpha_momentum, alpha_yield), each
# pointed at the freshly-created subnet so they execute REAL on-chain
# add_stake extrinsics against a live runtime.
#
# Strategy line-up:
#   • Tao  — alpha_dca       (DCA into the bootstrapped subnet)
#   • Mo   — alpha_momentum  (momentum rotation across subnets)
#   • Yumi — alpha_yield     (rebalance into highest-stability subnets)
#
# These three agents drive the new alpha-themed avatars in the Trading
# Desk UI when you point your browser at http://127.0.0.1:5173 (run
# `npm run dev` from apps/desk).
#
# Idempotent — re-running attaches to existing agents rather than
# duplicating them.
#
# Exit codes:
#   0  — demo running, decisions tailing
#   1  — preflight failed
#   2  — build / docker / migration failed
#   3  — config / agent setup failed

set -euo pipefail

if [[ -t 1 ]]; then
  GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RED=$'\033[31m'; BOLD=$'\033[1m'; OFF=$'\033[0m'
else
  GREEN= YELLOW= RED= BOLD= OFF=
fi
ok()   { echo "  ${GREEN}✓${OFF} $*"; }
warn() { echo "  ${YELLOW}⚠${OFF} $*"; }
fail() { echo "  ${RED}✗${OFF} $*" >&2; }
step() { echo; echo "${BOLD}❄  $*${OFF}"; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BIN_DIR="$ROOT_DIR/bin"
PATH="$BIN_DIR:$PATH"

DEMO_DIR="$ROOT_DIR/.permafrost-demo-bittensor"
DEMO_CONFIG="$DEMO_DIR/config.yaml"
DEMO_KEYSTORE="$DEMO_DIR/keystore.json"
DEMO_ENV="$DEMO_DIR/env"

COMPOSE_FILE="deploy/compose/docker-compose.yml"

# Tell docker compose where to mount the keystore into the daemon
# container. We do NOT export PERMAFROST_BITTENSOR__* env vars here
# because they'd also be picked up by the host CLI (which needs
# ws://localhost:9944, not ws://subtensor:9944). Instead, the daemon
# container picks up its bittensor config from the compose file's
# defaults (`${PERMAFROST_BITTENSOR__RPC_URL:-ws://subtensor:9944}`).
export PERMAFROST_KEYSTORE_DIR="$DEMO_DIR"

# ─── 1. preflight ─────────────────────────────────────────────────────────
step "Permafrost — Bittensor alpha-trading demo"

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
  if [[ ! -x "$BIN_DIR/$bin" ]]; then needs_build=1; break; fi
  if find ./cmd ./internal ./pkg ./strategies -name '*.go' -newer "$BIN_DIR/$bin" -print -quit 2>/dev/null | grep -q .; then
    needs_build=1; break
  fi
done
if [[ "$needs_build" -eq 1 ]]; then
  echo "  → building binaries…"
  make build > /dev/null
fi
ok "binaries built ($BIN_DIR/{permafrost,permafrostd})"

# ─── 3. compose stack: Postgres + permafrostd + subtensor ────────────────
step "Bringing up the stack…"
echo "  → Postgres + permafrostd…"
if ! make up > /dev/null 2>&1; then
  fail "make up failed. Try \`docker ps\` to see if another Postgres is on :5432."
  exit 2
fi
ok "Postgres + permafrostd up"

echo "  → subtensor (Bittensor chain — pulls ~200MB on first run)…"
if ! docker compose -f "$COMPOSE_FILE" --profile bittensor up -d subtensor > /dev/null 2>&1; then
  fail "subtensor failed to start"
  exit 2
fi

# Wait for subtensor RPC to be reachable (up to 60s).
echo -n "  → waiting for subtensor RPC"
for i in $(seq 1 60); do
  if curl -sf -X POST http://127.0.0.1:9944 \
       -H "Content-Type: application/json" \
       -d '{"jsonrpc":"2.0","id":1,"method":"system_chain","params":[]}' \
       2>/dev/null | grep -q "Bittensor"; then
    echo
    ok "subtensor up (Bittensor chain at ws://localhost:9944)"
    break
  fi
  echo -n "."
  sleep 1
  if [[ $i -eq 60 ]]; then
    echo
    fail "subtensor did not become reachable within 60s"
    exit 2
  fi
done

# Wait for daemon health.
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/v1/health > /dev/null 2>&1; then break; fi
  sleep 1
done

# ─── 4. init wizard (idempotent) — point at the local subtensor ─────────
if [[ ! -f "$DEMO_CONFIG" ]]; then
  echo "  → first run: generating config + keystore passphrase…"
  permafrost init \
    --non-interactive \
    --theme arctic \
    --config-out "$DEMO_CONFIG" \
    --keystore-out "$DEMO_KEYSTORE" \
    --env-out "$DEMO_ENV" \
    > /dev/null

  # Patch the config so:
  # 1. The keystore points at the demo-local file (not ~/.permafrost
  #    which may have a stale keystore from a previous demo run).
  # 2. The bittensor section is added with the local subtensor URL
  #    and live submission enabled.
  # The host CLI uses ws://localhost:9944; the daemon container uses
  # ws://subtensor:9944 (compose service DNS) — passed via env var
  # in docker-compose.yml.
  python3 - <<PYEOF
import re
p = "$DEMO_CONFIG"
with open(p) as f:
    cfg = f.read()
cfg = re.sub(r'(\s+keystore_path:)\s*""', r'\1 "$DEMO_KEYSTORE"', cfg)
if "bittensor:" not in cfg:
    cfg += """

bittensor:
  rpc_url: ws://localhost:9944
  network: local
  allow_submit: true
"""
with open(p, "w") as f:
    f.write(cfg)
PYEOF
fi
ok "config: $DEMO_CONFIG"

# ─── 5. source the env file ──────────────────────────────────────────────
# shellcheck disable=SC1090
set -a; source "$DEMO_ENV"; set +a
ok "keystore unlocked"

# ─── 6. doctor preflight ─────────────────────────────────────────────────
step "Preflight check"
if ! permafrost --config "$DEMO_CONFIG" doctor; then
  fail "doctor failed. Fix the errors above and retry."
  exit 3
fi

# ─── 7a. ensure a Bittensor wallet is in the keystore ────────────────────
# The demo agents will trade live against the local subtensor — they
# need a Bittensor signer in the keystore. We import //Alice (the well-
# known dev account, pre-funded with 1M TAO on the localnet image) so
# the demo trades real TAO to real alpha out of the box.
step "Importing Alice's Bittensor wallet (//Alice)…"
if ! permafrost --config "$DEMO_CONFIG" wallet show 2>/dev/null | grep -q '^bittensor'; then
  permafrost --config "$DEMO_CONFIG" wallet import \
    --chain bittensor \
    --from-uri "//Alice" > /dev/null
  ok "Alice's bittensor key imported"
else
  ok "bittensor key already in keystore"
fi

# ─── 7b. bootstrap a tradeable subnet on the local subtensor ─────────────
step "Bootstrapping a tradeable subnet on the local subtensor…"
NETUID_FILE="$DEMO_DIR/netuid"
if [[ ! -f "$NETUID_FILE" ]]; then
  bootstrap_out=$(permafrost --config "$DEMO_CONFIG" bittensor bootstrap --from-uri "//Alice" 2>&1) \
    || { fail "bootstrap failed: $bootstrap_out"; exit 3; }
  echo "$bootstrap_out"
  NETUID=$(echo "$bootstrap_out" | grep -oE 'subnets: \[[0-9]+\]' | grep -oE '[0-9]+')
  if [[ -z "$NETUID" ]]; then
    fail "could not parse netuid from bootstrap output"
    exit 3
  fi
  echo "$NETUID" > "$NETUID_FILE"
else
  NETUID=$(cat "$NETUID_FILE")
  ok "re-using existing tradeable subnet: SN$NETUID"
fi
ok "tradeable netuid: SN$NETUID"

# ─── 8. recruit Tao, Mo, and Yumi ────────────────────────────────────────
step "Recruiting alpha-trading expedition…"

recruit() {
  local name=$1 strategy=$2 cfg_json=$3 alloc=$4 tick=$5
  local existing
  existing=$(permafrost --config "$DEMO_CONFIG" agent list 2>/dev/null \
    | awk -v n="$name" '$0 ~ "name="n {print $1; exit}' || true)
  if [[ -n "$existing" ]]; then
    ok "$name already on the ice ($existing)"
    permafrost --config "$DEMO_CONFIG" agent start "$existing" > /dev/null 2>&1 || true
    echo "$existing"
    return
  fi
  local out
  out=$(permafrost --config "$DEMO_CONFIG" agent create \
    --name "$name" \
    --strategy "$strategy" \
    --perp "" \
    --spot bittensor \
    --mode live \
    --alloc "$alloc" \
    --tick-secs "$tick" \
    --universe "SN${NETUID}" \
    --config-json "$cfg_json" 2>&1) \
    || { fail "agent create $name: $out"; exit 3; }
  local id
  id=$(echo "$out" | grep -oE 'id=[^ ]+' | head -1 | cut -d= -f2)
  permafrost --config "$DEMO_CONFIG" agent start "$id" > /dev/null
  ok "$name recruited: $id  ($strategy)"
  echo "$id"
}

TAO_CFG=$(printf '{"subnets":[%s],"tao_per_buy":1.0,"interval_ticks":2,"slippage_bps":100}' "$NETUID")
MO_CFG=$(printf  '{"universe":[%s],"window_ticks":5,"top_k":1,"exit_threshold":-0.05,"tao_per_position":2.0,"slippage_bps":100}' "$NETUID")
YUMI_CFG=$(printf '{"universe":[%s],"top_k":1,"rebalance_ticks":5,"volatility_window":5,"tao_per_position":2.0,"slippage_bps":100}' "$NETUID")

TAO_ID=$(recruit "Tao"  "alpha_dca"      "$TAO_CFG"  "500"  "30")
MO_ID=$(recruit  "Mo"   "alpha_momentum" "$MO_CFG"   "750"  "30")
YUMI_ID=$(recruit "Yumi" "alpha_yield"   "$YUMI_CFG" "1000" "30")

# ─── 8b. restart daemon so its supervisor picks up the new agents ────────
# The daemon's supervisor only loads agents marked status=running at boot
# time. Agents created after boot need a daemon restart to start ticking.
# We also use --force-recreate so any environment changes (keystore mount,
# bittensor RPC, allow_submit) are picked up.
# (A future PR will add hot-reload via SIGHUP or an admin endpoint.)
step "Recreating daemon container so it picks up the keystore + new agents…"
PERMAFROST_BITTENSOR__ALLOW_SUBMIT=true \
PERMAFROST_KEYSTORE_PASSPHRASE="$PERMAFROST_KEYSTORE_PASSPHRASE" \
PERMAFROST_KEYSTORE_DIR="$DEMO_DIR" \
docker compose -f "$COMPOSE_FILE" up -d --force-recreate permafrostd > /dev/null 2>&1
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:8080/v1/health > /dev/null 2>&1; then break; fi
  sleep 1
done
ok "daemon back up — supervisor now driving Tao, Mo, Yumi against the local subtensor"

# ─── 9. tail decisions across all three ──────────────────────────────────
step "🐧🐧🐧  Three agents on the ice — tailing decisions"
echo "  Open the Trading Desk UI: cd apps/desk && npm run dev → http://127.0.0.1:5173"
echo "  (SIGINT to stop the demo; the daemon + subtensor keep running.)"
echo "  Tear down everything: \`make demo-bittensor-clean\`"
echo

trap 'echo; echo "  (demo stopped — daemon + subtensor still running; \`make demo-bittensor-clean\` to teardown)"; exit 0' INT TERM

LAST_PRINT=""
while true; do
  combined=""
  for id_pair in "Tao:$TAO_ID" "Mo:$MO_ID" "Yumi:$YUMI_ID"; do
    name=${id_pair%%:*}
    aid=${id_pair#*:}
    out=$(permafrost --config "$DEMO_CONFIG" agent decisions "$aid" --since 30s --limit 2 2>/dev/null || true)
    if [[ -n "$out" ]]; then
      combined+="[$name] $(echo "$out" | tail -2)"$'\n'
    fi
  done
  if [[ -n "$combined" && "$combined" != "$LAST_PRINT" ]]; then
    echo "$combined"
    LAST_PRINT="$combined"
  fi
  sleep 3
done
