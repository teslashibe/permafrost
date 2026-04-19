---
sidebar_position: 1
---

# SAPI overview

The two packages strategies depend on. Authoritative reference is the doc comments in the source; this page is the index.

## `pkg/strategy`

The Strategy contract.

| Symbol | Purpose |
|---|---|
| `Strategy` (interface) | What every strategy must implement. `Name() string`, `Warmup`, `Decide`. |
| `Constructor` | `func(cfg map[string]any) (Strategy, error)`. The factory each strategy registers. |
| `Register(name, ctor)` | Self-registration in `init()`. Panics on duplicate name. |
| `Get(name)` | Registry lookup. Returns the Constructor for `name`. |
| `List()` | Sorted list of registered names. |
| `WarmupInput` | Carries `AgentID`, `Universe`, `Config`, `Now`, `Services`. |
| `DecisionInput` | Per-tick state: `Now`, `Market`, `PerpPositions`, `SpotBalances`, `BasisPositions`, `Cash`, `Signals`, `Limits`. |
| `Decision` | Strategy output: `Orders`, `Swaps`, `Cancels`, `Notes`, `Confidence`. |
| `Services` | Framework deps for the strategy: `Logger`, `Inference`. Extension point for new framework features. |

Source: [`pkg/strategy/`](https://github.com/teslashibe/permafrost/tree/main/pkg/strategy).
GoDoc: [pkg.go.dev/github.com/teslashibe/permafrost/pkg/strategy](https://pkg.go.dev/github.com/teslashibe/permafrost/pkg/strategy).

## `pkg/types`

Trading-domain primitives the SAPI references. Stable public types.

| Symbol | Purpose |
|---|---|
| `Asset`, `ChainID`, `WalletBalance`, `Balance` | Money + wallets. |
| `OrderIntent`, `OrderType`, `Side`, `TIF`, `OrderID` | Perp orders. |
| `SwapIntent` | DEX swaps. Pairs with an OrderIntent via `BasisKey`. |
| `Position`, `BasisPosition`, `BasisLeg`, `BasisState` | Open positions. |
| `MarketSnapshot`, `SymbolSnap`, `FundingRate` | Per-tick market view. |
| `RiskLimits` | Per-agent risk bounds. |

Source: [`pkg/types/`](https://github.com/teslashibe/permafrost/tree/main/pkg/types).
GoDoc: [pkg.go.dev/github.com/teslashibe/permafrost/pkg/types](https://pkg.go.dev/github.com/teslashibe/permafrost/pkg/types).

## `pkg/inference`

The OpenAI-compatible LLM contract. Optional for strategies; required only if the strategy uses `Services.Inference`.

| Symbol | Purpose |
|---|---|
| `Provider` (interface) | `Name()`, `Complete(ctx, Request)`, `Embed(ctx, EmbedRequest)`. |
| `Request`, `Response` | Chat completion. |
| `Message`, `Role`, `ToolSpec`, `ToolCall`, `Schema` | Messages and structured-output config. |
| `EmbedRequest`, `EmbedResponse` | Embeddings. |
| `ErrUnsupportedFeature`, `ErrRateLimited` | Sentinel errors strategies should handle. |

Source: [`pkg/inference/`](https://github.com/teslashibe/permafrost/tree/main/pkg/inference).
GoDoc: [pkg.go.dev/github.com/teslashibe/permafrost/pkg/inference](https://pkg.go.dev/github.com/teslashibe/permafrost/pkg/inference).

## What's *not* in the public SAPI

Strategies in this single-module repo *can* import `internal/*` packages directly (Go's `internal` rule allows it within the same module), but doing so accepts that those internals may change without notice. The packages most commonly reached for:

- `internal/assets` — the curated asset registry. `assets.LoadEmbedded()`. Reasonable to import; the schema is stable.
- `internal/wallet` — the keystore + signer abstractions. Avoid; strategies should not be touching keys.
- `internal/agent` — the runtime itself. Avoid; you're a guest of this package, not its user.

## Stability

`pkg/strategy`, `pkg/types`, and `pkg/inference` are committed public APIs. Breaking changes follow semantic versioning — major-version bump or nothing. Anything outside `pkg/` is fair game to change between releases.

## Next steps

- [Writing a strategy](/strategies/sapi)
- [CLI reference](/reference/cli)
