---
sidebar_position: 2
---

# Keystore and backups

Permafrost holds private keys directly. This page covers what's stored, where, how it's protected, and how to back it up so you don't lose the funds.

## What's in the keystore

`internal/wallet` manages an encrypted JSON keystore (default path `~/.permafrost/keystore.json`; override via `wallet.keystore_path` in `config.yaml`). It contains, per chain:

- **Solana** — an ed25519 private key.
- **Hyperliquid** — an ECDSA private key used to sign Hyperliquid orders. (HL itself has no on-chain wallet to fund; this key authenticates API requests.)
- **EVM chains** — an ECDSA private key (one for all EVM chains; addresses are derived per chain).

`internal/wallet` is the **only** package that touches private key bytes. Everything else uses a `Signer` abstraction. If a key ever leaks via a log or panic, that's a bug.

## Encryption

The keystore JSON is encrypted with a passphrase loaded from the `PERMAFROST_KEYSTORE_PASSPHRASE` environment variable at daemon startup. The passphrase is never written to disk by Permafrost.

In Docker / systemd deployments, supply `PERMAFROST_KEYSTORE_PASSPHRASE` via:

- A `.env` file outside the container, mounted read-only.
- A secrets manager (Doppler, 1Password CLI, Vault, AWS Secrets Manager).
- For tests / paper-mode: a one-line shell export. **Never** for live mode.

## Importing keys

```bash
# Solana — from a Solana CLI keypair file
permafrost wallet import --chain solana --from ~/.config/solana/id.json

# EVM — from a hex-encoded private key
permafrost wallet import --chain ethereum --hex 0x...

# Hyperliquid — same flow, hex private key
permafrost wallet import --chain hyperliquid --hex 0x...
```

The CLI prompts for `PERMAFROST_KEYSTORE_PASSPHRASE` if not set in the env. Repeated imports for the same chain replace the existing entry (no key rotation history is kept on disk).

## Listing what you have

```bash
permafrost wallet show
# chain         address                                       has_signer
# solana        Au7yYMK5...                                   yes
# hyperliquid   0xabc1...                                     yes
# ethereum      0xdef2...                                     yes

permafrost wallet balances --chain solana
```

## Backups

The keystore + the passphrase together are the keys to the funds. **Lose either one and the funds are unrecoverable.** No exchange support, no one to call.

Minimum:

1. **Encrypted keystore** — backed up to two physically separate locations (e.g. Time Machine + a USB key in a safe).
2. **Passphrase** — written down, stored separately from the keystore. A password manager works; a piece of paper in a safe works. Memorising it is not a backup.

Better:

3. **Test the restore.** On a clean machine, install Permafrost, restore the keystore, set the passphrase, run `permafrost wallet show`. If all three addresses come back correct, the backup works.

Reminder: if you also have private strategies under `strategies/private/`, those need [their own backup discipline](/strategies/private-strategies#backups-you-have-to-think-about-this).

## Database backups

The TimescaleDB database holds the entire history of agents, decisions, orders, positions, fills, PnL, vault state. It does **not** hold private keys, but losing it loses your audit trail and your reconciliation baseline (the framework would re-derive positions from venue truth on next start, but the historical PnL graph would be empty).

```bash
pg_dump --no-owner -Fc permafrost > backup-$(date +%F).dump
```

Restore:

```bash
pg_restore --clean --if-exists -d permafrost backup-2026-04-19.dump
```

Schedule a daily `pg_dump`; rotate weekly. Keep the last 30 days off-host.

## Next steps

- [Killswitch tuning](/operations/killswitch-tuning)
- [Private strategies → Backups](/strategies/private-strategies#backups-you-have-to-think-about-this)
