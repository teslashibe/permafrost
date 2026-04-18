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

Permafrost is a self-custodied, locally-runnable trading protocol. It uses AI agents wrapped around deterministic strategy logic to capture yield from on-chain and off-chain markets while keeping spot custody fully self-hosted.

The first agent — `funding_arb_basic` — captures perpetual funding by holding a spot position on a Solana DEX and shorting the matching perpetual on Hyperliquid, fully delta-neutral.

> **Status:** MVP, single-operator, local-first. v2 will introduce hosted vaults with on-chain accounting.

---

## Why Permafrost

- **Self-custodied spot leg.** The long side of every trade is held in your own Solana wallet. No CEX can freeze, rehypothecate, or lose your tokens.
- **AI agents, deterministic execution.** Strategies are deterministic Go code. The LLM augments — vetoing risky entries based on news, tuning thresholds — but never invents orders.
- **Open source, local-first.** Clone, configure, and run. The same codebase will later power hosted vaults; today, it's yours.
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
                │  │  Strategy: funding_arb_basic │◄───┘      │
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
    --strategy funding_arb_basic \
    --perp hyperliquid --spot solana \
    --universe wif,bonk,popcat \
    --alloc 5000 \
    --funding-entry-bps 50 --funding-exit-bps 10
permafrost agent start <id>
```

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
| M10 | First strategy: `funding_arb_basic` | Planned |
| M11 | Backtest harness | Planned |

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
