---
sidebar_position: 2
---

# CLI reference

The `permafrost` CLI talks to a running `permafrostd` daemon over REST for most commands. A handful (`db migrate`, `backtest`, `wallet`) work without the daemon.

Run `permafrost --help` or `permafrost <subcommand> --help` for the authoritative reference; this page is the index.

## Top-level commands

```
permafrost wallet      import | show | balances
permafrost vault       init | deposit | status | nav
permafrost agent       create | start | stop | list | status | decisions | positions | pnl | tick | killswitch-events | set-mode | set-config
permafrost strategy    list | backtest <name>
permafrost inference   test
permafrost swap        quote
permafrost risk        show
permafrost db          migrate | status
permafrost serve
```

## Wallet

```bash
permafrost wallet import --chain solana --from ~/.config/solana/id.json
permafrost wallet import --chain hyperliquid --hex 0x...
permafrost wallet import --chain ethereum --hex 0x...
permafrost wallet show
permafrost wallet balances --chain solana
```

See [keystore + backups](/operations/keystore-and-backups).

## Vault

```bash
permafrost vault init                    # initialize the vault row
permafrost vault deposit --amount 5000   # record a deposit (off-chain accounting)
permafrost vault status
permafrost vault nav
```

## Agent

```bash
permafrost agent create \
    --strategy <name> \
    --perp hyperliquid \
    [--spot solana] \
    [--inference openrouter:claude-sonnet-4.5] \
    [--mode paper|live] \
    [--network mainnet|testnet] \
    [--universe wif,bonk,popcat] \
    [--alloc 5000] \
    [--tick-secs 60] \
    [--config-json '{"key":"value"}']

permafrost agent start <id>
permafrost agent stop <id>            # graceful (re-startable)
permafrost agent stop --all --reason "<reason>"   # killswitch

permafrost agent list
permafrost agent status <id>
permafrost agent decisions <id> [--follow] [--limit N] [--with-prompts]
permafrost agent positions <id>
permafrost agent pnl <id>
permafrost agent tick <id> [--funding WIF=80,BONK=15]   # one-shot decision tick (paper-only)

permafrost agent set-mode <id> --status running|halted
permafrost agent set-config <id> --config-json '{...}'
permafrost agent killswitch-events [--limit N]
```

Live mode requires `--confirm-live` and signers in the keystore for every venue the agent touches.

## Strategy

```bash
permafrost strategy list
permafrost strategy backtest <name> --csv funding.csv [--config-json '{...}']
```

Backtest loads a CSV of funding ticks (`time,symbol,rate,interval_seconds`) and runs them through the named strategy via the registry — same construction path as a live agent. Per-strategy config goes via `--config-json`.

## Inference

```bash
permafrost inference test --provider openrouter --model anthropic/claude-sonnet-4.5
```

## Swap

```bash
permafrost swap quote --chain solana --in USDC --out WIF --amount 100
```

Quotes only — does not broadcast. Useful for sanity-checking what an `OrderIntent` would experience.

## Risk

```bash
permafrost risk show <agent-id>      # current limits + breaker status
```

## DB

```bash
permafrost db migrate up
permafrost db migrate status
```

Migrations are run automatically by `make up`; this is the manual escape hatch.

## Serve

```bash
permafrost serve
```

Equivalent to running `permafrostd` directly. Exists so operators can run the same binary for both the CLI and the daemon if they want.

## Environment variables

- `PERMAFROST_CONFIG` — config file path (default `./config.yaml`).
- `PERMAFROST_HYPERLIQUID_NETWORK` — daemon-wide override of every agent's stored network.
- `KEYSTORE_PASSPHRASE` — passphrase to decrypt the keystore.
