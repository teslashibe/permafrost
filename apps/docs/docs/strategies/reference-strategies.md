---
sidebar_position: 6
title: Reference strategies
---

# Reference strategies

The OSS build ships three strategies as reference implementations. Together they exercise every code path in the runtime so you have a working example for each shape of strategy you might want to write.

| Strategy | SwapIntent | OrderIntent | Cancels | LLM veto | Lives at |
|---|---|---|---|---|---|
| `noop` | -- | -- | -- | -- | `strategies/noop/` |
| `dca_buy` | ✓ | -- | -- | -- | `strategies/dca_buy/` |
| `market_maker_basic` | -- | ✓ | ✓ | ✓ | `strategies/market_maker_basic/` |
| `alpha_dca` | ✓ | -- | -- | -- | `strategies/alpha_dca/` |
| `alpha_momentum` | ✓ | -- | -- | -- | `strategies/alpha_momentum/` |
| `alpha_yield` | ✓ | -- | -- | -- | `strategies/alpha_yield/` |

## `noop`

The smallest strategy that satisfies the SAPI. ~20 lines of Go that
return `Decision{}` every tick. Use it as a copy-paste starting point
or as the smoke-test target for verifying your install -- see
[running noop](/getting-started/running-noop).

## `dca_buy`

Deterministic dollar-cost-averaging into a configured spot asset. No
shorting, no LLM. Useful in real life: anyone who wants to DCA into
SOL / ETH / WIF from their own wallet has a working strategy out of
the box.

### Configuration

Pass via `--config-json` at agent-create time:

```json
{
  "asset": "SOL",
  "chain": "solana",
  "usdc_per_tick": "50",
  "interval_secs": 14400,
  "slippage_bps": 50
}
```

| Key | Default | Notes |
|---|---|---|
| `asset` | `"SOL"` | Spot symbol to buy. Resolved against the configured swap router. |
| `chain` | `"solana"` | Settlement chain. Must have a USDC mapping (Solana, Ethereum, Base, Avalanche, BSC). |
| `usdc_per_tick` | `"50"` | USDC notional to buy each cycle. Must be positive. |
| `interval_secs` | `14400` (4h) | Minimum gap between buys. Cooldown after a successful tick. |
| `slippage_bps` | `50` | Per-swap slippage tolerance. 0..1000. |

### Behaviour

```
tick → cooldown active?            ─ yes ──► skip (note: "cooldown: 3h12m remaining")
       │
       no
       │
       ▼
       USDC mapped for chain?      ─ no ──► skip (note: "no USDC mapping for chain X")
       │
       yes
       │
       ▼
       emit SwapIntent: USDC → asset, BasisKey="dca:<ASSET>"
       update lastBuyAt
       Decision{Confidence: 1.0, Notes: "dca buy: 50 USDC → SOL on solana"}
```

Cooldown is in-memory. A daemon restart resets it; the strategy may
buy immediately on the first tick after restart even if it bought
recently. v2 will read prior buy times from `BasisPositions`.

### Run it

```bash
permafrost agent create \
    --strategy dca_buy \
    --perp hyperliquid \
    --spot solana \
    --alloc 1000 \
    --tick-secs 60 \
    --config-json '{"asset":"SOL","usdc_per_tick":"50","interval_secs":14400}'
permafrost agent start <id>
```

## `market_maker_basic`

Places paired bid/ask limit orders on Hyperliquid around the mid,
half-spread = `SpreadBps/2` on each side. Refreshes every
`RefreshTicks` ticks. Optional LLM veto consults the inference
provider per refresh cycle (JSON-Schema response, graceful degrade
on `ErrUnsupportedFeature`).

### Configuration

```json
{
  "symbol":         "WIF",
  "spread_bps":     25,
  "order_size":     "10",
  "refresh_ticks":  1,
  "use_llm_veto":   true,
  "veto_model":     "anthropic/claude-sonnet-4.5"
}
```

| Key | Default | Notes |
|---|---|---|
| `symbol` | (required) | Perp symbol (e.g. `"WIF"`). |
| `spread_bps` | `25` | Half-spread around mid in bps. 1..1000. |
| `order_size` | (required) | Per-side base-asset quantity. Must be positive. |
| `refresh_ticks` | `1` | Quote refresh cadence in ticks. |
| `use_llm_veto` | `false` | If true, ask LLM whether to skip each refresh cycle. |
| `veto_model` | (agent's) | Falls back to `Services.InferenceModel`. |

### Behaviour per tick

```
tick → tickCount++
       │
       ▼
       no market data for symbol?       ─ yes ──► skip (note: "no market data for X")
       │
       no
       │
       ▼
       no mark price?                    ─ yes ──► skip (note: "no mark price; skipping")
       │
       no
       │
       ▼
       refresh-ticks gate                ─ skip ─► skip (note: "skip: tick N, refresh every M")
       │
       │
       ▼
       use_llm_veto and veto = true?     ─ yes ──► skip (note: "vetoed: <reason>")
       │
       no
       │
       ▼
       emit two limit orders: bid at mid*(1-half), ask at mid*(1+half)
       BasisKey = "mm:<SYMBOL>"
       Decision{Confidence: 0.7, ...}
```

`Warmup` fails fast if `use_llm_veto: true` but no inference provider
is wired on the agent -- the operator notices at startup, not on the
first decision tick.

### LLM veto

The strategy POSTs a JSON-Schema-constrained prompt:

```
System: You are a volatility filter for a basic crypto perpetuals
        market maker. Decide whether the maker should SKIP this
        refresh cycle. Default to NOT vetoing.

User:   Symbol: WIF
        Mark: 1.234
        Funding (annualised): 0.045
        Funding interval: 1h0m0s

        Should the maker SKIP requoting now?
```

The response must match:

```json
{ "veto": true|false, "reason": "string ≤ 200 chars" }
```

A provider that returns `ErrUnsupportedFeature` (e.g. base Ollama
model without JSON-Schema mode) is treated as `veto: false` -- the
strategy quotes normally rather than blocking on a feature the model
can't enforce.

### Run it

```bash
permafrost agent create \
    --strategy market_maker_basic \
    --perp hyperliquid \
    --inference openrouter:anthropic/claude-sonnet-4.5 \
    --alloc 500 \
    --tick-secs 30 \
    --config-json '{"symbol":"WIF","order_size":"10","spread_bps":25,"use_llm_veto":true}'
permafrost agent start <id>
```

## `alpha_dca`

Dollar-cost-average into a fixed set of Bittensor subnet alpha tokens. Every N ticks, buys `tao_per_buy` TAO of each listed subnet's alpha. Pure on-chain — no LLM, no third-party data.

### Configuration

```json
{
  "subnets":        [8, 3, 19],
  "tao_per_buy":    1.0,
  "interval_ticks": 10,
  "slippage_bps":   100
}
```

| Key | Default | Notes |
|---|---|---|
| `subnets` | `[8, 3, 19]` | Netuids to buy. SN8 = Vanta/Taoshi, SN3 = Templar, SN19 = Inference. |
| `tao_per_buy` | `1.0` | TAO amount per subnet per buy tick. |
| `interval_ticks` | `10` | Minimum ticks between buys. With 30s ticks → buy every 5min. |
| `slippage_bps` | `100` | Per-swap slippage cap (1.00%). AMM slippage on alpha pools can be material. |

### Run it

```bash
permafrost agent create \
    --strategy alpha_dca \
    --spot bittensor \
    --tick-secs 30 \
    --config-json '{"subnets":[8,3,19],"tao_per_buy":1.0,"interval_ticks":10}'
permafrost agent start <id>
```

Requires:
- A funded Bittensor wallet in the keystore (`permafrost wallet generate --chain bittensor`)
- `bittensor.allow_submit: true` in `config.yaml` to enable real on-chain trading

## `alpha_momentum`

Tracks rolling price changes across all subnets in `universe`, rotates into the top-K by momentum, exits when momentum flips below `exit_threshold`. Pure price-action — no external signals.

### Configuration

```json
{
  "universe":         [1, 3, 8, 19, 64],
  "window_ticks":     30,
  "top_k":            5,
  "exit_threshold":   -0.02,
  "tao_per_position": 5.0,
  "slippage_bps":     100
}
```

| Key | Default | Notes |
|---|---|---|
| `universe` | `[1..64]` | Netuids tracked for momentum. |
| `window_ticks` | `30` | Rolling window length for momentum (last - first) / first. |
| `top_k` | `5` | Maximum number of subnets held simultaneously. |
| `exit_threshold` | `-0.02` | Momentum below which positions are exited (e.g. -2%). |
| `tao_per_position` | `5.0` | TAO notional allocated to each new position. |
| `slippage_bps` | `100` | Per-swap slippage cap. |

### Behaviour per tick

```
tick → ingest current price for every universe symbol
       compute momentum = (last - first) / first over window_ticks
       sort subnets by momentum desc
       │
       ▼
       held positions with momentum < exit_threshold → emit sell (alpha → TAO)
       │
       ▼
       top-K subnets with momentum > 0 not yet held → emit buy (TAO → alpha)
       │
       ▼
       Decision{Confidence: 0.7, Notes: "holding N subnets, M entries, K exits"}
```

Position sizing: entries use `tao_per_position` TAO; exits sell the alpha amount estimated at entry (`tao_per_position / entry_price`). When the framework surfaces actual venue alpha balances via `Services`, the strategy will pick those up automatically.

### Run it

```bash
permafrost agent create \
    --strategy alpha_momentum \
    --spot bittensor \
    --tick-secs 30 \
    --config-json '{"universe":[1,3,8,19,64],"top_k":3,"tao_per_position":2.0}'
permafrost agent start <id>
```

## `alpha_yield`

Ranks subnets by **price-stability proxy** for emission yield: subnets whose alpha price is stable over the rolling window are ranked higher, the assumption being that emissions accumulate value rather than being eroded by churn. Stakes into the top-K, rebalances every `rebalance_ticks` ticks.

This is a deliberately conservative stand-in for on-chain `Emission` storage reads (which require runtime-version-specific storage layout work). The strategy still aims at the same goal — yield-stable subnets — and is the cleanest fork target for a real emission-aware variant.

### Configuration

```json
{
  "universe":          [1, 3, 8, 19, 64],
  "top_k":             3,
  "rebalance_ticks":   50,
  "min_yield_delta":   0.05,
  "volatility_window": 30,
  "tao_per_position":  10.0,
  "slippage_bps":      100
}
```

| Key | Default | Notes |
|---|---|---|
| `universe` | `[1..64]` | Subnets evaluated. |
| `top_k` | `3` | Subnets held at any time. |
| `rebalance_ticks` | `50` | Re-evaluation cadence. With 30s ticks → rebalance every 25min. |
| `min_yield_delta` | `0.05` | Minimum rank-improvement to trigger a rotation (anti-churn). |
| `volatility_window` | `30` | Lookback for stability score. |
| `tao_per_position` | `10.0` | TAO notional per position. |
| `slippage_bps` | `100` | Per-swap slippage cap. |

### Run it

```bash
permafrost agent create \
    --strategy alpha_yield \
    --spot bittensor \
    --tick-secs 30 \
    --config-json '{"universe":[1,3,8,19,64,52],"top_k":2,"tao_per_position":5.0}'
permafrost agent start <id>
```

## v2 plans for the references

- `dca_buy` -- read prior buy times from `BasisPositions` so cooldown
  is restart-resilient.
- `market_maker_basic` -- explicit cancel emission for stale quotes
  (today the runtime's reduce-only handling drops them naturally as
  new quotes replace them).
- A third reference (`grid_trader` or similar) demonstrating
  range-bound passive accumulation.

## Next steps

- [Scaffolding a new strategy](/strategies/scaffolding) -- `permafrost strategy-new`
- [Decision contract](/strategies/decision-contract)
- [Services](/strategies/services)
