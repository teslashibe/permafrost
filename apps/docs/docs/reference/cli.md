---
sidebar_position: 2
---

# CLI reference

The `permafrost` CLI talks to a running `permafrostd` daemon over REST for most agent operations. A handful (`db migrate`, `strategy backtest`, `wallet`) work without the daemon.

Run `permafrost --help` or `permafrost <subcommand> --help` for the authoritative reference; this page is the index. Anything documented here is asserted by the CI build; if a command is in this page it exists in the binary.

## Top-level command tree

```
permafrost wallet      show | generate | import | path
permafrost vault       init | deposit | withdraw | lockup | status | record-nav | nav
permafrost agent       create | list | status | decisions | set-mode | set-network | start | stop | tick | run
permafrost strategy    list | backtest <name>
permafrost inference   test | list
permafrost swap        quote
permafrost risk        show
permafrost pnl         summary | positions | history
permafrost reconcile   [agent-id]
permafrost db          migrate
permafrost serve
```

## Wallet

```bash
permafrost wallet show
permafrost wallet generate --chain solana
permafrost wallet import   --chain solana --from <keypair-file>
permafrost wallet path
```

`--chain` accepts `solana` or `hyperliquid`. Hyperliquid signers are commonly imported via a hex private key; consult `wallet import --help` for the exact flag your build supports. Keystore unlock uses the env var named in `wallet.passphrase_env` (defaults to `PERMAFROST_KEYSTORE_PASSPHRASE`). See [keystore + backups](/operations/keystore-and-backups).

## Vault

```bash
permafrost vault init                                  # initialize the vault row
permafrost vault deposit  --amount 5000                # record a deposit (off-chain accounting)
permafrost vault withdraw --amount 1000
permafrost vault lockup
permafrost vault status
permafrost vault record-nav
permafrost vault nav
```

## Agent

Agents are the unit of deployment: a database row that wraps a strategy with an inference provider, a perp venue, optional spot venues, risk limits, a tick schedule, and a decision log.

```bash
permafrost agent create \
    --strategy <name> \
    --perp hyperliquid \
    [--spot solana] \
    [--inference openrouter:claude-sonnet-4.5] \
    [--mode paper|live] \
    [--network mainnet|testnet] \
    [--universe wif,bonk,popcat] \
    [--alloc 5000] \
    [--tick-secs 60] \
    [--config-json '{"key":"value"}']

permafrost agent list
permafrost agent status   <id>
permafrost agent decisions <id>
permafrost agent set-mode    <id> <paper|live>
permafrost agent set-network <id> <mainnet|testnet>
```

### Starting and stopping

There are two ways to run an agent. Pick the one that matches your deployment:

```bash
# Production: mark as running so the daemon supervisor picks it up.
# Persistent across daemon restarts.
permafrost agent start <id>
permafrost serve                    # in another shell, or via systemd / docker

# Foreground (single-shell) iteration. Tick loop runs in this process
# and exits on SIGINT. Useful for paper-mode iteration; doesn't require
# the daemon.
permafrost agent run <id> [--ticks N] [--dry-run] [--network testnet]
```

```bash
permafrost agent stop <id>            # halt one agent (sets status=halted)
permafrost agent stop --all           # halt every agent
```

`agent stop` performs the persistent state change. The full kill switch (cancel orders + close shorts) is wired inside the running daemon; see [risk and the killswitch](/concepts/risk-and-killswitch).

### One-shot integration tick

```bash
permafrost agent tick <id> [--funding WIF=80,BONK=15]
```

Runs a single decision tick against synthetic funding rates and prints the resulting `Decision`. Paper-mode only; safe to run without venue connectivity.

## Strategy

```bash
permafrost strategy list
permafrost strategy backtest <name> --csv funding.csv [--config-json '{...}']
```

`backtest` looks the strategy name up in the registry — the same registry the daemon uses — so private strategies registered via `cmd/permafrost/strategies_local.go` are backtest-able too. See [private strategies](/strategies/private-strategies).

CSV format: header `time,symbol,rate,interval_seconds`. `time` accepts RFC3339 or epoch milliseconds.

## Inference

```bash
permafrost inference list                                                # configured providers
permafrost inference test --provider openrouter --model anthropic/claude-sonnet-4.5
```

## Swap

```bash
permafrost swap quote --chain solana --in USDC --out WIF --amount 100
```

Quotes only — does not broadcast. Useful for sanity-checking what an `OrderIntent` paired with a `SwapIntent` would experience.

## Risk

```bash
permafrost risk show
```

## PnL (top-level command)

```bash
permafrost pnl summary
permafrost pnl positions <agent-id>
permafrost pnl history   <agent-id>
```

## Reconcile

```bash
permafrost reconcile [agent-id]
```

Triggers a one-shot reconciliation pass for one agent (or all agents) without restarting the daemon. See [reconcile and PnL](/concepts/reconcile-and-pnl).

## DB

```bash
permafrost db migrate up
permafrost db migrate down
permafrost db migrate status
```

`make up` runs migrations automatically; this is the manual escape hatch.

## Serve

```bash
permafrost serve [--hyperliquid-network testnet]
```

Equivalent to running the dedicated `permafrostd` binary. Loads every agent with `status='running'` and supervises their tick loops until the process receives SIGINT/SIGTERM. The optional `--hyperliquid-network` flag is an emergency override that forces every supervised agent onto the chosen network regardless of its stored value.

## Environment variables

- `PERMAFROST_CONFIG` — config file path (default `./config.yaml`).
- `PERMAFROST_HYPERLIQUID_NETWORK` — daemon-wide override of every agent's stored Hyperliquid network. Useful for "panic switch all agents to testnet" scenarios.
- `PERMAFROST_KEYSTORE_PASSPHRASE` — passphrase to decrypt the keystore. Configurable via `wallet.passphrase_env` in `config.yaml` if you want a different env var name.
- Per-provider inference API keys via the env vars referenced by `inference.providers.<name>.api_key_env` (e.g. `OPENROUTER_API_KEY`).
- `ONEINCH_API_KEY` — for EVM swaps via 1inch.
