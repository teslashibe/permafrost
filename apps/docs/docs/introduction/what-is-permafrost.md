---
sidebar_position: 1
slug: /introduction/what-is-permafrost
---

# What is Permafrost

Permafrost is an open-source DeFi trading framework. It is an agent runtime built around a stable Strategy SAPI: write a deterministic Go strategy, register it, and the framework handles exchange adapters, swap routing, position reconciliation, PnL accounting, risk gates, the killswitch, decision provenance, and LLM augmentation.

Strategies are first-class extensions, not patches. Each strategy lives as its own subdirectory under `strategies/`, calls `strategy.Register` in `init()`, and is enabled by adding one blank-import line to `cmd/permafrostd/strategies.go`. The framework knows nothing about any specific strategy — `BuildStrategy` is a pure registry lookup.

The OSS framework ships with `noop` as a reference implementation. Real strategies — basis trades, market makers, anything you can express as deterministic Go — are yours to build, share, or keep private.

## Status

MVP. Single-operator, local-first. v2 will introduce hosted vaults with on-chain accounting; the v1 framework is designed not to preclude it.

## Who is this for

- **Quant operators** who want to run a self-custodied trading bot without trusting a centralized service or learning a Python framework's idioms.
- **Strategy developers** who want a real Go agent runtime to plug into instead of building one from scratch.
- **Teams** that want to ship multiple strategies on shared infrastructure (one daemon, one database, one set of risk rules).

## What you get out of the box

- A deterministic agent loop with paired-execution invariants (swap-before-order for delta-neutral trades).
- Exchange adapter for Hyperliquid (perp).
- Swap adapters for Solana (Jupiter) and EVM chains (1inch v6 — Ethereum, Base, Avalanche, BSC).
- A self-custodied keystore. Private key bytes never leave the `internal/wallet` boundary.
- Position reconciliation, PnL accounting, NAV tracking.
- A killswitch with circuit breakers (drawdown, daily loss, funding flip, RPC errors, …).
- Optional LLM-veto path via an OpenAI-compatible inference client (works with OpenAI, OpenRouter, Groq, vLLM, Ollama, etc.).
- Decision provenance: every order links back to the exact prompt and model response.
- A CSV-driven backtester.

## Next steps

- [Local install](/getting-started/local-install)
- [Architecture overview](/introduction/architecture)
- [Writing a strategy](/strategies/sapi)
