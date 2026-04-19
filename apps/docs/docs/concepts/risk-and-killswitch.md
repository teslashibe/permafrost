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

- **MaxDrawdownBreaker** — fraction of equity below NAV high-water mark.
- **DailyLossBreaker** — absolute USDC loss within a UTC day.
- **FundingFlipBreaker** — perp funding flips sign while a basis is open (the trade thesis just inverted).
- *Pluggable.* `risk.Breaker` is an interface; new breakers are added by implementing it and wiring into `BuildRiskEngine`.

## Killswitch

The killswitch is the global stop-everything. It cancels every open order across every agent, optionally market-closes perp shorts, and (configurably) liquidates spot legs back to USDC. Triggers:

- Operator command: `permafrost agent stop --all --reason "<reason>"`.
- Repeated breaker trips on multiple agents (configurable threshold).
- Daemon-level kill conditions (RPC error storm, DB unreachable, inference rate-limit storm).

The killswitch flow lives in `internal/agent/killswitch.go`. See [killswitch tuning](/operations/killswitch-tuning) for the configurable knobs.

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
