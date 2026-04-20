# Contributing to Permafrost

First: thanks for considering a contribution. Permafrost is a
self-custodied trading framework, so the bar for changes that touch
the keystore, signing, the killswitch, or the agent runtime is
deliberately high. The bar for new strategies, docs, sprites, and
quality-of-life polish is lower — those land quickly.

## TL;DR

1. **Bugs:** open an issue with `permafrost version`, OS, and a repro.
   For security-relevant bugs, **don't** file a public issue — see
   [`SECURITY.md`](SECURITY.md).
2. **Features / refactors:** open an issue first to discuss scope,
   then a PR.
3. **Strategies:** read the
   [strategy authors guide](https://teslashibe.github.io/permafrost/strategies/sapi)
   first. New strategies usually don't need a discussion issue.
4. **Docs / typos / sprites:** PR directly.

## Development setup

Prerequisites: Go 1.25+, Docker, Node 20+ (for `apps/desk` and
`apps/docs`).

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
make demo                        # one-shot: build + Postgres + Pip the noop agent
```

Or step-by-step:

```bash
make build                       # bin/permafrost + bin/permafrostd
make up                          # Postgres + permafrostd via docker-compose
permafrost init --non-interactive
permafrost doctor
permafrost agent create --strategy noop --perp hyperliquid --alloc 100
permafrost agent run <id>
```

For the Trading Desk UI:

```bash
cd apps/desk && npm install && npm run dev    # http://127.0.0.1:5173
```

## What we expect from a PR

Before opening:

- [ ] `make test` passes locally (`go test ./... -race -count=1`)
- [ ] `make lint` passes (`go vet ./...`; `golangci-lint` if you have
      it installed locally)
- [ ] If you touched `apps/desk`: `npm run typecheck && npm run build`
      pass
- [ ] If you touched `apps/docs`: `npm run build` passes (the docs
      build has `onBrokenLinks: 'throw'`; broken links fail CI)
- [ ] Commit messages follow the repo style: imperative subject,
      one-line summary, then a body that explains *why* the change
      exists (not just *what*)

In the PR description:

- Link the issue you're closing (or the discussion you're addressing).
- Note any user-visible behaviour change.
- Call out anything you couldn't test (e.g. "I don't have a
  Hyperliquid testnet wallet, so the EIP-712 signing path is exercised
  only by the existing unit tests").

## What we look for in review

- **Correctness first.** This is trading software; correctness beats
  cleverness. If a change makes a trade-off, say so in the commit
  message and the PR description.
- **No silent expansion of scope.** A bug-fix PR should fix the bug
  and not refactor the surrounding 200 lines unless that's clearly
  necessary. If it is, split the refactor out.
- **Tests for anything that can regress.** New strategies need at
  least one happy-path test against synthetic funding rates. Anything
  that touches signing, the killswitch, or the keystore needs
  unit-level coverage of the failure modes.
- **No new external dependencies without discussion.** Both Go and
  npm. If you need a library, mention it in the issue first.

## Strategies

Strategies are first-class extensions, not patches:

- One subdirectory under `strategies/<name>/`
- Calls `strategy.Register` in `init()`
- Enabled by adding one blank-import line to **both**
  `cmd/permafrost/strategies.go` (for `strategy backtest`) and
  `cmd/permafrostd/strategies.go` (for the daemon supervisor).

Use `permafrost strategy-new <name>` to scaffold the directory and
both import lines automatically.

For private / proprietary strategies, use
`permafrost strategy-new <name> --private` — it lands the package
under `strategies/private/` and registers it via `*_local.go` files,
both of which are gitignored. See the
[private strategies guide](https://teslashibe.github.io/permafrost/strategies/private-strategies).

## Reporting security issues

Do not open a public GitHub issue. See [`SECURITY.md`](SECURITY.md).

## Code of conduct

This project follows the
[Contributor Covenant 2.1](CODE_OF_CONDUCT.md). By participating you
agree to abide by it.

## License

By contributing, you agree that your contributions will be licensed
under the [MIT License](LICENSE).
