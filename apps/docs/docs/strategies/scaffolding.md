---
sidebar_position: 7
title: Scaffolding a new strategy
---

# `permafrost strategy-new`

Scaffolds a new strategy package, registers it in both binaries, and prints the next 3 commands to type. Replaces six manual steps (mkdir + write strategy.go + write Config + write tests + edit two import files + go build) with one command.

## Quick reference

```bash
# Public strategy (committed, registered in cmd/permafrost(d)/strategies.go)
permafrost strategy-new my_strategy

# Private strategy (gitignored, registered in cmd/permafrost(d)/strategies_local.go)
permafrost strategy-new my_secret --private

# Specify a template (only 'noop' shipped in v1)
permafrost strategy-new my_basis --template basis    # → "not shipped yet" error
```

## Output

```
✓ Created strategies/my_strategy/ (template=noop)
✓ Registered in cmd/permafrost/strategies.go
✓ Registered in cmd/permafrostd/strategies.go

Next steps:
  go build ./...
  permafrost agent create --strategy my_strategy --perp hyperliquid --alloc 100
  permafrost agent run <id>      # foreground iteration; SIGINT to stop
```

## What gets created

For `permafrost strategy-new my_strategy`:

```
strategies/my_strategy/
├── README.md         # template — fill in your strategy's purpose + run instructions
├── strategy.go       # Strategy struct, init(), New(), Name(), Warmup(), Decide() stubs
└── strategy_test.go  # Two starter tests: TestNew_ReturnsStrategy + TestDecide_NoOp
```

The generated `strategy.go` is the noop template with package name + `Name` constant substituted. `Decide` returns an empty `Decision` with the note "TODO implement"; replace that with your logic.

For `--private`:

```
strategies/private/my_secret/
├── README.md
├── strategy.go
└── strategy_test.go

cmd/permafrost/strategies_local.go        # created with package main + import block
cmd/permafrostd/strategies_local.go       # ditto
```

Both `*_local.go` files are gitignored. They're created with a clean header (`package main`, empty import block, generation comment) on first use.

## Validation

The command refuses three things up-front, before touching the filesystem:

1. **Bad name format.** Must be lowercase snake_case, ≤ 64 chars. Friendlier than a confusing Go package-name compile error later.
2. **Already-registered name.** Refuses to scaffold a name the registry already knows about (e.g. `noop`, `dca_buy`, `market_maker_basic`).
3. **Existing directory.** Refuses to write over `strategies/<name>/` if it already exists. No silent overwrite.

## Idempotency

`appendImport` is idempotent: re-running with the same name (after the first attempt failed mid-way) does not duplicate the import line. The directory check and the registry check stay strict — you'll get a clear error rather than a half-finished tree.

## Templates

| Template | Status | What it produces |
|---|---|---|
| `noop` | ✓ shipped | Minimal stub; one-line `Decide` returns `Decision{}` |
| `basis` | planned | Paired SwapIntent + OrderIntent skeleton |
| `maker` | planned | Perp-only OrderIntent skeleton with optional LLM veto |
| `dca` | planned | SwapIntent-only DCA skeleton |

For now, `dca_buy`, `market_maker_basic`, and (for maintainers) `funding_arb_basic` are the working examples to copy from while the templates land — see [reference strategies](/strategies/reference-strategies).

## Top-level command name

The cobra command is `strategy-new` (not `strategy new`). Cobra accepts the alias `new`, so both work:

```bash
permafrost strategy-new my_x
permafrost new          my_x
```

This is purely an internal-organisation quirk of how the command was wired into the cobra tree; users can use whichever they prefer.

## Where it lives

- `internal/cli/strategy_new.go` — the command body and `appendImport` helper.
- `internal/cli/templates/strategy/noop/*.tmpl` — the embedded template files (`go:embed`).
- `internal/cli/strategy_new_test.go` — 7 tests covering name validation, scaffolding, `--private`, registry-collision refusal, existing-dir refusal, unshipped-template refusal, `appendImport` idempotency.

## Next steps

- [The Strategy SAPI](/strategies/sapi)
- [Reference strategies](/strategies/reference-strategies) — copy from `dca_buy` or `market_maker_basic` for the `basis` / `maker` / `dca` shapes
- [Private strategies](/strategies/private-strategies) — backups, sharing across machines, leak prevention
