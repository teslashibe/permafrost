---
sidebar_position: 2
---

# Local install

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
cp config.example.yaml config.yaml      # edit with your keys & RPC URLs
make up                                  # bring up Timescale + permafrostd
```

That gives you a running daemon with `noop` registered. To do anything useful, register a wallet, initialize the vault, and create an agent:

```bash
permafrost wallet import --chain solana --from <keypair.json>
permafrost vault init
permafrost agent create \
    --strategy noop \
    --perp hyperliquid \
    --alloc 5000
permafrost agent start <id>
```

Default mode is `paper` — no real orders are placed until the agent is explicitly promoted to `live` (and you pass `--confirm-live`). See [running noop](/getting-started/running-noop) for the full smoke-test flow.

## What `make up` does

- Starts a TimescaleDB container on `localhost:5432`.
- Runs goose migrations.
- Builds the daemon (`go build -o bin/permafrostd ./cmd/permafrostd`).
- Starts `permafrostd` against your `config.yaml`.

The CLI (`permafrost`) talks to the daemon over REST for most commands. A handful (`db migrate`, `backtest`, `wallet`) work without the daemon running.

## Adding a strategy

The OSS build only registers `noop`. Every other strategy is added by the operator — see [writing a strategy](/strategies/sapi). The short version:

```bash
mkdir -p strategies/my_strategy
# write strategies/my_strategy/strategy.go that registers in init()
echo 'import _ "github.com/teslashibe/permafrost/strategies/my_strategy"' \
  >> cmd/permafrostd/strategies.go
go build -o bin/permafrostd ./cmd/permafrostd
```

For a strategy you do not want pushed publicly, put it under `strategies/private/<name>/` (gitignored) and add the import line to `cmd/permafrostd/strategies_local.go` (also gitignored). See [private strategies](/strategies/private-strategies) for the full pattern, including backups.

## Next steps

- [Configuration reference](/getting-started/configuration)
- [Running noop](/getting-started/running-noop)
- [Writing a strategy](/strategies/sapi)
