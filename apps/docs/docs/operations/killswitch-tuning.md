---
sidebar_position: 3
---

# Killswitch tuning

The killswitch is the framework's last line of defence. This page documents what it actually does today (per the v1 implementation in `internal/agent/killswitch.go`) — not aspirational behaviour.

## What the killswitch is

A single function on `Runtime`:

```go
type KillSwitchOptions struct {
    CloseShorts   bool   // flatten any open perp positions via reduce-only market orders
    LiquidateSpot bool   // ALSO swap open spot legs back to USDC (not implemented in v1)
    Reason        string // recorded on the agent run
}

func (r *Runtime) Trip(ctx context.Context, opts KillSwitchOptions) error
```

When `Trip` fires, in order:

1. **Stop** the runtime's tick loop.
2. **Set `status='halted'`** on the agent (so a daemon restart does not auto-resume it).
3. **Cancel open orders** on the perp venue (best-effort — see "Known limits" below).
4. If `CloseShorts`, **flatten open perp positions** with reduce-only market orders.
5. If `LiquidateSpot`, log a warning that spot liquidation is not implemented in v1.

Errors during steps 3 and 4 are logged but do not stop the rest of the sequence — a single venue failure should not prevent flattening on the others.

## How it gets invoked

Two paths:

```bash
# Operator-initiated, daemon-mediated (cancels orders + closes shorts):
permafrost agent stop --all --reason "<reason>"
```

The `agent stop --all` CLI sets every agent's status to `halted`. The full kill switch (Trip) is invoked by the running daemon's supervisor when it observes the status change. If you don't have the daemon running, `agent stop` only does the persistent state change.

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

The framework installs two breakers automatically and reads the config above:

- **MaxDrawdownBreaker** — tracks NAV drawdown vs the agent's high-water mark. `max_drawdown: 0` disables it; otherwise expressed as a fraction (e.g. `0.10` = 10%).
- **DailyLossBreaker** — tracks absolute USDC loss within a UTC day. `max_daily_loss: 0` disables it.

Additional breakers (basis blowout, funding flip, RPC health) are part of the framework's roadmap; what's in v1 today is the two above plus the pre-trade limits.

## Practical tuning advice

Defaults are intentionally conservative; tune up only as you build confidence.

- **First week of live trading:** set `max_drawdown` low (5%) and `max_daily_loss` to a small absolute number (e.g. 1% of allocation). You're catching sizing bugs, not real market moves.
- **`CloseShorts: true`** is the safe default — leaving naked shorts running after a kill is rarely what you want.
- **`LiquidateSpot: true`** is **a no-op in v1**. If/when you need spot liquidation, you'll wire your own path (or watch this space — implementing the asset-registry-aware liquidation routine is a planned follow-up).

## Known limits

These are explicit gaps the v1 killswitch acknowledges:

- **`cancelOpenOrders` is currently a no-op.** The `exchange.Venue` interface does not yet expose an `OpenOrders` method, so the killswitch logs a warning and asks the operator to cancel manually via the exchange UI. When `OpenOrders` lands as a small follow-up, this becomes a loop over `v.OpenOrders(); v.Cancel(id)` and the warning goes away.
- **Spot liquidation is not implemented.** `LiquidateSpot: true` logs a warning. Auto-liquidating spot legs requires an asset-registry-aware caller threaded into the supervisor; deferred.
- **No daemon-wide killswitch trigger.** Multi-agent breaker escalation, RPC error storms, and inference rate-limit storms are not auto-triggered by the framework. An operator hitting `permafrost agent stop --all` is the manual equivalent today.

These limits are documented in `internal/agent/killswitch.go` itself; the source is the authoritative reference for what behaviour is wired vs aspirational.

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

- `internal/agent/killswitch.go` — the killswitch implementation.
- `internal/risk/breakers.go` — the per-agent breakers.
- `internal/risk/policy.go` — the pre-trade risk policy.

## Next steps

- [Risk and the killswitch (concept)](/concepts/risk-and-killswitch)
- [Reconcile and PnL](/concepts/reconcile-and-pnl)
