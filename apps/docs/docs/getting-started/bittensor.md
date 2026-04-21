---
sidebar_position: 7
title: Bittensor alpha tokens
---

# Trading Bittensor subnet alpha tokens

Permafrost supports buying, holding, and selling alpha tokens on every Bittensor subnet, entirely on-chain. No third-party APIs. Three reference strategies (`alpha_dca`, `alpha_momentum`, `alpha_yield`) are ready to fork.

<div align="center">

![Permafrost Trading Desk — three Bittensor alpha agents trading live](../../assets/permafrost-animation.gif)

*Three alpha-trading agents (Tao, Mo, Yumi) executing real on-chain swaps against a local Subtensor devnet.*

</div>

This guide takes you from a fresh clone to a running agent staked into your first subnet in under 10 minutes.

## What are alpha tokens?

Every Bittensor subnet (128+ active as of 2026) has its own token. You "buy" alpha by staking TAO into a subnet's on-chain AMM pool (`SubtensorModule.add_stake`); you "sell" by unstaking (`remove_stake`). Price = `tao_in_pool / alpha_in_pool`.

All data and execution lives on the Subtensor blockchain — Permafrost talks to it directly via WebSocket RPC.

## Prerequisites

- A working Permafrost install ([init wizard + doctor](/getting-started/init-and-doctor)).
- Some TAO. Buy from [Coinbase](https://www.coinbase.com/), [Kraken](https://www.kraken.com/), or [MEXC](https://www.mexc.com/) and withdraw to your Bittensor wallet (you'll create it below). Minimum recommended for testing: 5 TAO (covers a few small trades + gas).
- Optional but recommended: a self-hosted [subtensor node](https://github.com/opentensor/subtensor) for unrate-limited RPC. The default public endpoint works for getting started.

## Step 1 — Configure the RPC endpoint

In your `config.yaml`:

```yaml
bittensor:
  rpc_url: wss://entrypoint-finney.opentensor.ai:443  # public default
  network: finney                                      # finney | test | local
  allow_submit: false                                  # ⚠ keep false until you've tested
```

Or via env var:

```bash
export PERMAFROST_BITTENSOR__RPC_URL=wss://entrypoint-finney.opentensor.ai:443
```

### Endpoint options

| Provider | URL | Notes |
|---|---|---|
| Opentensor (free) | `wss://entrypoint-finney.opentensor.ai:443` | Default. Rate-limited but adequate for single-agent operation. |
| Opentensor testnet | `wss://test.finney.opentensor.ai:443` | Use for development. Get free testnet TAO from the [Bittensor Discord](https://discord.gg/bittensor). |
| Dwellir (paid) | `wss://bittensor-mainnet.dwellir.com:443` | Higher rate limits. Sign up at [dwellir.com](https://dwellir.com). |
| Self-hosted | `wss://your-node:9944` | Full sovereignty. Run [subtensor](https://github.com/opentensor/subtensor) yourself. |

## Step 2 — Verify the connection

```bash
permafrost doctor
```

You should see:

```
✓  bittensor RPC              wss://entrypoint-finney.opentensor.ai:443 (Bittensor)
```

If it fails, try a different endpoint or check your network.

## Step 3 — Generate a Bittensor wallet

```bash
permafrost wallet generate --chain bittensor
```

Output:

```
bittensor    5EPcXq5WbaH5QP2rGAoVd78o23Kwdi7SWJVyKWGhABqaUwLy

Fund this address with TAO before trading. Send TAO from
an exchange (Coinbase, Kraken, MEXC) or another Bittensor wallet.
```

The address is **SS58-encoded with prefix 42** (the standard Substrate generic format Bittensor uses). Send TAO to it from any exchange or wallet — the address is interchangeable with btcli, polkadot.js, and Subscan.

The key is **sr25519** (Bittensor's native scheme) and stored in your encrypted local keystore alongside any Solana / Hyperliquid keys you already have.

### Importing an existing wallet

If you already have a btcli or polkadot.js seed:

```bash
# 32-byte hex seed
permafrost wallet import --chain bittensor --from /path/to/seed.txt

# BIP-39 mnemonic phrase (12+ words, space-separated)
permafrost wallet import --chain bittensor --from /path/to/mnemonic.txt
```

## Step 4 — Fund the wallet

Send TAO to the address printed in step 3. Verify it arrived:

```bash
permafrost bittensor balance
```

```
Address: 5EPcXq5WbaH5QP2rGAoVd78o23Kwdi7SWJVyKWGhABqaUwLy

TAO Balance: 5.000000000 TAO (5000000000 RAO)
```

You can also check the address on [Subscan](https://bittensor.com/explorer) for an independent view.

## Step 5 — Browse subnets

```bash
permafrost bittensor subnets --max 20
```

```
Connected to: Bittensor (wss://entrypoint-finney.opentensor.ai:443)

NETUID  SYMBOL  ALPHA PRICE (TAO)
1       SN1     0.011044455
3       SN3     0.024446316
8       SN8     0.036815043   ← Vanta / Taoshi (trading signals)
19      SN19    0.014404773   ← Inference
...
```

Each row is a tradeable alpha token. Subnet 8 (Vanta/Taoshi, formerly known by its $TAOSHI ticker) and subnet 19 (Inference) are popular for trading.

## Step 6a — Validate against a local devnet (recommended for community testers)

The fastest, no-permissions path to verify Permafrost talks to a real Subtensor runtime: run the official `subtensor-localnet` Docker image. Alice comes pre-funded with 1 million TAO, so you can test the full signed-extrinsic pipeline without waiting on a faucet.

```bash
docker run -d --rm --name local_subtensor \
    -p 9944:9944 -p 9945:9945 \
    ghcr.io/opentensor/subtensor-localnet:devnet-ready
```

Wait ~5 seconds for the chain to boot, then point Permafrost at it:

```yaml
bittensor:
  rpc_url: ws://localhost:9944
  allow_submit: true
```

Run the devnet test suite:

```bash
go test -tags=devnet -v ./internal/chain/bittensor/...
```

### What devnet validates

| What you can verify on devnet | How |
|---|---|
| Chain client connects + reads chain state | `permafrost doctor`, `permafrost bittensor subnets` |
| Wallet sr25519 + SS58 produces canonical addresses | `permafrost wallet generate --chain bittensor` (cross-check with btcli/polkadot.js) |
| Real signed extrinsics submit + finalize | `TestDevnet_BalancesTransfer` (Alice→Bob 5 TAO, balance updates on-chain) |
| Sudo wrapping works | `TestDevnet_FullStakeFlow` step 0 |
| Strategy logic ticks correctly | Run an agent, watch `permafrost agent decisions <id> --tail` |

### Deploy alpha to your own devnet (full end-to-end test)

The pre-existing subnets on the devnet image (netuid 1, 2) ship with empty AMM pools. To get a fully tradeable subnet for end-to-end testing:

```bash
go test -tags=devnet -v ./internal/chain/bittensor/... -run TestDevnet_DeployAlphaAndStake
```

This test:
1. Creates a brand new subnet via `register_network` (auto-populates initial pool reserves from the registration burn)
2. Activates it via `start_call` (sets `SubtokenEnabled = true` + `FirstEmissionBlockNumber`)
3. Submits a real 50 TAO `add_stake_limit` extrinsic
4. Verifies on-chain that:
   - 50 TAO was actually deducted from the staker's coldkey
   - Alpha was minted to the hotkey via on-chain `TotalHotkeyAlpha` storage

A passing run looks like:

```
✓ register_network finalized        (subnet N created)
✓ start_call invoked → subnet activated
✓ AMM pool populated: 1 TAO yields 0.998 alpha
✓ add_stake finalized
✓ Alice TAO consumed: 50.001358611 TAO
✓ ALPHA CREDITED ON-CHAIN: TotalHotkeyAlpha[Alice, netuid N] = 62195.485 alpha
```

This is **proof on your own machine** that Permafrost's chain client correctly drives the full alpha-trading lifecycle on a real Subtensor runtime. The same path works against finney mainnet — the only difference is mainnet's subnets are already activated by their owners.

### What devnet does NOT validate

| Limitation | Why |
|---|---|
| Real subnet pricing dynamics | Newly-created devnet subnets start at `1 TAO per alpha` until trading happens. Mainnet has years of price discovery. |
| Emission-driven yield | Devnet doesn't run the full epoch/emission machinery by default — `alpha_yield`'s rebalance logic runs but actual yield doesn't accrue. |
| Liquidity depth | Mainnet pools have real reserves; devnet pools start small. |

**Bottom line:** devnet now proves end-to-end alpha trading (real signed extrinsics, real TAO deducted, real alpha minted on-chain). For realistic price discovery and emission yields, point at finney mainnet.

Tear down when done:

```bash
docker rm -f local_subtensor
```

## Step 6b — Run a strategy on testnet

For testing against real network conditions:

```yaml
bittensor:
  network: test
  allow_submit: true
```

Get free testnet TAO from the [Bittensor Discord](https://discord.gg/bittensor) `#testnet-faucet` channel. Then:

```bash
permafrost agent create \
    --strategy alpha_dca \
    --spot bittensor \
    --tick-secs 30 \
    --config-json '{"subnets":[1],"tao_per_buy":0.1,"interval_ticks":2}'

permafrost agent start <id>
```

Watch the agent's decisions:

```bash
permafrost agent decisions <id> --tail
```

After a couple of minutes you should see successful swaps in the output. Verify on [testnet Subscan](https://test.scan.opentensor.ai/) that the extrinsics landed.

## Step 7 — Enable mainnet trading

Once testnet works:

```yaml
bittensor:
  network: finney
  rpc_url: wss://entrypoint-finney.opentensor.ai:443
  allow_submit: true   # ⚠ this enables real on-chain transactions
```

`allow_submit: false` is the default safety guard rail. Until you set it to `true`, the Bittensor swap venue returns `ErrSubmitDisabled` rather than submitting extrinsics — so a misconfigured agent cannot accidentally lose funds.

Switch to a small mainnet config:

```bash
permafrost agent create \
    --strategy alpha_dca \
    --spot bittensor \
    --tick-secs 60 \
    --config-json '{"subnets":[8],"tao_per_buy":0.5,"interval_ticks":10}'
```

## What the three strategies do

| Strategy | What it does | When to use |
|---|---|---|
| [`alpha_dca`](/strategies/reference-strategies#alpha_dca) | DCA into a fixed set of subnets every N ticks | Conviction in specific subnets; passive accumulation |
| [`alpha_momentum`](/strategies/reference-strategies#alpha_momentum) | Rotate into top-K subnets by rolling momentum | Capturing trends across the subnet ecosystem |
| [`alpha_yield`](/strategies/reference-strategies#alpha_yield) | Stake into highest-stability subnets, rebalance | Conservative emissions accumulation |

All three are deliberately simple — fork them and add your own logic. See the [Strategy SAPI](/strategies/sapi).

## Operational notes

### Tick cadence

Bittensor block time is ~12 seconds. Don't tick faster than 30s; the on-chain state won't have changed and you'll waste RPC calls. 60s is comfortable.

### RPC rate limits

The default public endpoint is shared infrastructure. If you see frequent timeouts:

1. Switch to a paid provider (Dwellir).
2. Self-host a subtensor node.
3. Increase `tick-secs` to reduce request volume.

### Slippage

AMM pools on smaller subnets can have meaningful price impact for trades > 1 TAO. The strategies default to `slippage_bps: 100` (1%) which is generous; tighten via the typed config when running on liquid subnets only.

### Stake vs balance

"Buying alpha" is staking TAO. The alpha shows up as **stake** on the subnet, not as a free balance — the on-chain semantic is identical to delegating to a validator. `permafrost bittensor balance` shows your free TAO; alpha balances per subnet are read by the Bittensor swap venue when strategies query them via `Balance(asset)`.

## Troubleshooting

**`bittensor RPC` check fails in `permafrost doctor`**
- Try a different `rpc_url`. The default endpoint is sometimes overloaded.
- Verify your network can reach `wss://entrypoint-finney.opentensor.ai:443` (port 443).

**`Swap` returns `ErrSubmitDisabled`**
- Set `bittensor.allow_submit: true` in `config.yaml`. This is a deliberate safety guard rail.

**`account does not exist on-chain`**
- Your wallet has zero TAO. Send some TAO to the SS58 address before trading.

**Extrinsic submitted but never finalized**
- Network congestion. Increase `bittensor.poll_timeout_secs` (default 60s) or self-host a node.

## Next steps

- [Reference strategies](/strategies/reference-strategies) — full config tables for the three alpha strategies.
- [Strategy SAPI](/strategies/sapi) — write your own.
- [Scaffolding a new strategy](/strategies/scaffolding) — `permafrost strategy-new`.
- [CLI reference](/reference/cli) — every command.
