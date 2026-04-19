---
sidebar_position: 3
---

# Killswitch tuning

The killswitch is the framework's last line of defense. It runs continuously and can halt every agent in milliseconds.

## What it does when it fires

In order:

1. **Cancel** every open order on every venue across every running agent.
2. **Optionally market-close** open perp shorts. Configurable per-agent.
3. **Optionally swap** open spot legs back to USDC. Configurable per-agent.
4. **Stop** all agent runtimes (the loop exits; persisted status is set to `halted`).
5. **Persist** a killswitch event with the trigger reason for post-mortem.

## Triggers

The killswitch can be invoked manually:

```bash
permafrost agent stop --all --reason "manual: market dislocation"
```

Or fired automatically by:

- **Repeated breaker trips on multiple agents.** If two or more agents trip a breaker within a configurable window, the framework escalates to a system-wide killswitch. Tunable per breaker type.
- **Daemon-level kill conditions.** RPC error storm (sustained > N% errors over N seconds), DB unreachable, inference rate-limit storm.

## Per-agent breaker configuration

Each agent's `risk` config controls the breakers that fire for that agent. Set via `agent create --config-json` or `agent set-config`:

```json
{
  "risk": {
    "max_drawdown":       0.10,    // 10% of NAV high-water mark
    "max_daily_loss":     "200",   // USDC
    "max_concurrent_positions": 5,
    "max_notional_per_leg":     "1000",
    "max_total_basis_exposure": "5000",
    "max_spot_slippage_bps":    50
  }
}
```

`max_drawdown=0` disables the drawdown breaker. `max_daily_loss=0` disables the daily-loss breaker.

## Daemon-wide killswitch settings

Configured in `config.yaml`:

```yaml
killswitch:
  # If N or more agents trip a breaker within window_secs, fire the system killswitch.
  multi_agent_breaker_threshold: 2
  multi_agent_breaker_window_secs: 60

  # When the killswitch fires:
  cancel_all_orders: true        # always
  market_close_perp_shorts: true # close shorts at market
  swap_spot_to_usdc: false       # leave spot in place by default

  # Daemon-level kill conditions
  rpc_error_rate_threshold: 0.5  # 50% error rate
  rpc_error_window_secs: 30
  inference_rate_limit_threshold: 10  # 10 rate-limits in window
  inference_rate_limit_window_secs: 60
```

Default values are intentionally conservative; tune up for aggressive trading or down for a hair-trigger.

## After a killswitch fires

The framework leaves agents in `halted` state. Restart is a manual two-step:

```bash
# Inspect the trigger reason
permafrost agent killswitch-events --limit 10

# Once you've understood and addressed the cause:
permafrost agent set-mode <id> --status running
permafrost agent start <id>
```

This is intentional friction. The killswitch fired because something looked wrong; restarting should require an operator decision.

## Practical tuning advice

- **First week of live trading:** set `max_drawdown` low (5%) and `max_daily_loss` to a small absolute number (e.g. 1% of allocation). You're catching sizing bugs, not real market moves.
- **After a few weeks:** loosen if you have evidence the strategy behaves as expected; tighten if you don't.
- **Multi-agent threshold:** keep it at 2. One agent tripping is an agent issue; two simultaneously is a market issue.
- **`swap_spot_to_usdc`:** leave it `false` for delta-neutral strategies — closing the spot leg into a thin market amplifies whatever caused the kill in the first place. Set it `true` only for directional strategies where holding the spot leg without the perp hedge is itself the risk.

## Where it lives

- `internal/agent/killswitch.go` — the killswitch implementation.
- `internal/risk/breakers.go` — the per-agent breakers.

## Next steps

- [Risk and the killswitch (concept)](/concepts/risk-and-killswitch)
- [Reconcile and PnL](/concepts/reconcile-and-pnl)
