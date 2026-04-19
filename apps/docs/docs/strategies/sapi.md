---
sidebar_position: 1
---

# The Strategy SAPI

A Permafrost strategy is a Go package that implements `pkg/strategy.Strategy` and registers itself with the framework's registry.

## The interface

```go
type Strategy interface {
    Name() string
    Warmup(ctx context.Context, in WarmupInput) error
    Decide(ctx context.Context, in DecisionInput) (Decision, error)
}
```

That's it. `Name` returns the snake_case identifier; `Warmup` runs once after construction; `Decide` runs every tick.

## Three rules

1. **`Decide` must be deterministic given `DecisionInput`.** Same input → same output, every time. Pull randomness, time, or external signals through `WarmupInput.Services` or the `Signals` map on `DecisionInput` so the framework can audit and replay.
2. **`Warmup` must be idempotent.** It runs once before the tick loop starts, but the framework reserves the right to call it again on hot reload. Don't open files or sockets that can't be re-opened.
3. **Swap-before-order ordering.** If your strategy emits both a `SwapIntent` and an `OrderIntent` in the same `Decision`, the runtime executes swaps first and only sends the orders after every swap confirms. This preserves the spot-first invariant for delta-neutral strategies. Rely on it; don't try to fight it.

## The `Constructor`

```go
type Constructor func(cfg map[string]any) (Strategy, error)
```

`cfg` is the per-agent JSONB blob from the database — whatever the operator passed to `agent create --config-json`. Parse it into your typed config inside the constructor, apply defaults, validate, and return the strategy.

**Don't** take framework dependencies as constructor arguments. Pull them from `WarmupInput.Services` later. See [Services](/strategies/services).

## Registration

Each strategy package registers itself in `init()`:

```go
package my_strategy

import "github.com/teslashibe/permafrost/pkg/strategy"

const Name = "my_strategy"

func init() { strategy.Register(Name, New) }

func New(cfg map[string]any) (strategy.Strategy, error) { /* ... */ }
```

Registration is name-keyed (snake_case, must match what gets stored in `agents.strategy`). Double registration panics — that's intentional, so a typo surfaces at process start.

## End-to-end: writing your first strategy

The fast path is the scaffolder — see [`strategy-new` scaffolding](/strategies/scaffolding):

```bash
permafrost strategy-new my_first_strategy
go build ./...
permafrost agent create --strategy my_first_strategy --perp hyperliquid --alloc 100
permafrost agent run <id>
```

That generates `strategies/my_first_strategy/`, registers it in both binaries' `strategies.go`, and prints next steps.

The manual equivalent is six commands:

```bash
mkdir -p strategies/my_first_strategy
```

```go title="strategies/my_first_strategy/strategy.go"
package my_first_strategy

import (
    "context"
    "github.com/teslashibe/permafrost/pkg/strategy"
)

const Name = "my_first_strategy"

type Strategy struct{}

func init() { strategy.Register(Name, New) }

func New(cfg map[string]any) (strategy.Strategy, error) { return &Strategy{}, nil }

func (Strategy) Name() string { return Name }

func (Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

func (Strategy) Decide(_ context.Context, _ strategy.DecisionInput) (strategy.Decision, error) {
    return strategy.Decision{Notes: "first tick"}, nil
}
```

Enable it in both binaries (one line each — the daemon for runtime, the CLI for `strategy backtest`):

```go title="cmd/permafrostd/strategies.go"
import _ "github.com/teslashibe/permafrost/strategies/my_first_strategy"
```

```go title="cmd/permafrost/strategies.go"
import _ "github.com/teslashibe/permafrost/strategies/my_first_strategy"
```

Build and run. `agent start` marks the agent runnable so `permafrost serve` (the daemon) picks it up; `agent run` is the foreground equivalent for paper-mode iteration in a single shell:

```bash
go build -o bin/permafrostd ./cmd/permafrostd
go build -o bin/permafrost  ./cmd/permafrost
permafrost agent create --strategy my_first_strategy --perp hyperliquid --alloc 100
permafrost agent start <id>     # production: daemon picks up
# OR
permafrost agent run   <id>     # foreground iteration; SIGINT to stop
```

See [private strategies](/strategies/private-strategies) for the gitignored variants when you want a strategy that doesn't ship in the public repo.

## Reference implementations

Three strategies ship in the OSS build as working examples. Together they exercise every code path in the runtime:

| Strategy | Demonstrates |
|---|---|
| [`noop`](https://github.com/teslashibe/permafrost/tree/main/strategies/noop) | Minimum surface (20 lines). Copy-paste starter. Smoke test for the whole stack. |
| [`dca_buy`](/strategies/reference-strategies#dca_buy) | SwapIntent-only path; periodic spot accumulation with cooldown. |
| [`market_maker_basic`](/strategies/reference-strategies#market_maker_basic) | OrderIntent + LLM-veto path; paired bid/ask quoting on Hyperliquid. |

Full details on the [reference strategies](/strategies/reference-strategies) page.

## Next steps

- [Decision contract](/strategies/decision-contract)
- [Services](/strategies/services)
- [Private strategies](/strategies/private-strategies)
- [Testing](/strategies/testing)
