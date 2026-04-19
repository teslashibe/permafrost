<div align="center">

# Permafrost

**An open-source DeFi market-making and hedge-fund protocol where AI agents deploy capital into a range of trading strategies. Capital and gains are locked into the permafrost.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Status](https://img.shields.io/badge/status-MVP-orange)]()
[![Built with TimescaleDB](https://img.shields.io/badge/database-TimescaleDB-FDB515)](https://www.timescale.com/)
[![Hyperliquid](https://img.shields.io/badge/perp-Hyperliquid-50D2C2)](https://hyperliquid.xyz/)
[![Solana](https://img.shields.io/badge/spot-Solana-9945FF?logo=solana)](https://solana.com/)

📚 **[Read the docs →](https://teslashibe.github.io/permafrost/)**

</div>

---

## Overview

Permafrost is a self-custodied, locally-runnable trading framework. It is an open agent runtime around the Strategy SAPI: write a deterministic Go strategy, register it, and the framework handles exchange adapters, swap routing, reconciliation, PnL accounting, risk gates, the killswitch, decision provenance, and LLM augmentation.

Strategies are first-class extensions, not patches: each one lives as its own subdirectory under `strategies/`, calls `strategy.Register` in `init()`, and is enabled by adding one blank-import line to `cmd/permafrostd/strategies.go`. See the [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi) for the full extension flow.

The framework ships with `noop` as the reference implementation. Real strategies — basis trades, market makers, anything you can express as Go — are yours to build, share, or keep private.

> **Status:** MVP, single-operator, local-first. v2 will introduce hosted vaults with on-chain accounting.

---

## Why Permafrost

- **Self-custodied spot leg.** The long side of every trade is held in your own wallet. No CEX can freeze, rehypothecate, or lose your tokens.
- **AI agents, deterministic execution.** Strategies are deterministic Go code. The LLM augments — vetoing risky entries based on news, tuning thresholds — but never invents orders.
- **Open framework, your strategies.** Drop a folder in `strategies/`, register, build. Keep your code public, private, or somewhere in between. The framework doesn't care.
- **Built for transparency.** Every decision the agent makes — including the exact prompt and model response — is persisted in TimescaleDB and joinable to the resulting orders.

---

## Architecture

```
                ┌─────────────────────────────────────────────┐
                │                  permafrostd                │
                │  ┌──────────┐   ┌────────────┐  ┌────────┐  │
   CLI ◄───────►│  │  Fiber   │   │  Scheduler │  │ Agent  │  │
                │  │   API    │   │            │  │ Runtime│  │
                │  └──────────┘   └────────────┘  └────┬───┘  │
                │                                       │      │
                │  ┌──────────────────────────────┐    │      │
                │  │  Strategy (your code)        │◄───┘      │
                │  └──────┬─────────┬─────────┬───┘           │
                │         │         │         │                │
                │         ▼         ▼         ▼                │
                │   ┌────────┐ ┌────────┐ ┌────────┐          │
                │   │  Risk  │ │Inference│ │Wallet  │          │
                │   └────────┘ └────────┘ └────────┘          │
                │         │         │         │                │
                └─────────┼─────────┼─────────┼────────────────┘
                          │         │         │
                          ▼         ▼         ▼
                  ┌───────────┐  ┌─────┐  ┌─────────────┐
                  │TimescaleDB│  │ LLM │  │Local        │
                  └───────────┘  └─────┘  │keystore     │
                                          └─────────────┘
                          │
                          ▼
              ┌────────────────────────┐
              │  Venues                 │
              │  • Hyperliquid (perp)   │
              │  • Jupiter (Solana spot)│
              └────────────────────────┘
```

For the full architectural specification, the agent runtime, the strategy SAPI, and operations guidance, see the [docs site](https://teslashibe.github.io/permafrost/).

---

## Tech Stack

| Layer | Choice |
|---|---|
| Language | Go 1.25+ |
| HTTP API | [Fiber](https://gofiber.io/) |
| Database | [TimescaleDB](https://www.timescale.com/) (Postgres + hypertables) |
| Migrations | [`goose`](https://github.com/pressly/goose) |
| Query layer | [`sqlc`](https://sqlc.dev/) over [`pgx`](https://github.com/jackc/pgx) |
| CLI | [`cobra`](https://github.com/spf13/cobra) |
| Config | [`viper`](https://github.com/spf13/viper) (yaml + env) |
| Logging | [`log/slog`](https://pkg.go.dev/log/slog) (stdlib) |
| Perp venue | [Hyperliquid](https://hyperliquid.xyz/) |
| Spot venues | [Solana](https://solana.com/) via [Jupiter](https://jup.ag/); EVM (Ethereum, Base, Avalanche, BSC) via [1inch](https://1inch.io/) v6 |
| MEV protection | [Jito](https://www.jito.wtf/) bundles (Solana) |
| Inference | OpenAI-compatible (works with OpenAI, OpenRouter, Groq, Together, vLLM, Ollama, etc.) |
| Deploy | Docker + docker-compose |

---

## Quick Start

> Requires Docker and Go 1.25+.

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost

# Build the CLI + daemon binaries.
make build
export PATH=$PWD/bin:$PATH

# Bring up Postgres + permafrostd in Docker.
make up

# The keystore is unlocked from this env var. Any reasonably strong string is fine
# for the noop quickstart (noop never signs anything). Save it somewhere safe.
export PERMAFROST_KEYSTORE_PASSPHRASE=$(uuidgen 2>/dev/null || openssl rand -hex 16)

# Create and start a paper-mode noop agent.
permafrost agent create --strategy noop --perp hyperliquid --alloc 1000
# → "created agent id=ag-... ..."
permafrost agent start ag-...
permafrost agent decisions ag-...
```

`agent start` marks the agent runnable so the daemon (started by `make up`) picks it up. For one-shot foreground iteration in a single shell without the daemon, use `permafrost agent run ag-...` instead — that runs the tick loop in the foreground and exits on SIGINT.

The OSS build ships with `noop` registered; that's enough to confirm your install, database, and venue connections are working. To run a real strategy, see the [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi) — adding one is a folder + a registration call + one import line per binary.

Default mode is `paper` — no real orders are placed until the agent is explicitly promoted to `live` (which requires `--confirm-live`).

### Going live

For real-money operation you'll also need:

```bash
# Import a Solana keypair (for the spot leg of any basis-style strategy)
permafrost wallet import --chain solana --from ~/.config/solana/id.json

# Import a Hyperliquid signer key (for placing real perp orders)
permafrost wallet import --chain hyperliquid --hex 0x...

# Initialize vault accounting (off-chain in v1)
permafrost vault init

# Promote the agent to live (asks for explicit confirmation)
permafrost agent set-mode ag-... live --confirm-live
```

**EVM RPCs**: pick fresh ones from [chainlist.org](https://chainlist.org/) and drop them into `config.yaml` under `evm.chains.<name>.rpc_url`. Public defaults work for testing but rate-limit aggressively in production.

---

## CLI

```
permafrost wallet     show | generate | import | path
permafrost vault      init | deposit | withdraw | lockup | status | record-nav | nav
permafrost agent      create | list | status | decisions | set-mode | set-network | start | stop | tick | run
permafrost strategy   list | backtest
permafrost inference  test | list
permafrost swap       quote
permafrost risk       show
permafrost pnl        summary | positions | history
permafrost reconcile
permafrost db         migrate
permafrost serve
```

See the [CLI reference](https://teslashibe.github.io/permafrost/reference/cli) for flags and semantics.

---

## Roadmap

What's shipped in `main` today:

| Milestone | Description | Status |
|---|---|---|
| M0 | Skeleton (config, logging, compose, migrations, health endpoint) | ✅ |
| M1 | Core interfaces (`Strategy`, `Venue`, `SwapVenue`, `Provider`, `Signer`, `Risk`) | ✅ |
| M2 | Hyperliquid adapter | ✅ |
| M3 | Inference (OpenAI-compatible client + mock) | ✅ |
| M4 | Wallet & keystore | ✅ |
| M5 | Solana SwapVenue (Jupiter + Jito) | ✅ |
| M6 | Asset registry | ✅ |
| M7 | Vault & accounting | ✅ |
| M8 | Agent runtime | ✅ |
| M9 | Risk + circuit breakers + kill switch | ✅ (kill switch order-cancel + spot-liquidation are partial — see [#38](https://github.com/teslashibe/permafrost/issues/38)) |
| M10 | Backtest harness | ✅ |

What's next: the [Trading Desk epic (#30)](https://github.com/teslashibe/permafrost/issues/30) — onboarding polish, web dashboard, hosted demo, brand narrative.

---

## Safety

A leveraged AI agent will absolutely try to nuke a vault if you let it. Permafrost ships with these guardrails:

- **Spot-first execution** — DEX swap must confirm before the perp short is sent.
- **Idempotent intents** — every order and swap carries a deterministic client ID.
- **Decision provenance** — every order links back to the exact prompt + model response.
- **Paper mode by default** — no real orders until explicitly promoted to `live` with `--confirm-live`.
- **Per-agent circuit breakers** — NAV drawdown and daily loss are wired and configurable via the agent's `risk` config block; additional breakers (funding flip, RPC errors) tracked on the [Trading Desk epic](https://github.com/teslashibe/permafrost/issues/30).
- **Kill switch** — `permafrost agent stop --all` halts every agent and (when the daemon is running) flattens open perp positions via reduce-only market orders. **Open-order cancellation and spot-leg liquidation are partial in v1** — tracked by [#38](https://github.com/teslashibe/permafrost/issues/38). Until that lands, cancel resting orders manually via the exchange UI if needed.
- **Mainnet gating** — Hyperliquid live mode behind explicit `--confirm-live` per agent.

For full killswitch behaviour, configurable knobs, and current limits, see the [killswitch tuning page](https://teslashibe.github.io/permafrost/operations/killswitch-tuning).

---

## Contributing

This is an open-source project and contributions are welcome. Please read the [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi) (for new strategies) and the [architecture docs](https://teslashibe.github.io/permafrost/introduction/architecture) before opening a PR.

---

## License

[MIT](LICENSE)
