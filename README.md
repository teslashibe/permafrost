<div align="center">

# Permafrost

**An open-source DeFi market-making and hedge-fund protocol where AI agents deploy capital into a range of trading strategies. Capital and gains are locked into the permafrost.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Status](https://img.shields.io/badge/status-MVP-orange)]()
[![Built with TimescaleDB](https://img.shields.io/badge/database-TimescaleDB-FDB515)](https://www.timescale.com/)
[![Hyperliquid](https://img.shields.io/badge/perp-Hyperliquid-50D2C2)](https://hyperliquid.xyz/)
[![Solana](https://img.shields.io/badge/spot-Solana-9945FF?logo=solana)](https://solana.com/)

</div>

---

## Overview

Permafrost is a self-custodied, locally-runnable trading framework. It is an open agent runtime around the Strategy SAPI: write a deterministic Go strategy, register it, and the framework handles exchange adapters, swap routing, reconciliation, PnL accounting, risk gates, the killswitch, decision provenance, and LLM augmentation.

Strategies are first-class extensions, not patches: each one lives as its own subdirectory under `strategies/`, calls `strategy.Register` in `init()`, and is enabled by adding one blank-import line to `cmd/permafrostd/strategies.go`. See [`STRATEGY_AUTHORS.md`](STRATEGY_AUTHORS.md) for the full extension flow.

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

See [`SCOPE.md`](SCOPE.md) for the full architectural specification.

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
cp config.example.yaml config.yaml          # edit with your keys & RPC URLs
make up                                      # bring up Timescale + permafrostd
permafrost wallet import --chain solana --from <keypair.json>
permafrost vault init
permafrost agent create \
    --strategy noop \
    --perp hyperliquid \
    --alloc 5000
permafrost agent start <id>
```

The OSS build ships with `noop` registered; that's enough to confirm your install, database, and venue connections are working. To run a real strategy, see [`STRATEGY_AUTHORS.md`](STRATEGY_AUTHORS.md) — adding one is a folder + a registration call + one import line.

Default mode is `paper` — no real orders are placed until the agent is explicitly promoted to `live`.

---

## CLI

```
permafrost wallet     import | show | balances
permafrost vault      init | deposit | status | nav
permafrost agent      create | start | stop | logs | decisions | positions | pnl
permafrost strategy   list | backtest
permafrost inference  test
permafrost swap       quote
permafrost risk       show
permafrost db         migrate
permafrost serve
```

---

## Roadmap

| Milestone | Description | Status |
|---|---|---|
| M0 | Skeleton (config, logging, compose, migrations, health endpoint) | Planned |
| M1 | Core interfaces (`Strategy`, `Venue`, `SwapVenue`, `Provider`, `Signer`, `Risk`) | Planned |
| M2 | Hyperliquid adapter | Planned |
| M3 | Inference (OpenAI-compatible client + mock) | Planned |
| M4 | Wallet & keystore | Planned |
| M5 | Solana SwapVenue (Jupiter + Jito) | Planned |
| M6 | Asset registry | Planned |
| M7 | Vault & accounting | Planned |
| M8 | Agent runtime | Planned |
| M9 | Risk + circuit breakers + kill switch | Planned |
| M10 | Backtest harness | Planned |

After v1: additional chains (Base, Arbitrum), auto-bridging via CCTP, hosted deployments, on-chain vault contracts, additional strategies.

---

## Safety

A leveraged AI agent will absolutely try to nuke a vault if you let it. Permafrost ships with non-negotiable guardrails:

- **Spot-first execution** — DEX swap must confirm before the perp short is sent.
- **Idempotent intents** — every order and swap carries a deterministic client ID.
- **Decision provenance** — every order links back to the exact prompt + model response.
- **Paper mode by default** — no real orders until explicitly promoted to `live`.
- **Circuit breakers** — NAV drawdown, funding flip, basis blowout, RPC errors, margin buffer, inference error rate.
- **Kill switch** — `permafrost agent stop --all` cancels orders, closes shorts, and (configurably) liquidates spot legs.
- **Mainnet gating** — Hyperliquid mainnet behind an explicit config flag; default is testnet.

---

## Contributing

This is an open-source project and contributions are welcome. Please read [`SCOPE.md`](SCOPE.md) before opening a PR — it defines the framework boundaries we're working within for v1.

---

## License

[MIT](LICENSE)
