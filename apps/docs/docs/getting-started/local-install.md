---
sidebar_position: 3
---

# Local install (manual)

The fast path is [`make demo`](/getting-started/make-demo) -- one command, ~60 seconds. This page is the manual walk-through if you want to drive each step yourself, or to understand what `make demo` does under the hood.

## The four commands

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost

make build                                 # builds bin/permafrost + bin/permafrostd
export PATH=$PWD/bin:$PATH

permafrost init                            # generates config.yaml + keystore passphrase
set -a; source ~/.permafrost/env; set +a   # load the passphrase into your shell

make up                                    # bring up Timescale + permafrostd in Docker
permafrost doctor                          # confirm everything is wired

permafrost wallet generate --chain solana  # only if you'll trade Solana spot
permafrost vault init
permafrost agent create \
    --strategy noop \
    --perp hyperliquid \
    --alloc 1000
permafrost agent start <id>                # supervisor picks it up
```

Default mode is `paper` -- no real orders are placed until the agent is explicitly promoted to `live` (and you pass `--confirm-live` to `agent run`). See [running noop](/getting-started/running-noop) for the full smoke-test flow and [the init wizard + doctor](/getting-started/init-and-doctor) for what `init` and `doctor` do.

## What `make up` does

- Starts a TimescaleDB container on `localhost:5432`.
- Runs goose migrations.
- Builds the daemon (`go build -o bin/permafrostd ./cmd/permafrostd`).
- Starts `permafrostd` against your `config.yaml`.

The CLI (`permafrost`) talks to the daemon over REST for most commands. A handful (`db migrate`, `backtest`, `wallet`, `init`, `doctor`) work without the daemon running.

## Adding a strategy

The OSS build registers `noop`, `dca_buy`, and `market_maker_basic` out of the box (see [reference strategies](/strategies/reference-strategies)). To add your own, the easy path is the scaffolder:

```bash
permafrost strategy-new my_strategy
go build ./...
```

That generates `strategies/my_strategy/`, registers it in both binaries' `strategies.go` files, and prints the next 3 commands to type. See [`strategy-new` scaffolding](/strategies/scaffolding) for the full reference.

For a private strategy that should not be pushed publicly:

```bash
permafrost strategy-new my_secret --private
```

Routes everything to `strategies/private/my_secret/` and the gitignored `*_local.go` files (created if missing). See [private strategies](/strategies/private-strategies) for the full pattern, including backups.

## Next steps

- [The init wizard + doctor](/getting-started/init-and-doctor)
- [Configuration reference](/getting-started/configuration)
- [Running noop](/getting-started/running-noop)
- [Writing a strategy](/strategies/sapi)
