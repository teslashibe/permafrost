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

# 4. Start the agent
permafrost agent start ag-...

# 5. Watch the decisions land
permafrost agent decisions ag-... --follow
# Each tick logs: confidence=0.00 swaps=0 orders=0 cancels=0 notes="noop"
```

If you see decisions appearing every 30 seconds, the daemon is healthy: the scheduler is firing, the runtime is calling `Strategy.Decide`, results are being persisted to TimescaleDB, and the kill-switch is monitoring without tripping.

## Inspecting agent state

```bash
permafrost agent status ag-...      # high-level health
permafrost agent positions ag-...   # any reconciled positions (none for noop)
permafrost agent pnl ag-...         # PnL since start (zero for noop)
```

## Stopping cleanly

```bash
permafrost agent stop ag-...
```

`stop` is graceful: the runtime finishes the current tick and exits the loop. It does **not** change the persisted agent status — to permanently halt an agent across daemon restarts, set its status to `halted`:

```bash
permafrost agent set-mode ag-... --halt
```

## Next steps

You've confirmed the framework runs. Now write something that actually trades:

- [The Strategy SAPI](/strategies/sapi)
- [Decision contract](/strategies/decision-contract)
- [Private strategies](/strategies/private-strategies)
