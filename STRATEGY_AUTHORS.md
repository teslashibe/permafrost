# Strategy Authors Guide

This is everything you need to write a strategy for Permafrost.

A Permafrost strategy is a Go package that implements the `pkg/strategy.Strategy` interface and registers itself with the framework. The agent runtime takes care of market data, exchange adapters, swap routing, position reconciliation, PnL accounting, the killswitch, and persistence — your strategy just decides what to do.

## Where strategies live

Every strategy is its own subdirectory under `strategies/` at the repo root. Reference and community-shipped strategies live alongside private ones:

```
strategies/
├── noop/                      ← reference, committed to the OSS repo
├── <your_public_strategy>/    ← committed if you contribute it back
└── private/                   ← gitignored as a directory; your local-only strategies
    └── <your_private_strategy>/
```

Anything under `strategies/private/` is excluded from version control by `.gitignore`. Anything else is fair game to commit (and to PR upstream if you want to share).

## The contract

The full interface lives in `pkg/strategy/strategy.go`. In summary:

```go
type Strategy interface {
    Name() string
    Warmup(ctx context.Context, in WarmupInput) error
    Decide(ctx context.Context, in DecisionInput) (Decision, error)
}
```

Three rules to follow:

1. **`Decide` must be deterministic given `DecisionInput`.** Same input → same output, every time. Pull randomness, time, or external signals through `WarmupInput.Services` or `DecisionInput.Signals` so the framework can audit and replay.
2. **`Warmup` must be idempotent.** It runs once before the tick loop starts, but the framework reserves the right to call it again (e.g. after a hot config reload). Don't open files or sockets that can't be re-opened.
3. **Swap-before-order ordering.** If your strategy emits both a `SwapIntent` and an `OrderIntent` in the same `Decision`, the runtime executes swaps first and only sends the orders after every swap confirms. This preserves the spot-first invariant for delta-neutral strategies. Rely on it; don't try to fight it.

## The `Constructor` and `Services`

The registry only knows your strategy through one function:

```go
type Constructor func(cfg map[string]any) (Strategy, error)
```

`cfg` is the per-agent JSONB blob from the database (whatever the operator passed to `agent create --config-json`). Parse it into your typed config inside the constructor, apply defaults, validate, and return the strategy. **Don't** take framework dependencies as constructor arguments — pull them from `WarmupInput.Services` later:

```go
type Services struct {
    Logger    *slog.Logger      // always non-nil
    Inference inference.Provider // nil if no provider configured for this agent
}
```

If your strategy requires a service (e.g. `Inference` for an LLM-veto path), validate its presence in `Warmup` and return an error if it's missing. The framework will refuse to start the agent and the operator will see the error immediately, instead of getting a surprise on the first decision tick.

## Registration

Each strategy package registers itself in `init()`:

```go
package my_strategy

import "github.com/teslashibe/permafrost/pkg/strategy"

const Name = "my_strategy"

func init() { strategy.Register(Name, New) }

func New(cfg map[string]any) (strategy.Strategy, error) { /* ... */ }
```

Registration is name-keyed (`snake_case`, must match what gets stored in `agents.strategy`). Double registration panics — that's intentional, so a typo surfaces at process start.

## Enabling your strategy in a build

Go has no auto-discovery for packages, so a strategy is only included in the binary if something blank-imports it. Two files do this:

- **`cmd/permafrostd/strategies.go`** — committed to the repo. Add a line here for any strategy that ships in the OSS framework or that you're contributing back.
- **`cmd/permafrostd/strategies_local.go`** — gitignored. Add a line here for any private strategy you don't want pushed.

Example `strategies_local.go`:

```go
package main

import (
    _ "github.com/teslashibe/permafrost/strategies/private/funding_arb_basic"
    _ "github.com/teslashibe/permafrost/strategies/private/my_other_strategy"
)
```

To remove a strategy from a build, delete its line. To revert to an OSS-only build (no private strategies), delete the whole `strategies_local.go` file.

## End-to-end: writing your first strategy

```bash
# 1. Create the package directory
mkdir -p strategies/private/dca_buy

# 2. Write strategy.go
cat > strategies/private/dca_buy/strategy.go <<'EOF'
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
EOF

# 3. Enable it in the local build
cat >> cmd/permafrostd/strategies_local.go <<'EOF'
import _ "github.com/teslashibe/permafrost/strategies/private/dca_buy"
EOF

# 4. Build and run
go build -o bin/permafrostd ./cmd/permafrostd
./bin/permafrostd
```

The strategy is now registered. Create an agent with `--strategy dca_buy` and it'll start ticking.

## Reference: `noop`

The shortest possible strategy that satisfies the interface lives at `strategies/noop/noop.go`. Use it as a copy-paste template. It does literally nothing (returns an empty `Decision` every tick), so the framework's loops, killswitch, reconcile, and PnL paths all execute even with no real strategy logic — useful as both a reference and a smoke test.

## Reference: `pkg/strategy` and `pkg/types`

The two packages strategies depend on:

- **`pkg/strategy`** — the SAPI: `Strategy`, `Decision`, `DecisionInput`, `WarmupInput`, `Services`, `Register`, `Get`, `List`.
- **`pkg/types`** — trading-domain types: `OrderIntent`, `SwapIntent`, `MarketSnapshot`, `Position`, `BasisPosition`, `WalletBalance`, `Balance`, `RiskLimits`, `ChainID`, `Asset`, etc.

Read the doc comments in those packages for the authoritative spec. Anything outside `pkg/` (e.g. `internal/assets`, `internal/agent`) is framework internals — strategies in this single-module repo *can* import them, but you're stepping outside the stable API surface and accepting that internals may change without notice.

## Backups for private strategies

Anything under `strategies/private/` is gitignored. That means it isn't backed up by your normal `git push` flow, and a careless `git clean -fdx` will wipe it. If your private strategy represents real value, do at least one of:

- Maintain it in a private fork on a branch you `git push` (just keep it off `main`).
- Add `strategies/private/` to your Time Machine / Backblaze / equivalent.
- Tar it periodically into a separate location (`tar -czf ~/backup/strategies-$(date +%F).tar.gz strategies/private/`).

Pick something. Don't rely solely on the working directory.

## Testing

Strategies are pure Go packages — `go test ./strategies/<your_strategy>/...` works exactly as you'd expect. Use `pkg/inference/mock` for any LLM-veto tests so unit tests don't make real network calls.

For integration tests against the framework's backtester, write the test inside your strategy's package (see `strategies/private/funding_arb_basic/runner_test.go` for an example pattern — that test exercises the backtester driving fab end-to-end). Keeping these tests with the strategy means a fresh OSS clone (which doesn't have your strategy) still builds and tests cleanly.

## Contributing a strategy upstream

If you want to share a strategy with the community, just commit it under `strategies/<your_strategy>/` (not under `private/`), add the import line to `cmd/permafrostd/strategies.go`, and open a PR. There is no separate plugin registry, no review board, no manifest format — Go modules and `git` are the whole system.
