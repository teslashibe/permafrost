---
sidebar_position: 4
---

# Reconcile and PnL

The framework's view of "what positions does this agent hold?" must always match reality on the exchanges and on-chain. Reconciliation is the loop that enforces it.

## What reconcile does

After every tick (and on a longer slow-tick interval), the runtime invokes `internal/reconcile`:

1. **Fetch venue truth.** Pull current perp positions from Hyperliquid and current spot balances from the configured wallets.
2. **Compare to the framework's stored state.** Match against `BasisPosition` rows in TimescaleDB.
3. **Update.** State transitions (`opening` → `open`, `closing` → `closed`) happen here, not in the strategy. Strategies only emit *intents*.
4. **Detect drift.** If the framework thinks a basis is `open` but the exchange shows zero perp position, reconciliation flags drift and the killswitch may engage.

The strategy never sees stale data. By the time `Decide` is called for the next tick, `DecisionInput.BasisPositions` reflects the most recent reconciled view.

## PnL accounting

`internal/pnl` computes per-agent PnL from:

- **Realized funding** — accumulated funding payments across the holding period.
- **Realized basis P&L** — the profit/loss from opening at one basis and closing at another.
- **Unrealized basis P&L** — mark-to-market of the open spot/perp pair.
- **Fees** — perp taker/maker fees + DEX swap fees.
- **Gas costs** — included for EVM swaps; trivial for Solana.

NAV is exposed via the API and CLI:

```bash
permafrost pnl summary
permafrost pnl positions <agent-id>
permafrost pnl history   <agent-id>
permafrost vault nav
```

## Where it lives

- `internal/reconcile/` — the reconciliation loop.
- `internal/pnl/` — PnL accounting.
- `internal/agent/runtime.go` — schedules reconcile after each tick.
- TimescaleDB hypertables — the underlying storage for time-series PnL data.

## Next steps

- [Inference](/concepts/inference)
- [Operations: deployment](/operations/deployment)
