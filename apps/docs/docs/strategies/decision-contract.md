---
sidebar_position: 2
---

# Decision contract

Every tick, the runtime calls `Strategy.Decide(ctx, in)`. This page is the spec for what's in `in` and what you may return in the `Decision`.

## `DecisionInput`

```go
type DecisionInput struct {
    AgentID         string
    Now             time.Time
    Market          types.MarketSnapshot
    PerpPositions   []types.Position
    SpotBalances    []types.WalletBalance
    BasisPositions  []types.BasisPosition
    Cash            map[string]types.Balance // keyed by location: "hyperliquid", "solana"
    Signals         map[string]any
    Limits          types.RiskLimits
}
```

Framework guarantees:

- **`Cash`, `PerpPositions`, `SpotBalances`, `BasisPositions` reflect on-chain / venue state at `Now`.** Reconcile has run before `Decide` was called.
- **`Market` is consistent across symbols.** All symbol snapshots come from the same wall-clock fetch round.
- **`Signals` is opaque to the framework.** Strategies populate it themselves between ticks (e.g. with cached LLM outputs or computed indicators).
- **`Limits`** carries the per-agent `RiskLimits` set by the operator.

## `Decision`

```go
type Decision struct {
    Orders     []types.OrderIntent
    Swaps      []types.SwapIntent
    Cancels    []types.OrderID
    Notes      string  // human/LLM-readable rationale
    Confidence float64 // 0..1; advisory
}
```

What the runtime does with each field:

- **`Cancels`** are sent to the perp venue first.
- **`Swaps`** execute and are awaited until confirmation. `BasisKey` is the join key -- a swap and an order with the same `BasisKey` are treated as one paired action.
- **`Orders`** are sent only after every swap with a matching `BasisKey` confirms. If a swap fails, the matching order is dropped.
- **`Notes`** is persisted with the decision row. LLM rationales, debug strings, anything that helps you read your own decisions later.
- **`Confidence`** is advisory -- surfaced in the API and CLI for monitoring but does not influence execution.

## `OrderIntent`

```go
type OrderIntent struct {
    Venue       string          // "hyperliquid", ...
    Symbol      string          // e.g. "WIF-PERP"
    Side        Side            // SideBuy / SideSell
    Type        OrderType       // OrderTypeLimit / OrderTypeMarket
    Price       decimal.Decimal // for Limit
    Size        decimal.Decimal // base-asset units (NOT USDC notional)
    TIF         TIF             // GTC, IOC, FOK
    ReduceOnly  bool
    BasisKey    string          // pairs with a SwapIntent for delta-neutral execution
    Tag         string          // free-form, surfaces in venue order metadata
}
```

`Size` is in base-asset units, not USDC notional. Strategies that think in USDC must divide by mark price themselves.

## `SwapIntent`

```go
type SwapIntent struct {
    Chain       ChainID         // settlement chain
    InToken     Asset           // typically USDC
    OutToken    Asset           // the asset being acquired
    InAmount    decimal.Decimal // input-token units
    SlippageBps int             // strategy's tolerance
    BasisKey    string          // pairs with an OrderIntent
    Tag         string
}
```

`InAmount` is in input-token units (e.g. USDC has 6 decimals, so `InAmount` of `100` = $100). The framework's swap router translates this to the underlying venue's expected input format.

## Determinism

Same `DecisionInput` → same `Decision`. Period.

What "same" means:

- Same agent ID, same `Now`, same `Market` snapshot, same positions, same cash, same `Signals`, same `Limits` → byte-for-byte identical `Decision`.
- Random number generators, current wall-clock time, network calls outside `Services` -- all forbidden inside `Decide`.
- Caches that depend on previous tick output are fine but they *must* be derivable from prior `DecisionInput`s alone.

This is what makes the backtester and the decision-replay machinery work. Break determinism and you break audit.

## Next steps

- [Services](/strategies/services)
- [Testing](/strategies/testing)
