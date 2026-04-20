---
sidebar_position: 2
---

# Why self-custodied

Permafrost is built on the conviction that the long side of every trade should live in a wallet you control.

## What "self-custodied" means here

- Spot positions are held on-chain in a wallet whose keys you own. There is no exchange custody, no rehypothecation, no withdrawal queue.
- Perp positions on Hyperliquid are signed by a key in your local keystore. The exchange holds margin, but you sign every order.
- The Permafrost daemon runs on your machine (or your server). It needs no central permission to start, stop, or extract trades.

## Why it matters for a trading bot

A leveraged trading bot is a fast way to lose money. The classic blow-up modes -- exchange counterparty risk, key compromise, runaway strategy logic -- get worse, not better, when an AI is in the loop.

Self-custody addresses the first mode (counterparty risk) directly. The other two are addressed by the framework's design:

- **Key compromise:** `internal/wallet` is the only package that touches private key bytes. Everything else uses a `Signer` abstraction. Keys live in an encrypted JSON keystore loaded with a passphrase from an environment variable.
- **Runaway logic:** strategies are deterministic Go code. The LLM augments -- it can veto entries based on news, tune thresholds -- but it never invents orders. Every decision is persisted with the exact prompt + model response so you can audit what the model saw.

On top of that, the killswitch and circuit breakers fire on drawdown, daily loss, funding flip, and a configurable set of conditions; see [risk and the killswitch](/concepts/risk-and-killswitch).

## Trade-offs

- You're responsible for backups. If you lose your keystore and your passphrase, the funds are gone -- no exchange support to call.
- You need to run the daemon somewhere. v1 is local-first; the same codebase will later power hosted deployments, but right now the operator is you.
- Spot leg execution depends on DEX liquidity and on-chain conditions (gas, RPC, MEV). The framework wires Jito bundles for Solana and per-chain RPC config for EVM, but you should understand the route for any asset you trade.

## Next steps

- [Architecture overview](/introduction/architecture)
- [Keystore + backups](/operations/keystore-and-backups)
