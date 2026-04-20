---
sidebar_position: 2
title: The init wizard + doctor
---

# The init wizard + doctor

Two commands that take a fresh clone to a working configuration in
under a minute, with a clear preflight check that surfaces problems
before they bite you on the first decision tick.

## `permafrost init`

Walks an operator through:

1. **Captain's callsign** (display only; defaults to "Captain Pole" in
   the arctic theme).
2. **Config path** (default `./config.yaml`).
3. **Keystore path** (default `~/.permafrost/keystore.json`).
4. **Env-file path** for the generated keystore passphrase
   (default `~/.permafrost/env`).
5. **Optional inference provider** (openrouter / openai / groq /
   ollama / skip).
6. **EVM RPC nudge** -- points you at [chainlist.org](https://chainlist.org/)
   when you're ready to trade EVM spot legs.

End state:

- A starter `config.yaml` (copied from `config.example.yaml` if
  available; minimal stub otherwise).
- A 64-hex-character keystore passphrase, written 0600 to a sourceable
  env file. The wizard prints it once on stdout -- back it up.
- A clear next-steps block telling you exactly the next 5 commands to
  type.

The wizard is **idempotent**: re-running detects existing files and
keeps them rather than overwriting.

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `--non-interactive` | false | Skip prompts; accept defaults silently. CI / scripts. |
| `--theme {arctic,plain}` | arctic | Vocabulary theme. `plain` strips arctic flavour text. |
| `--config-out <path>` | `./config.yaml` | Where to write config.yaml |
| `--keystore-out <path>` | `~/.permafrost/keystore.json` | Where to put the keystore |
| `--env-out <path>` | `~/.permafrost/env` | Where to write the passphrase env file |

### Sourcing the env file

The wizard does **not** auto-source the env file or modify your shell
profile. To load the passphrase into your current shell:

```bash
set -a; source ~/.permafrost/env; set +a
```

Or in CI / Docker, mount it as an env file (`docker run --env-file
~/.permafrost/env …`).

## `permafrost doctor`

A preflight checker that verifies every system Permafrost depends on
before you start an agent.

```bash
permafrost doctor
```

```
Permafrost preflight ───────────────────────────────────────────
  ✓  go version                 go1.25.5
  ✓  docker                     28.0.0
  ✓  config loaded              env=dev
  ✓  database reachable         postgres://REDACTED@localhost:5432/permafrost?sslmode=disable
  ✓  keystore passphrase env    PERMAFROST_KEYSTORE_PASSPHRASE set
  ⚠  keystore file              ~/.permafrost/keystore.json (not yet created -- run `permafrost wallet generate` or `wallet import`)
  ⚠  inference: groq            base_url=https://api.groq.com/openai/v1 but GROQ_API_KEY not set (provider may reject auth)
  ✓  inference: ollama          base_url=http://localhost:11434/v1
  ✓  solana RPC                 https://api.mainnet-beta.solana.com (slot 414335213)
  ✓  evm RPC: base              https://mainnet.base.org (block 0x2ad7400)
  ✓  evm RPC: ethereum          https://ethereum-rpc.publicnode.com (block 0x17c3237)
  ✓  strategies registered      dca_buy, market_maker_basic, noop
─────────────────────────────────────────────────────────────────
11 ok, 4 warning(s), 1 error(s). Fix errors before `permafrost agent start`.
```

### What it checks

| Category | Check | Failure means |
|---|---|---|
| Toolchain | `go version` | Go binary missing on PATH |
| Toolchain | `docker --version` | Docker daemon not reachable |
| Config | config loaded | `config.yaml` missing or unparseable |
| Data | database reachable | Postgres TCP + SQL ping |
| Keystore | `PERMAFROST_KEYSTORE_PASSPHRASE` set | Won't decrypt |
| Keystore | keystore file present | `wallet import` not run yet |
| Inference | per-provider base_url + API key env | Provider will reject auth |
| Chain | Solana RPC `getSlot` round-trip | RPC unreachable / rate-limited |
| Chain | EVM RPC `eth_blockNumber` per chain | Same per chain |
| Strategies | registered count | No strategies linked into binary |

### Severity model

- **`✓` (green)** -- passing.
- **`⚠` (yellow)** -- degraded but won't block agent start. Common cases:
  rate-limited RPC, missing optional inference key, keystore not yet
  populated.
- **`✗` (red)** -- hard failure. Doctor exits 1.
- **`─` (grey)** -- intentionally skipped (e.g. Solana RPC check when
  no Solana RPC is configured).

### Flags

```bash
permafrost doctor --verbose    # show underlying error details for failures
```

### Implementation notes

Doctor uses an explicit 5-second HTTP timeout per check (TLS-handshake
hang protection). The Solana check uses `getSlot` rather than `getHealth`
- commercial providers (Helius, Triton) restrict `getHealth`. RPC URLs
are redacted (API keys stripped) before being printed.

## Where they fit in your workflow

```
                                    one-time
                                  ┌───────────┐
                                  │  init     │
                                  └─────┬─────┘
                                        │
                                        ▼
                       ┌────────────────────────────────┐
                       │  source ~/.permafrost/env      │
                       │  (load keystore passphrase)    │
                       └─────┬──────────────────────────┘
                             │
                             ▼
                       ┌──────────┐         ┌──────────────────┐
                       │  doctor  │  ──────►│  fix any errors  │
                       └─────┬────┘         └──────────────────┘
                             │
                             ▼
                       ┌──────────────────┐
                       │  agent start …   │
                       └──────────────────┘
```

## Next steps

- [Running noop](/getting-started/running-noop)
- [Configuration reference](/getting-started/configuration)
- [Operations: keystore + backups](/operations/keystore-and-backups)
