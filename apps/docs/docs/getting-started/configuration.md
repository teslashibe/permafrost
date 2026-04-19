---
sidebar_position: 4
---

# Configuration

Permafrost is configured via a YAML file (`config.yaml`) loaded by [viper](https://github.com/spf13/viper). Environment variables can override any field. The shipped `config.example.yaml` is the canonical reference; this page covers the important blocks.

## Database

```yaml
database:
  url: postgres://permafrost:permafrost@localhost:5432/permafrost?sslmode=disable
  max_open_conns: 25
  max_idle_conns: 5
```

Permafrost requires TimescaleDB-flavored Postgres. Plain Postgres works for the schema but you lose hypertable-backed time-series queries.

## Logging

```yaml
logging:
  level: info        # debug | info | warn | error
  format: text       # text | json (json recommended in prod)
```

## Hyperliquid

```yaml
hyperliquid:
  network: mainnet   # mainnet | testnet
```

The Hyperliquid signer comes from the keystore, not the config file.

## Solana

```yaml
solana:
  rpc_url: https://mainnet.helius-rpc.com/?api-key=...
  jupiter_base_url: https://quote-api.jup.ag/v6
  jupiter_api_key: ""        # optional, public Jupiter is unauthenticated
  submit_mode: jito          # rpc | jito
  jito_bundle_url: https://mainnet.block-engine.jito.wtf/api/v1/bundles
  priority_fee_micro_lamports: 1000
  confirmation_timeout_secs: 30
```

Live mode for any agent with a Solana spot leg requires both `rpc_url` and a Solana signer in the keystore.

## EVM (per-chain)

```yaml
evm:
  chains:
    base:
      rpc_url: https://mainnet.base.org
      one_inch_api_key: ""               # or one_inch_api_key_env: ONEINCH_API_KEY
      one_inch_base_url: https://api.1inch.dev/swap/v6.0
      default_slippage_bps: 50
      confirmation_timeout_secs: 60
    ethereum:
      rpc_url: ...
      one_inch_api_key_env: ONEINCH_API_KEY
```

Configured chains downgrade to paper-spot bookkeeping if either the API key or the chain signer is missing — the runtime logs a warning rather than failing.

## Inference

```yaml
inference:
  default: openrouter
  providers:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key_env: OPENROUTER_API_KEY
      request_timeout_secs: 60
    ollama:
      base_url: http://localhost:11434/v1
      api_key: ""
```

Multiple providers can coexist; agents pick by name in the `--inference provider:model` flag at agent-create time.

## Risk

Per-agent risk lives on the agent record (set via `agent create --config-json '{"risk":{...}}'`). The framework also has installation-wide killswitch tuning — see [killswitch tuning](/operations/killswitch-tuning).

## Environment overrides

Any nested config field is overridable via environment variable using `_` separators, e.g. `PERMAFROST_DATABASE_URL=postgres://...` overrides `database.url`. This is the recommended way to inject secrets in containerised deployments.

A few first-class env variables:

- `PERMAFROST_CONFIG` — path to the config file (default `./config.yaml`).
- `PERMAFROST_HYPERLIQUID_NETWORK` — daemon-wide override of every agent's stored network. Useful for "panic switch all agents to testnet" scenarios.
- `PERMAFROST_KEYSTORE_PASSPHRASE` — passphrase to decrypt the keystore at startup.

## Next steps

- [Running noop](/getting-started/running-noop)
- [Killswitch tuning](/operations/killswitch-tuning)
- [Keystore + backups](/operations/keystore-and-backups)
