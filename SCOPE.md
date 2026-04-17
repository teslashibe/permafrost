# Permafrost — MVP Scope

> Permafrost is an open-source DeFi market-making and hedge-fund protocol that uses agents to deploy capital into a range of AI trading strategies. Capital and gains are locked into the permafrost.

This document defines the framework for the MVP. Implementation order, interfaces, data model, and decisions are all here. Build this scaffold first; the first agent (`funding_arb_basic`) comes after.

---

## 0. Positioning

- **v1 — single-user, local-first, open source.** One operator (us) runs `permafrostd` on their own machine against their own keys and capital. Anyone can clone, configure, and run. No multi-tenant concerns.
- **v2 — hosted vaults.** Same codebase, deployed by us, accepting external LP capital into on-chain vault contracts. Out of scope here, but the v1 framework should not preclude it.

The whole MVP is built so a single operator can run it locally with `docker-compose up` and a config file.

---

## 1. Stack

| Layer | Choice |
|---|---|
| Language | Go |
| HTTP API | Fiber |
| Database | TimescaleDB (Postgres + hypertables) |
| Migrations | `goose` |
| Query layer | `sqlc` over `pgx` |
| CLI | `cobra` |
| Config | `viper` (yaml + env) |
| Logging | `log/slog` (stdlib, structured; JSON in prod, text in dev) |
| Perp venue | Hyperliquid Go SDK |
| Spot venue (v1) | Solana, via Jupiter aggregator |
| Solana RPC | Helius (paid) or Triton |
| MEV protection | Jito bundles (Solana) |
| Inference | Single OpenAI-compatible client (works with OpenAI, OpenRouter, Groq, Together, Fireworks, vLLM, Ollama, LM Studio) |
| Key management | Local encrypted JSON keystore, env-loaded passphrase |
| Deploy | Docker + docker-compose, runs locally |

---

## 2. Conceptual model

Five primitives the system revolves around:

- **Vault** — the "permafrost." Holds locked capital and realized gains. Tracks deposits, lockups, NAV, high-water mark. Off-chain accounting in v1.
- **Wallet** — a self-custodied keypair on a chain. v1 ships one Solana keypair + one Hyperliquid signer.
- **Strategy** — pure logic. Given market state, positions, and signals, produces target actions (orders + swaps). Stateless across restarts.
- **Agent** — a runtime that wraps a Strategy with: an inference provider, one or more venues, risk limits, a schedule, and a decision log. Agents are the unit of deployment.
- **Position** — a logical trade. For the funding arb, a position is a *pair* (long spot leg + short perp leg) with combined PnL.

**Capital flow:** `Vault → Allocator → Agent(s) → (perp Venue + swap SwapVenue)`
**Telemetry flow:** `Venues → Agent → Timescale → API/CLI`

---

## 3. Repository layout

```
permafrost/
├── cmd/
│   ├── permafrost/         # CLI entrypoint (cobra)
│   └── permafrostd/        # API server entrypoint (fiber)
├── internal/
│   ├── config/             # viper config, env, secrets
│   ├── vault/              # deposits, NAV, lockups, accounting
│   ├── allocator/          # capital allocation across agents + chains
│   ├── agent/              # agent runtime, scheduler, lifecycle
│   ├── strategy/           # Strategy interface + registry
│   │   └── funding_arb_basic/   # first strategy
│   ├── exchange/           # Venue interface (orderbook venues)
│   │   └── hyperliquid/    # HL SDK adapter (perp venue)
│   ├── swap/               # SwapVenue interface (DEX/aggregator)
│   │   └── jupiter/        # Solana via Jupiter
│   ├── chain/              # chain-level RPC clients + tx tracking
│   │   └── solana/         # Solana RPC, Jito bundle client, priority fees
│   ├── wallet/             # keystore, signers, address book
│   │   ├── keystore.go     # encrypted JSON, passphrase from env
│   │   ├── solana.go       # ed25519 signer
│   │   └── hyperliquid.go  # HL signer
│   ├── assets/             # asset registry (perp ↔ chain mint mapping)
│   │   └── registry.yaml   # hand-curated, source of truth
│   ├── inference/          # OpenAI-compatible client
│   │   ├── provider.go
│   │   ├── openai/
│   │   └── mock/
│   ├── risk/               # pre-trade + portfolio risk checks
│   ├── marketdata/         # subscriptions, candles, funding cache
│   ├── store/              # Timescale repo layer (sqlc + pgx)
│   │   ├── migrations/
│   │   └── queries/
│   ├── api/                # fiber handlers, middleware, DTOs
│   ├── telemetry/          # logs, metrics, traces
│   └── eventbus/           # in-proc pub/sub (NATS-ready interface)
├── pkg/                    # public exports (interfaces if open-sourcing SDK)
├── deploy/
│   ├── docker/
│   └── compose/
├── migrations/             # goose migrations
├── docs/
└── SCOPE.md
```

**Layering rules:**
- `internal/strategy/*` and `internal/agent` may only depend on the four core interfaces (`Strategy`, `Venue`, `SwapVenue`, `Provider`, `Risk`) — never on concrete adapters.
- `internal/store` is the only package that imports `pgx`.
- `internal/wallet` is the only package that touches private key bytes. Everything else uses `Signer`.

---

## 4. Core interfaces

### 4.1 Strategy

```go
package strategy

type Strategy interface {
    Name() string
    Warmup(ctx context.Context, in WarmupInput) error
    Decide(ctx context.Context, in DecisionInput) (Decision, error)
}

type DecisionInput struct {
    Now             time.Time
    Market          MarketSnapshot       // book, trades, funding, predicted next funding
    PerpPositions   []Position           // from Hyperliquid
    SpotBalances    []WalletBalance      // from Solana wallet
    BasisPositions  []BasisPosition      // logical paired positions
    Cash            map[string]Balance   // keyed by location: "hyperliquid", "solana"
    Signals         map[string]any       // from inference or indicators
    Limits          RiskLimits
}

type Decision struct {
    Orders     []OrderIntent       // perp orders (place/cancel)
    Swaps      []SwapIntent        // DEX swaps
    Notes      string              // human/LLM-readable rationale
    Confidence float64
}
```

Strategies own paired-execution invariants (open spot first, then short, unwind partials on next tick).

### 4.2 Venue (orderbook / perp)

```go
package exchange

type Venue interface {
    Name() string
    Subscribe(ctx context.Context, symbols []string) (<-chan MarketEvent, error)
    Place(ctx context.Context, o OrderIntent) (OrderAck, error)
    Cancel(ctx context.Context, id OrderID) error
    Positions(ctx context.Context) ([]Position, error)
    Balances(ctx context.Context) (Balance, error)
    FundingRates(ctx context.Context, symbols []string) ([]FundingRate, error)
}
```

### 4.3 SwapVenue (DEX / aggregator)

DEX execution is request-quote-execute-confirm, not place-and-wait. Different shape from `Venue`.

```go
package swap

type SwapVenue interface {
    Name() string
    Chain() string                       // "solana"
    Quote(ctx context.Context, q QuoteRequest) (Quote, error)
    Swap(ctx context.Context, q Quote, slippageBps int) (TxHash, error)
    WaitConfirm(ctx context.Context, tx TxHash) (SwapResult, error)
    Balance(ctx context.Context, asset Asset) (decimal.Decimal, error)
}

type QuoteRequest struct {
    InToken  Asset
    OutToken Asset
    InAmount decimal.Decimal
    Mode     QuoteMode   // ExactIn | ExactOut
}
```

Solana implementation routes through Jupiter and submits via Jito bundles for MEV protection.

### 4.4 Inference provider

One interface, one implementation (OpenAI-compatible). Multiple providers via `base_url`.

```go
package inference

type Provider interface {
    Name() string
    Complete(ctx context.Context, req Request) (Response, error)
    Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}

type Request struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolSpec
    JSONSchema  *Schema
    Temperature float64
    MaxTokens   int
}

var ErrUnsupportedFeature = errors.New("inference: unsupported feature")
```

### 4.5 Signer

```go
package wallet

type Signer interface {
    Address() string
    Chain() string                        // "solana", "hyperliquid"
    Sign(ctx context.Context, payload []byte) ([]byte, error)
}
```

`Signer` is the only thing strategies/adapters ever see. Raw key bytes never leave `internal/wallet`.

### 4.6 Risk

```go
package risk

type Engine interface {
    PreTrade(ctx context.Context, agentID string, intent any, snap PortfolioSnapshot) Verdict
    Portfolio(ctx context.Context, snap PortfolioSnapshot) Verdict
}
```

`intent any` accepts both `OrderIntent` and `SwapIntent`. Verdicts are advisory + blocking.

### 4.7 Agent runtime

```
tick → fetch state → Strategy.Decide
     → risk.PreTrade (per intent)
     → execute spot leg(s) first, await confirm
     → execute perp leg(s)
     → persist everything
     → reconcile any partial state
```

---

## 5. Data model (Timescale)

### Relational
- `vaults`
- `deposits`, `withdrawals`, `lockups`
- `nav_snapshots`
- `agents` (id, strategy, allocation, status, config jsonb)
- `agent_runs` (id, agent_id, started_at, ended_at, exit_reason)
- `wallets` (id, chain, address, label)
- `assets` (perp_symbol, perp_venue, chain, mint, decimals, hedge_ratio) — synced from `registry.yaml`
- `strategy_positions` (id, agent_id, kind, legs jsonb, opened_at, closed_at, state, realized_funding, realized_basis_pnl, realized_fees, realized_gas)

### Hypertables (partitioned on `time`)
- `market_ticks` (time, venue, symbol, bid, ask, mid, vol)
- `book_snapshots` (time, venue, symbol, levels jsonb)
- `funding_rates` (time, venue, symbol, rate, interval, predicted_next)
- `funding_payments` (time, agent_id, venue, symbol, amount, rate)
- `agent_decisions` (time, agent_id, input_hash, decision jsonb, rationale text, model, tokens_in, tokens_out, latency_ms)
- `orders` (time, agent_id, venue, symbol, side, type, px, qty, status, client_id, position_id)
- `fills` (time, order_id, agent_id, px, qty, fee)
- `swaps` (time, agent_id, chain, dex, in_token, out_token, in_amt, out_amt, slippage_bps, gas_paid, tx_hash, status, position_id)
- `positions_snapshots` (time, agent_id, symbol, qty, avg_px, upnl, rpnl)
- `wallet_balances` (time, chain, address, asset, amount)
- `gas_spend` (time, chain, agent_id, native_units, usd_equiv)
- `pnl_minutely` (time, agent_id, gross, funding, fees, gas, net) — Timescale continuous aggregate
- `inference_calls` (time, provider, model, latency_ms, cost_usd, status)

`position_id` foreign keys link orders, fills, and swaps back to the `strategy_positions` row that owns them.

---

## 6. Inference configuration

Single OpenAI-compatible client. Providers are named config entries:

```yaml
inference:
  default: openrouter
  providers:
    openai:
      base_url: https://api.openai.com/v1
      api_key_env: OPENAI_API_KEY
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key_env: OPENROUTER_API_KEY
    ollama:
      base_url: http://localhost:11434/v1
      api_key_env: ""
    groq:
      base_url: https://api.groq.com/openai/v1
      api_key_env: GROQ_API_KEY
```

Agents reference a provider by name + a model string:
`provider: openrouter, model: anthropic/claude-sonnet-4.5`

Native non-OpenAI SDKs (Anthropic Messages, Gemini, Bedrock) can be added later without touching strategies.

---

## 7. Wallet & key management (v1)

- One encrypted JSON keystore file at `~/.permafrost/keystore.json`.
- Passphrase from env: `PERMAFROST_KEYSTORE_PASSPHRASE`.
- Holds: one Solana ed25519 keypair, one Hyperliquid signer.
- `permafrost wallet ...` commands manage import/show/rotate.
- `internal/wallet` is the only package that decrypts. Decrypted keys live in memory only inside `Signer` instances.
- Production hardening (KMS, hardware signers) is a v2 concern, called out in `docs/`.

---

## 8. Runtime topology

One Go module, two binaries:

- **`permafrostd`** — long-running daemon. Owns: market data subscriptions, agent supervisors, Fiber API, scheduler, RPC clients.
- **`permafrost`** — CLI. Talks to `permafrostd` via REST for control-plane ops; reads Postgres directly for reports. Has offline subcommands (backtest, strategy lint, migrate, wallet).

In-process eventbus, with an interface so we can swap to NATS / Redis Streams later.

API binds to `127.0.0.1` by default in v1 — single user, local-only, no remote exposure unless explicitly enabled.

---

## 9. CLI surface

```
permafrost wallet import --chain solana --from <path>
permafrost wallet show
permafrost wallet balances

permafrost vault init
permafrost vault deposit --amount 10000 --asset USDC --chain solana
permafrost vault status
permafrost vault nav --since 7d

permafrost agent create --strategy funding_arb_basic \
                        --perp hyperliquid --spot solana \
                        --universe wif,bonk,popcat \
                        --alloc 5000 \
                        --funding-entry-bps 50 --funding-exit-bps 10
permafrost agent start <id>
permafrost agent stop <id>
permafrost agent stop --all                 # kill switch: cancels orders + closes spot positions
permafrost agent logs <id> --tail
permafrost agent decisions <id> --since 1h
permafrost agent positions <id>             # basis positions w/ PnL breakdown
permafrost agent pnl <id>

permafrost strategy list
permafrost strategy backtest funding_arb_basic --from 2026-01-01 --to 2026-04-01

permafrost inference test --provider openrouter --model anthropic/claude-sonnet-4.5
permafrost swap quote --in USDC --out WIF --amount 100   # smoke-test Jupiter
permafrost risk show --agent <id>

permafrost db migrate up
permafrost serve                            # runs permafrostd in foreground
```

---

## 10. API surface (Fiber)

Mirrors CLI. Binds to `127.0.0.1` by default.

```
GET    /v1/health
GET    /v1/vault
POST   /v1/vault/deposit
GET    /v1/vault/nav?since=...

GET    /v1/wallets
GET    /v1/wallets/:chain/balances

GET    /v1/agents
POST   /v1/agents
POST   /v1/agents/:id/start
POST   /v1/agents/:id/stop
GET    /v1/agents/:id/decisions
GET    /v1/agents/:id/positions
GET    /v1/agents/:id/pnl

GET    /v1/strategies
POST   /v1/strategies/:name/backtest

POST   /v1/inference/complete               # passthrough for testing
POST   /v1/swap/quote                       # passthrough for testing
```

**Auth (v1):** bearer token from config; only required if API is bound to a non-loopback address.
**Middleware:** request ID, structured `slog` log, Prometheus metrics.

---

## 11. Safety rails (non-negotiable)

A leveraged AI agent will absolutely try to nuke the vault if you let it. Day-one requirements:

**Per-agent hard limits**
- max notional per leg
- max total basis exposure
- max swaps/min, max orders/min
- max daily loss (in USDC)
- max acceptable slippage on spot leg
- max acceptable basis (perp − spot) at entry

**Execution invariants**
- **Spot-first execution.** Confirm DEX swap before sending Hyperliquid short.
- **1:1 hedge ratio.** Notional matched at entry; rebalance threshold (e.g. 5%) before adjusting.
- **Idempotent intents.** Both `OrderIntent` and `SwapIntent` carry deterministic client IDs derived from `(agent_id, decision_id, slot)`.
- **Decision provenance.** Every order/swap row links to the `agent_decisions` row that produced it, including exact prompt + model response.
- **Dry-run mode.** Every agent defaults to `mode=paper` until explicitly promoted to `live`.

**Circuit breakers (auto-halt agent)**
- NAV drawdown > N%
- Funding flip negative on open position
- Basis blowout > X bps on open position
- Inference error rate > Y% over 10m
- Hyperliquid reject rate > Z%
- Solana RPC error rate > W%
- Unconfirmed swap older than 60s
- Margin ratio on Hyperliquid below buffer

**Kill switch**
- `permafrost agent stop --all` cancels open HL orders, closes HL shorts, and (configurable) liquidates spot legs to USDC.

**MEV / front-running protection (Solana)**
- All swaps submitted via Jito bundles with priority fee.
- Skip swap if quote slippage exceeds configured limit.
- Cap per-swap notional to a fraction of pool depth from quote response.

**Mainnet gating**
- Hyperliquid mainnet behind explicit config flag; default is testnet.
- Solana mainnet vs devnet selected via RPC URL; no special flag needed since devnet has no real DEX liquidity (operator must opt in to mainnet to do anything useful).

---

## 12. Docker

`docker-compose.yml` (local-first):
- `permafrostd`
- `timescaledb`
- `migrate` (one-shot, runs `goose up`)
- `grafana` (optional, for PnL/decision dashboards)

Single multi-stage `Dockerfile` builds both `permafrost` and `permafrostd` from the same module.

Operator workflow:
```
git clone ...
cp config.example.yaml config.yaml
# fill in API keys, RPC URL, model
permafrost wallet import ...
docker-compose up -d
permafrost vault init
permafrost agent create ...
permafrost agent start ...
```

---

## 13. Build milestones

1. **M0 — skeleton.** Repo layout, config, slog logging, Postgres+Timescale via compose, goose runner, health endpoint, CLI skeleton.
2. **M1 — interfaces.** Define `Strategy`, `Venue`, `SwapVenue`, `Provider`, `Signer`, `Risk` with no-op implementations and tests.
3. **M2 — Hyperliquid adapter.** Read-only first (positions, balances, market data, funding rates), then order placement on testnet.
4. **M3 — Inference.** OpenAI-compatible client + `mock` provider. Cost tracking from day one.
5. **M4 — Wallet & keystore.** Encrypted JSON keystore, Solana + Hyperliquid signers, `permafrost wallet` commands.
6. **M5 — Solana SwapVenue.** Jupiter quote + swap, Jito bundle submission, tx confirmation, balance reads, gas accounting.
7. **M6 — Asset registry.** Hand-curated `registry.yaml` for ~10 alts (WIF, BONK, POPCAT, etc.) with HL perp ↔ Solana mint mapping; loader + DB sync.
8. **M7 — Vault + accounting.** Deposits, NAV snapshots, allocator stub.
9. **M8 — Agent runtime.** Scheduler, decision loop, paired execution (spot-first), persistence, paper mode.
10. **M9 — Risk + circuit breakers + kill switch.**
11. **M10 — First strategy (`funding_arb_basic`).** Deterministic funding-rate ranking + sizing; LLM used for event/news veto layer (and only that, in v1).
12. **M11 — Backtest harness.** Replay historical funding rates against `Strategy.Decide`.

---

## 14. First strategy: `funding_arb_basic`

**Universe:** alts that have (a) a Hyperliquid perp, (b) a liquid Jupiter route on Solana, (c) an entry in `assets/registry.yaml`. v1 target: ~10 tokens.

**Core loop (deterministic):**
1. Every N minutes, fetch funding rates for universe.
2. Rank by annualized funding.
3. Filter: `rate > entry_threshold_bps`, `pool_depth > min_depth`, `not in cooldown`.
4. For each candidate not already open, propose a basis position sized to per-position cap.
5. For each open position: close if `rate < exit_threshold_bps` or `basis_blowout > limit`.

**LLM role (v1):** event/news veto. Strategy proposes candidates; inference layer is asked "any reason not to open a basis on $X right now?" with a structured-output schema returning `{veto: bool, reason: string}`. LLM never generates orders, never sets sizes, never picks the universe.

**Execution:**
- Open: spot swap on Jupiter → wait confirm → place HL short → record `BasisPosition`.
- Close: cancel any resting HL orders → market-close HL short → swap spot back to USDC → close `BasisPosition`.
- Partial-fill recovery: next tick reconciles, unwinds dangling legs.

**PnL attribution:** `funding_received − borrow_costs − HL_fees − DEX_fees − gas − slippage − basis_pnl`. All persisted per position.

---

## 15. Out of scope for v1

- Multi-tenant / external LP capital (single operator)
- On-chain vault contract (off-chain accounting only)
- UI / web dashboard (Grafana is enough)
- Additional chains beyond Solana (Base + Arbitrum are v1.1)
- Auto-bridging USDC across venues (manual pre-fund only)
- KMS / hardware signer integration (local encrypted keystore only)
- Native non-OpenAI inference SDKs
- Strategies beyond `funding_arb_basic`
- Cross-perp basis (perp-vs-perp arb across venues)
- Hosted deployment (local-first only)
