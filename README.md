<div align="center">

<img src="apps/desk/src/characters/pole.svg" width="128" height="128" alt="Pole the Polar Bear" style="image-rendering: pixelated;" />

# Permafrost

**Your AI trading desk, locked in the ice.**

A Go framework for self-custodied algorithmic trading with optional LLM augmentation. Hummingbot-style strategies. Real killswitch. Hand-authored arctic-themed operator UI.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/teslashibe/permafrost?color=50D2C2)](https://github.com/teslashibe/permafrost/releases)
[![Docs](https://img.shields.io/badge/docs-live-50D2C2)](https://teslashibe.github.io/permafrost/)
[![Hyperliquid](https://img.shields.io/badge/perp-Hyperliquid-50D2C2)](https://hyperliquid.xyz/)
[![Solana](https://img.shields.io/badge/spot-Solana-9945FF?logo=solana)](https://solana.com/)
[![Bittensor](https://img.shields.io/badge/spot-Bittensor%20(alpha)-FF6B35)](https://bittensor.com/)

📚 **[Read the docs](https://teslashibe.github.io/permafrost/)** &middot; 🐧 **[Run the demo](https://teslashibe.github.io/permafrost/getting-started/make-demo)** &middot; 🦊 **[Bittensor alpha demo](https://teslashibe.github.io/permafrost/getting-started/bittensor)** &middot; 🐳 **[The Cast](https://teslashibe.github.io/permafrost/brand/cast)**

</div>

---

## What this is

Permafrost is a self-custodied, locally-runnable trading framework built around a stable Strategy SAPI. You write deterministic Go strategies, optionally augment them with an LLM, and the framework handles exchange adapters, swap routing, reconciliation, PnL accounting, risk gates, the killswitch, and decision provenance.

Strategies are first-class extensions, not patches. Each one lives as its own subdirectory under `strategies/`, calls `strategy.Register` in `init()`, and is enabled by adding one blank-import line to each binary's `strategies.go`. See the [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi).

The OSS build ships six reference strategies: three classics (`noop`, `dca_buy`, `market_maker_basic`) plus three Bittensor alpha-token strategies (`alpha_dca`, `alpha_momentum`, `alpha_yield`). Real strategies -- basis trades, market makers, anything you can express as Go -- are yours to build, share, or [keep private](https://teslashibe.github.io/permafrost/strategies/private-strategies).

---

## The expedition

Permafrost uses a polar-camp metaphor for every moving part of the framework. Every character is a hand-authored pixel-art SVG sprite that animates on the [Trading Desk UI](https://teslashibe.github.io/permafrost/operations/trading-desk-ui).

<table>
<tr>
<td align="center" width="11%"><img src="apps/desk/src/characters/pole.svg"    width="56" height="56" alt="Pole the polar bear" /><br/><b>Pole</b><br/><sub>Camp Director</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/penguin.svg" width="56" height="56" alt="Penguin trader" /><br/><b>Penguin</b><br/><sub>Strategy agent</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/narwhal.svg" width="56" height="56" alt="Narwhal advisor" /><br/><b>Narwhal</b><br/><sub>LLM consult</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/owl.svg"     width="56" height="56" alt="Aurora the snowy owl" /><br/><b>Aurora</b><br/><sub>Risk monitor</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/husky.svg"   width="56" height="56" alt="Skipper the husky" /><br/><b>Skipper</b><br/><sub>Reconciler</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/walrus.svg"  width="56" height="56" alt="Kelp the walrus" /><br/><b>Kelp</b><br/><sub>Swap router</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/whale.svg"   width="56" height="56" alt="Frostbite the whale" /><br/><b>Frostbite</b><br/><sub>Killswitch</sub></td>
<td align="center" width="11%"><img src="apps/desk/src/characters/mammoth.svg" width="56" height="56" alt="Tusk the mammoth" /><br/><b>Tusk</b><br/><sub>Private strategy</sub></td>
</tr>
</table>

---

## Quick start

> Requires Docker and Go 1.25+.

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
make demo
```

That's it. `make demo` builds the binaries, brings up Postgres + `permafrostd` in Docker, runs the [`init` wizard](https://teslashibe.github.io/permafrost/getting-started/init-and-doctor), recruits a paper-mode `noop` agent named **Pip**, and tails decisions in your terminal. Tear down with `make demo-clean`.

### Bittensor alpha-trading demo

```bash
make demo-bittensor
```

Spins up the same stack PLUS a local Subtensor chain in Docker, bootstraps a tradeable subnet, and recruits **three live-mode trading agents** that execute real on-chain `add_stake_limit` extrinsics:

- 🐧 **Tao** — `alpha_dca` (DCA into bootstrapped subnet)
- 🐧 **Mo** — `alpha_momentum` (rotation across subnets by momentum)
- 🐧 **Yumi** — `alpha_yield` (volatility-stability rebalance)

Verified end-to-end: ~1,000 TAO → ~595 alpha tokens minted on-chain in a single demo run. See the [Bittensor walkthrough](https://teslashibe.github.io/permafrost/getting-started/bittensor) for full details.

For the manual walk-through (one command at a time), see [local install](https://teslashibe.github.io/permafrost/getting-started/local-install).

### Trading Desk UI

```bash
cd apps/desk
npm install
npm run dev          # http://127.0.0.1:5173
```

The arctic-themed operator dashboard. Drag the HUDs, drag the characters, watch decisions land in real time. Falls back to demo data when the daemon isn't running. See [Trading Desk UI docs](https://teslashibe.github.io/permafrost/operations/trading-desk-ui).

---

## What you get out of the box

| Area | Capabilities |
|---|---|
| **Framework** | Generic Strategy SAPI · Hummingbot-style `strategies/` tree · `pkg/strategy`, `pkg/types`, `pkg/inference` |
| **Perp venues** | Hyperliquid (EIP-712 action signing, OpenOrders, idempotent place/cancel) |
| **Spot venues** | Solana via Jupiter (Jito bundles) · EVM via 1inch v6 (Ethereum, Base, Avalanche, BSC) · **Bittensor subnet alpha tokens via on-chain Subtensor RPC (sr25519 signing, `add_stake_limit` / `remove_stake_limit`)** |
| **Custody** | Self-custodied keystore; private key bytes never leave `internal/wallet` |
| **Risk** | Pre-trade limits, circuit breakers (drawdown, daily loss, funding flip) |
| **Killswitch** | Real: cancels open orders, flattens shorts, opt-in spot liquidation to USDC via SwapVenue |
| **AI** | OpenAI-compatible LLM-veto (OpenAI, OpenRouter, Groq, vLLM, Ollama) · decision provenance to the prompt |
| **Strategies** | `noop`, `dca_buy`, `market_maker_basic`, `alpha_dca`, `alpha_momentum`, `alpha_yield` · `permafrost strategy-new` scaffolds your own |
| **Tooling** | `permafrost init`, `doctor`, `agent`, `vault`, `wallet`, `serve`, `backtest` |
| **Distribution** | Multi-arch Docker on GHCR (amd64 + arm64) · `cli` compose service · `make demo` one-shot |
| **UI** | React + Vite arctic-themed Trading Desk with hand-authored pixel-art sprites |

---

## Architecture

```mermaid
flowchart LR
    CLI[permafrost CLI] -->|REST + DB| API
    Desk[Trading Desk UI] -->|REST| API
    subgraph permafrostd
      API[Fiber API]
      SUP[Supervisor]
      AGENT[Agent Runtime]
      STRAT[Strategy &lpar;your code&rpar;]
      RISK[Risk + Killswitch]
      INF[Inference]
      WAL[Wallet]
      RECON[Reconcile + PnL]
      API --> AGENT
      SUP --> AGENT
      AGENT --> STRAT
      AGENT --> RISK
      AGENT --> INF
      AGENT --> WAL
      AGENT --> RECON
    end
    AGENT -->|writes| TS[(TimescaleDB)]
    AGENT -->|orders| HL[Hyperliquid]
    AGENT -->|swaps| JUP[Jupiter / 1inch]
    AGENT -->|stake/unstake| BT[Bittensor / Subtensor]
    INF -->|HTTPS| LLM[OpenAI-compatible LLM]
    WAL --> KS[Encrypted local keystore]
```

Full architecture page: [docs/introduction/architecture](https://teslashibe.github.io/permafrost/introduction/architecture).

---

## Going live

Paper mode is the default -- real market data, no real orders. To trade real money:

```bash
# Import your spot signer (Solana keypair, Phantom-style JSON / base58 / hex)
permafrost wallet import --chain solana --from ~/.config/solana/id.json

# Import your Hyperliquid perp signer (hex private key in a file)
echo "0xYOUR_PRIVATE_KEY" > /tmp/hl-key
permafrost wallet import --chain hyperliquid --from /tmp/hl-key
shred -u /tmp/hl-key

permafrost vault init
permafrost agent set-mode <id> live
permafrost agent run     <id> --confirm-live   # explicit foreground gate
```

**EVM RPCs:** pick fresh ones from [chainlist.org](https://chainlist.org/) and drop them into `config.yaml`. Public defaults rate-limit aggressively in production.

---

## Safety

A leveraged AI agent will absolutely try to nuke a vault if you let it. Permafrost ships with these guardrails:

- **Spot-first execution** -- DEX swap must confirm before the perp short is sent
- **Idempotent intents** -- every order and swap carries a deterministic client ID, deduped at the DB layer
- **Paper mode by default** -- new agents land in `paper`; promoting requires explicit `set-mode live` and `--confirm-live` on `agent run`
- **Real killswitch** -- `permafrost agent kill <id>` cancels open orders, flattens shorts via reduce-only market orders, and (with `--liquidate-spot`) swaps every spot leg back to USDC via the configured `SwapVenue`. Documented at [killswitch tuning](https://teslashibe.github.io/permafrost/operations/killswitch-tuning).
- **Mode validation** -- typo-resistant: `agent.ParseMode` rejects anything that isn't exactly `paper` or `live` at every input boundary
- **Mainnet gating** -- Hyperliquid live mode behind explicit per-agent `network` + `mode` columns

When something does go wrong, **Frostbite the Whale** surfaces -- that's the killswitch character on the dashboard.

### Known limitations (v0.1.0)

These are scaffolded in code but not yet wired end-to-end. None of them affect paper-mode operators. Tracked for v0.1.x; do not run real money against an agent that depends on any of them being fixed.

- **Circuit breakers are scaffolded but not invoked per tick.** `MaxDrawdownBreaker` and `DailyLossBreaker` exist in `internal/risk/breakers.go` and are installed by `BuildRiskEngine`, but `Runtime` does not yet call `Risk.Portfolio` per tick, so they cannot trip. `FundingFlipBreaker` is defined but not registered.
- **`--confirm-live` is only enforced by `permafrost agent run`.** `permafrostd serve` will start any agent with `mode=live, status=running` on boot without re-prompting. Tracked by [#38](https://github.com/teslashibe/permafrost/issues/38).
- **Decision provenance stores the derived intent and rationale, not the raw LLM prompt/response.** The `agent_decisions` schema has columns for provider/model/tokens/cost; the runtime fills the first two but not the prompt/response blobs. `PersistInferenceCall` exists but has no callers yet.
- **`market_maker_basic` does not persist maker state across restarts.** Treat it as a reference implementation, not a production maker.

---

## CLI

```
permafrost init                      # interactive setup wizard
permafrost doctor                    # preflight check (Go / Docker / DB / RPCs / strategies)
permafrost strategy-new <name>       # scaffold a new strategy

permafrost wallet     show | generate | import | path
permafrost vault      init | deposit | withdraw | lockup | status | record-nav | nav
permafrost assets     list | sync
permafrost agent      create | list | status | decisions | set-mode | set-network | start | stop | kill | tick | run
permafrost strategy   list | backtest <name>
permafrost inference  test | list
permafrost swap       quote
permafrost bittensor  subnets | balance | price | bootstrap
permafrost risk       show
permafrost pnl        summary | positions | history
permafrost reconcile
permafrost db         migrate up | down | status
permafrost serve
permafrost version
```

Full reference: [`/reference/cli`](https://teslashibe.github.io/permafrost/reference/cli).

---

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go 1.25+ |
| HTTP API | [Fiber](https://gofiber.io/) |
| Database | [TimescaleDB](https://www.timescale.com/) |
| Migrations | [`goose`](https://github.com/pressly/goose) |
| Query layer | [`pgx`](https://github.com/jackc/pgx) (hand-written SQL; sqlc planned for v0.2) |
| CLI | [`cobra`](https://github.com/spf13/cobra) |
| Config | [`viper`](https://github.com/spf13/viper) |
| Logging | [`log/slog`](https://pkg.go.dev/log/slog) |
| Perp | [Hyperliquid](https://hyperliquid.xyz/) |
| Spot | [Jupiter](https://jup.ag/) (Solana) · [1inch v6](https://1inch.io/) (EVM) · [Subtensor](https://github.com/opentensor/subtensor) (Bittensor alpha tokens) |
| MEV | [Jito](https://www.jito.wtf/) bundles |
| Inference | OpenAI-compatible (OpenAI, OpenRouter, Groq, vLLM, Ollama, …) |
| UI | React 18 + Vite 5, no framework, hand-authored SVG sprites |
| Deploy | Docker + docker-compose · Multi-arch GHCR image |

---

## Roadmap

v0.1.0 (this release) closes the [Trading Desk epic](https://github.com/teslashibe/permafrost/issues/30):

| | Milestone | Status |
|---|---|---|
| M0 | Skeleton (config, logging, compose, migrations) | ✅ |
| M1 | Core interfaces (`Strategy`, `Venue`, `SwapVenue`, `Provider`, `Signer`, `Risk`) | ✅ |
| M2 | Hyperliquid adapter | ✅ |
| M3 | OpenAI-compatible inference | ✅ |
| M4 | Wallet & keystore | ✅ |
| M5 | Solana SwapVenue (Jupiter + Jito) | ✅ |
| M6 | Asset registry | ✅ |
| M7 | Vault & accounting | ✅ |
| M8 | Agent runtime | ✅ |
| M9 | Risk + circuit breakers + real killswitch | ✅ |
| M10 | Backtest harness | ✅ |
| M11 | EVM SwapVenue (1inch v6, multi-chain) | ✅ |
| M12 | Operator tooling (`init`, `doctor`, `make demo`, `strategy-new`) | ✅ |
| M13 | Multi-arch Docker image (GHCR) | ✅ |
| M14 | Reference strategies (`dca_buy`, `market_maker_basic`) | ✅ |
| M15 | Trading Desk UI + sprite cast | ✅ |
| M16 | Brand narrative + cast docs | ✅ |

Up next: hosted vault accounting on-chain, additional venues, more reference strategies.

---

## Contributing

Contributions welcome. Read the [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi) (for new strategies) and [architecture](https://teslashibe.github.io/permafrost/introduction/architecture) before opening a PR.

---

## License

[MIT](LICENSE)

<div align="center">
<sub>The expedition is on the ice. 🐻🐧🦄🦉🐕🦣🐳🪙</sub>
</div>
