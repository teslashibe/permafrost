---
sidebar_position: 5
---

# Testing strategies

Strategies are pure Go packages — `go test ./strategies/<your_strategy>/...` works exactly as you'd expect. This page covers the framework-specific patterns that make strategy testing straightforward.

## Unit tests

Strategy logic should be testable without touching the network. Two helpers make that easy:

- **`pkg/inference/mock`** — a `Provider` implementation that returns scripted responses. Use it for any LLM-veto path so unit tests don't make real network calls.
- **A typed-config test constructor** — strategies that need to assert on edge cases of their own typed `Config` commonly ship a `NewFromTypedConfig(cfg, …)` helper alongside the registry-facing `New(map[string]any)`. The helper accepts the typed struct directly and bypasses the JSONB-style parser, so tests can stay focused on strategy logic rather than config plumbing.

Example:

```go
func TestMyStrategy_VetoesOnNegativeNews(t *testing.T) {
    inf := mock.NewProvider(map[string]string{
        "veto_decision": `{"veto": true, "reason": "depeg rumor"}`,
    })
    s := newTestStrategy(t, Config{UseLLMVeto: true})
    s.SetInference(inf)

    dec, err := s.Decide(context.Background(), strategy.DecisionInput{
        // ... fixture ...
    })
    require.NoError(t, err)
    require.Empty(t, dec.Orders, "vetoed candidate should not produce orders")
}
```

## Integration tests with the backtester

Strategies that consume `MarketSnapshot` and produce `OrderIntent`s can be exercised against the framework's CSV-driven backtester (`internal/backtest`). The backtester takes a real `Strategy` and a CSV of funding ticks, returns realized PnL.

Write these tests **inside your strategy's package**, not under `internal/backtest/`:

```go title="strategies/private/my_strategy/runner_test.go"
package my_strategy_test

import (
    "github.com/teslashibe/permafrost/internal/backtest"
    mystrat "github.com/teslashibe/permafrost/strategies/private/my_strategy"
)

func TestRunner_Profit(t *testing.T) {
    s, err := mystrat.NewFromTypedConfig(...)
    // ...
    runner := backtest.NewRunner(s, /* startingNAV */, /* tickInterval */, backtest.Costs{})
    res, err := runner.Run(context.Background(), ticks)
    // ... assertions ...
}
```

Why with the strategy and not with the backtester? On a fresh OSS clone, your private strategy is not present. If the backtester package referenced your strategy, the framework's own tests would fail to compile for downstream users. Keeping these tests with the strategy keeps the OSS framework's test suite hermetic.

## Determinism tests

The SAPI promises that `Decide` is deterministic given `DecisionInput`. A common trap is accidentally pulling a wall-clock time inside `Decide`. A simple deterrence:

```go
func TestStrategy_DecisionIsDeterministic(t *testing.T) {
    s := newTestStrategy(t, Config{})
    in := strategy.DecisionInput{ /* ... fixed fixture ... */ }

    a, _ := s.Decide(context.Background(), in)
    b, _ := s.Decide(context.Background(), in)
    require.Equal(t, a, b)
}
```

Run it twice in CI; if it ever drifts, you have a regression.

## Live-mode smoke tests

For end-to-end testing against real venues, use **paper mode**. The fastest iteration loop is `agent run` (foreground, single shell, no daemon needed):

```bash
permafrost agent create \
    --strategy my_strategy \
    --perp hyperliquid \
    --network testnet \
    --alloc 100
permafrost agent run <id>          # foreground; SIGINT to stop
# OR, for daemon-supervised runs:
permafrost agent start <id>        # marks status=running
permafrost serve                   # picks it up
```

Paper mode reads real market data and produces real `Decision`s but does not place real orders. Pair it with `--network testnet` for the perp venue when iterating on a new strategy.

Promoting to live requires `--confirm-live` and the appropriate signers in the keystore — see [risk and the killswitch](/concepts/risk-and-killswitch).

## Next steps

- [Reference: SAPI overview](/reference/sapi-overview)
- [Operations: deployment](/operations/deployment)
