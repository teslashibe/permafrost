---
sidebar_position: 3
---

# Killswitch tuning

The killswitch is the framework's last line of defence. This page documents what it actually does today (per the v1 implementation in `internal/agent/killswitch.go`) — not aspirational behaviour.

## What the killswitch is

A single function on `Runtime`:

```go
type KillSwitchOptions struct {
    CloseShorts     bool   // flatten any open perp positions via reduce-only market orders
    LiquidateSpot   bool   // ALSO swap open spot legs back to USDC via SwapVenue
    SpotSlippageBps int    // explicit slippage cap for liquidation swaps (0 = use agent risk limit, fallback 100bps)
    Reason          string // recorded on the agent run
}

func (r *Runtime) Trip(ctx context.Context, opts KillSwitchOptions) error
```

When `Trip` fires, in order:

1. **Stop** the runtime's tick loop.
2. **Set `status='halted'`** on the agent (so a daemon restart does not auto-resume it).
3. **Cancel every open order** on the perp venue. Hyperliquid implementation reads `/info?type=openOrders` and cancels each one; per-order failures are logged and the loop continues.
4. If `CloseShorts`: **flatten every non-flat perp position** with reduce-only market orders. Shorts → buy back, longs → sell.
5. If `LiquidateSpot`: **swap every open spot leg back to USDC** via the chain's configured `SwapVenue` (Quote → Swap; doesn't `WaitConfirm` — killswitch is fire-and-forget).

Errors during steps 3-5 are logged but **do not stop the rest of the sequence**. The first error encountered is returned so the caller knows something failed; the rest is in the log.

## How it gets invoked

Two paths today:

```bash
# Operator-initiated, daemon-mediated:
permafrost agent stop --all --reason "<reason>"
```

The CLI flips every agent's status to `halted`. The daemon supervisor observes the status change and calls `Trip` on each affected runtime. If you don't have the daemon running, `agent stop` only does the persistent state change.

```bash
# Per-agent halt (no order cancellation; just persistent state change):
permafrost agent stop <id>
```

## Per-agent risk limits + breakers

Before the killswitch fires, per-agent risk limits and circuit breakers handle most bad situations. These live in `internal/risk/` and are configured via the agent's `risk:` config block:

```bash
permafrost agent create \
    --strategy <name> \
    --config-json '{
        "risk": {
            "max_concurrent_positions":     5,
            "max_notional_per_leg":         "1000",
            "max_total_basis_exposure":     "5000",
            "max_daily_loss":               "200",
            "max_spot_slippage_bps":        50,
            "max_drawdown":                 0.10
        }
    }'
```

The framework installs two breakers automatically:

- **MaxDrawdownBreaker** — tracks NAV drawdown vs the agent's high-water mark. `max_drawdown: 0` disables it; otherwise expressed as a fraction (e.g. `0.10` = 10%).
- **DailyLossBreaker** — tracks absolute USDC loss within a UTC day. `max_daily_loss: 0` disables it.

Additional breakers (basis blowout, funding flip, RPC health) are part of the framework's roadmap; what's in v1 today is the two above plus the pre-trade limits.

## Tuning `LiquidateSpot`

Spot liquidation is **opt-in** because it requires a `SwapVenue` configured for every chain you hold spot on. Default `KillSwitchOptions.CloseShorts: true, LiquidateSpot: false` is the safe choice for the first week of operation — flatten the perp risk, leave the spot inventory in place where you can sell it manually if needed.

When you flip `LiquidateSpot: true`:

- Every chain in `BasisPosition.Legs[].Asset.Chain` must have a `SwapVenue` registered in `Deps.SwapVenues`. Chains without one log a warning and the leg is left in place.
- Every chain must have a USDC mapping. The framework ships these for Solana, Ethereum, Base, Avalanche, BSC (`internal/assets.USDCMintFor`). Chains without a mapping warn and skip.
- `SpotSlippageBps` defaults to whatever the agent's `MaxSpotSlippageBps` risk limit is, with a 100bps safety ceiling. A killswitch is by definition urgent — typical slippage budgets here are wider than normal entry/exit. Tune deliberately.

## Practical tuning advice

Defaults are intentionally conservative; tune up only as you build confidence.

- **First week of live trading:** set `max_drawdown` low (5%) and `max_daily_loss` to a small absolute number (e.g. 1% of allocation). You're catching sizing bugs, not real market moves.
- **`CloseShorts: true`** is the safe default — leaving naked shorts running after a kill is rarely what you want.
- **`LiquidateSpot: true`** is appropriate when you want fully-flat exposure after a kill; leave it off when you'd rather sell spot manually after diagnosing.

## Known limits

- **No daemon-wide killswitch trigger.** Multi-agent breaker escalation, RPC error storms, and inference rate-limit storms are not auto-triggered by the framework. An operator hitting `permafrost agent stop --all` is the manual equivalent today.
- **Liquidation is fire-and-forget.** `Trip` does not `WaitConfirm` on liquidation swaps. Operator can inspect tx hashes from the decision log if they need to follow up — that trade-off is deliberate (a kraken doesn't wait politely).
- **Per-chain policy is global.** `LiquidateSpot` either applies to every chain or none. Per-chain selectivity (e.g. "liquidate Solana legs but not EVM") needs a tiny wrapper around `Trip` for now.

These are documented in `internal/agent/killswitch.go` itself; the source is the authoritative reference.

## After a killswitch fires

The framework leaves agents in `halted` state. Restart is a manual two-step:

```bash
# Inspect what halted (decisions log carries the agent's last activity)
permafrost agent decisions <id> --limit 20

# Once you've understood and addressed the cause:
permafrost agent set-mode <id> paper      # downgrade to paper while diagnosing
permafrost agent start <id>               # mark running; supervisor picks it up
```

This is intentional friction. The killswitch fired (or was tripped) for a reason; restarting should require an operator decision.

## Where it lives

- `internal/agent/killswitch.go` — implementation.
- `internal/agent/killswitch_test.go` — `cancelOpenOrders` happy path / no-orders / one-failure-continues; `flattenPositions` short / long / flat-skipped; defaults regression guard; `Runtime.Trip` end-to-end.
- `internal/exchange/exchange.go` — `Venue.OpenOrders` interface.
- `internal/exchange/hyperliquid/venue.go` — Hyperliquid `OpenOrders` implementation.
- `internal/risk/breakers.go` — per-agent breakers.
- `internal/risk/policy.go` — pre-trade risk policy + `Limits()` accessor.
- `internal/assets/usdc.go` — canonical USDC mint mapping per chain.

## Next steps

- [Risk and the killswitch (concept)](/concepts/risk-and-killswitch)
- [Reconcile and PnL](/concepts/reconcile-and-pnl)
