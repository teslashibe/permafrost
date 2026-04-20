---
sidebar_position: 3
---

# Risk and the killswitch

A leveraged AI agent will absolutely try to nuke a vault if you let it. Permafrost layers protections at multiple levels.

## Pre-trade risk

Before any `OrderIntent` is sent, the per-agent `risk.Engine` evaluates a `RiskLimits` set. If any limit is exceeded, the order is rejected and the runtime logs a structured warning.

Per-agent limits live on the agent record (set via `agent create --config-json '{"risk":{...}}'`):

```json
{
  "risk": {
    "max_concurrent_positions":     5,
    "max_notional_per_leg":         "1000",
    "max_total_basis_exposure":     "5000",
    "max_daily_loss":               "200",
    "max_spot_slippage_bps":        50,
    "max_drawdown":                 0.10
  }
}
```

## Circuit breakers

Beyond pre-trade limits, the framework runs a small set of circuit breakers continuously. Each breaker, when tripped, halts the agent and (depending on configuration) escalates to the killswitch:

- **MaxDrawdownBreaker** -- fraction of equity below NAV high-water mark.
- **DailyLossBreaker** -- absolute USDC loss within a UTC day.
- **FundingFlipBreaker** -- perp funding flips sign while a basis is open (the trade thesis just inverted).
- *Pluggable.* `risk.Breaker` is an interface; new breakers are added by implementing it and wiring into `BuildRiskEngine`.

## Killswitch

The killswitch is the per-agent stop-and-unwind. When `Runtime.Trip` fires it:

1. Stops the runtime's tick loop and sets `status='halted'` on the agent.
2. Calls `v.OpenOrders(ctx)` on the perp venue and cancels every one. Per-order failures are logged but don't abort the rest.
3. (If `CloseShorts: true`) Reads `v.Positions(ctx)` and submits a reduce-only market order for every non-flat position. Shorts → buy, longs → sell.
4. (If `LiquidateSpot: true`) Walks every open `BasisPosition`'s spot legs and submits a Quote → Swap pair against the configured `SwapVenue` for that chain, swapping back to USDC. Slippage cap precedence: explicit `opts.SpotSlippageBps` → agent's `MaxSpotSlippageBps` risk limit → 100bps ceiling.

Triggers today (v1):

- Operator command: `permafrost agent stop --all --reason "<reason>"` flips agent status; the daemon supervisor invokes `Trip` when it observes the change.
- Per-agent breakers (`MaxDrawdownBreaker`, `DailyLossBreaker`) trigger `Trip` when limits are hit.

Not yet auto-wired (planned follow-ups):

- Daemon-wide killswitch trigger (RPC error storms, multi-agent breaker cascades). Manual `agent stop --all` is the v1 path.
- Per-chain liquidation policy (today `LiquidateSpot` is global).

The full implementation lives in `internal/agent/killswitch.go`; tests in `internal/agent/killswitch_test.go` exercise the cancel path, the flatten path, the failure-continues invariant, and the conservative-defaults regression guard. See [killswitch tuning](/operations/killswitch-tuning) for the configurable knobs.

## Mainnet gating

Hyperliquid mainnet is behind an explicit per-agent `--network mainnet` flag and a daemon-wide `--confirm-live` requirement before any agent enters live mode. The default is paper mode (real market data, no real orders).

## Layers in summary

| Layer | What it does | Where |
|---|---|---|
| Strategy SAPI | Forces deterministic decisions, paired-execution invariants | `pkg/strategy` |
| Pre-trade risk | Rejects individual `OrderIntent`s that exceed limits | `internal/risk` |
| Circuit breakers | Halts an agent on continuous risk metrics | `internal/risk/breakers.go` |
| Killswitch | Stops every agent and optionally unwinds | `internal/agent/killswitch.go` |
| Mainnet gating | Default paper mode; live requires explicit opt-in | `internal/cli/agent.go` |

## Next steps

- [Killswitch tuning](/operations/killswitch-tuning)
- [Reconcile and PnL](/concepts/reconcile-and-pnl)
