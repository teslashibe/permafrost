---
sidebar_position: 4
---

# Running noop

`noop` is the smallest strategy that satisfies the `Strategy` interface. It returns an empty `Decision` every tick — no orders, no swaps. It exists for two reasons:

1. **Reference implementation.** Twenty lines of Go that demonstrate the SAPI surface.
2. **End-to-end smoke test.** All of the framework's loops (scheduler, runtime, reconcile, killswitch, PnL) execute even with `noop` selected, so a successful `noop` run proves your install, database, keystore, and venue connections are all working.

## Smoke test

```bash
# 1. Install + bring up Postgres + daemon
make up

# 2. Confirm the registry sees noop
permafrost strategy list
# → noop

# 3. Create a paper-mode agent
permafrost agent create \
    --strategy noop \
    --perp hyperliquid \
    --alloc 1000 \
    --tick-secs 30
# → created agent id=ag-... name= strategy=noop mode=paper network=mainnet alloc=1000

# 4. Mark the agent runnable; the daemon supervisor picks it up.
#    For one-shot iteration without the daemon, see `agent run` below.
permafrost agent start ag-...

# 5. Inspect the most recent decisions
permafrost agent decisions ag-...
# Each tick logged: confidence=0.00 swaps=0 orders=0 cancels=0 notes="noop"
```

If you see decisions appearing every 30 seconds, the daemon is healthy: the scheduler is firing, the runtime is calling `Strategy.Decide`, results are being persisted to TimescaleDB, and the kill-switch is monitoring without tripping.

For one-shot foreground iteration in a single shell (no daemon), use `permafrost agent run ag-...` instead — that runs the tick loop in the foreground and exits on SIGINT.

## Inspecting agent state

```bash
permafrost agent status ag-...      # high-level health
permafrost pnl positions ag-...     # any reconciled positions (none for noop)
permafrost pnl summary              # PnL across all agents
```

## Stopping

```bash
permafrost agent stop ag-...           # one agent
permafrost agent stop --all            # every agent
```

`agent stop` sets the agent's persisted status to `halted` so a subsequent `permafrost serve` will not auto-resume it. To restart later, use `agent start <id>` again.

## Next steps

You've confirmed the framework runs. Now write something that actually trades:

- [The Strategy SAPI](/strategies/sapi)
- [Decision contract](/strategies/decision-contract)
- [Private strategies](/strategies/private-strategies)
