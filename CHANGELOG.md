# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Reserved for upcoming changes.

## [0.1.0] - 2026-04-19

### Added
- Stable Strategy SAPI with first-class `strategies/` extension model.
- Hyperliquid perp venue integration.
- Solana Jupiter swap venue with Jito submission mode support.
- EVM swap venue integration via 1inch v6.
- Self-custodied encrypted keystore and wallet import/generate tooling.
- Agent runtime with persistence, decision ledger, and reconciliation hooks.
- Risk policy layer and killswitch implementation.
- Backtest harness.
- Operator CLI workflows (`init`, `doctor`, `strategy-new`, `agent`, `vault`, `pnl`, `reconcile`).
- Trading Desk UI (`apps/desk`) with arctic sprite cast.
- Docs site (`apps/docs`) with architecture, operations, and strategy guides.
- Multi-arch Docker image pipeline to GHCR.

### Changed
- README and docs aligned to current implementation details and known limitations.

### Security
- `permafrost init` no longer prints generated keystore passphrase to stdout.
- Mode parsing hardened to reject invalid `paper|live` values at input boundaries.
- Added explicit operator killswitch command: `permafrost agent kill <id>`.
- Swap idempotency persistence aligned to deterministic `client_id` keying.

[unreleased]: https://github.com/teslashibe/permafrost/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/teslashibe/permafrost/releases/tag/v0.1.0
