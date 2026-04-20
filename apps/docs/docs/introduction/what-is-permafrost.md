---
sidebar_position: 1
slug: /introduction/what-is-permafrost
---

# What is Permafrost

Permafrost is an open-source DeFi trading framework. It is an agent runtime built around a stable Strategy SAPI: write a deterministic Go strategy, register it, and the framework handles exchange adapters, swap routing, position reconciliation, PnL accounting, risk gates, the killswitch, decision provenance, and LLM augmentation.

Strategies are first-class extensions, not patches. Each strategy lives as its own subdirectory under `strategies/`, calls `strategy.Register` in `init()`, and is enabled by adding one blank-import line to `cmd/permafrostd/strategies.go`. The framework knows nothing about any specific strategy -- `BuildStrategy` is a pure registry lookup.

The OSS framework ships with `noop` as a reference implementation. Real strategies -- basis trades, market makers, anything you can express as deterministic Go -- are yours to build, share, or keep private.

## Status

MVP. Single-operator, local-first. v2 will introduce hosted vaults with on-chain accounting; the v1 framework is designed not to preclude it.

## Who is this for

- **Quant operators** who want to run a self-custodied trading bot without trusting a centralized service or learning a Python framework's idioms.
- **Strategy developers** who want a real Go agent runtime to plug into instead of building one from scratch.
- **Teams** that want to ship multiple strategies on shared infrastructure (one daemon, one database, one set of risk rules).

## What you get out of the box

**Framework**

- A deterministic agent loop with paired-execution invariants (swap-before-order for delta-neutral trades).
- Exchange adapter for Hyperliquid (perp), with `OpenOrders` + idempotent place/cancel.
- Swap adapters for Solana (Jupiter) and EVM chains (1inch v6 -- Ethereum, Base, Avalanche, BSC).
- A self-custodied keystore. Private key bytes never leave the `internal/wallet` boundary.
- Position reconciliation, PnL accounting, NAV tracking.
- A real killswitch -- cancels every open order, flattens shorts via reduce-only market orders, and (opt-in) liquidates spot legs back to USDC via the configured swap router.
- Pre-trade risk + circuit breakers (drawdown, daily loss, funding flip, …).
- Optional LLM-veto path via an OpenAI-compatible inference client (OpenAI, OpenRouter, Groq, vLLM, Ollama, etc.).
- Decision provenance: every order links back to the exact prompt and model response.
- A CSV-driven backtester.

**Reference strategies**

- `noop` -- minimal smoke-test strategy.
- `dca_buy` -- deterministic dollar-cost-averaging into a configured spot asset.
- `market_maker_basic` -- paired bid/ask quoting on Hyperliquid with optional LLM veto.

**Operator tooling**

- `make demo` -- one command from `git clone` to a paper-mode agent posting decisions in your terminal.
- `permafrost init` -- interactive setup wizard (config + keystore passphrase + first-agent template).
- `permafrost doctor` -- preflight check for Go, Docker, DB, keystore, inference providers, RPCs, registered strategies.
- `permafrost strategy-new <name>` -- scaffolds a new strategy package and registers it in the build.
- A multi-arch Docker image (`linux/amd64` + `linux/arm64`) published to GHCR, with a `cli` compose service for one-off commands.
- A read-only **Trading Desk web UI** -- arctic-themed React + Vite dashboard with hand-authored pixel-art SVG sprites of the cast.

## Next steps

- [Run the demo in 60 seconds](/getting-started/make-demo)
- [The init wizard + doctor](/getting-started/init-and-doctor)
- [Architecture overview](/introduction/architecture)
- [Writing a strategy](/strategies/sapi)
- [The cast and the LLM-as-agent thesis](/brand/llm-as-agent)
