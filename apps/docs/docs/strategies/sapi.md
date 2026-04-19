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

```bash
mkdir -p strategies/dca_buy
```

```go title="strategies/dca_buy/strategy.go"
package dca_buy

import (
    "context"
    "github.com/teslashibe/permafrost/pkg/strategy"
)

const Name = "dca_buy"

type Strategy struct{}

func init() { strategy.Register(Name, New) }

func New(cfg map[string]any) (strategy.Strategy, error) { return &Strategy{}, nil }

func (Strategy) Name() string { return Name }

func (Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

func (Strategy) Decide(_ context.Context, _ strategy.DecisionInput) (strategy.Decision, error) {
    return strategy.Decision{Notes: "dca tick"}, nil
}
```

Enable it in the local build:

```go title="cmd/permafrostd/strategies.go"
import _ "github.com/teslashibe/permafrost/strategies/dca_buy"
```

Build and run:

```bash
go build -o bin/permafrostd ./cmd/permafrostd
permafrost agent create --strategy dca_buy --perp hyperliquid --alloc 100
permafrost agent start <id>
```

That's the whole loop.

## Reference: `noop`

The shortest possible strategy lives at `strategies/noop/noop.go` — twenty lines that satisfy the full interface. Use it as a copy-paste starting point. The framework's loops execute identically with `noop` selected, so it's also a valid smoke test for the whole stack.

## Next steps

- [Decision contract](/strategies/decision-contract)
- [Services](/strategies/services)
- [Private strategies](/strategies/private-strategies)
- [Testing](/strategies/testing)
