---
sidebar_position: 1
title: Run the demo (60 seconds)
---

# Run the demo

The fastest path from `git clone` to "decisions tailing in my terminal":

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
make demo
```

That's the whole quickstart. Idempotent -- re-running attaches to the
existing demo agent rather than duplicating it. SIGINT to stop the tail;
the daemon keeps running until `make demo-clean`.

## What `make demo` does

The script (`scripts/demo.sh`) is short and readable. In order:

1. **Preflight.** Confirm `docker` + `go` are present and the daemon
   isn't already up.
2. **Build binaries.** `bin/permafrost` + `bin/permafrostd` if missing
   or stale. Cheap mtime-newer-than-binary check.
3. **Compose stack.** `docker compose up -d` for Postgres + permafrostd.
   Waits up to 30s for the daemon health endpoint.
4. **Init wizard** (`permafrost init --non-interactive`) writes config
   + a fresh 64-hex-char keystore passphrase to
   `.permafrost-demo/` (gitignored, isolated from your real
   `~/.permafrost/`).
5. **Source the env file** to load the passphrase into the shell.
6. **Doctor preflight** -- bails out with a clear message if any check
   fails hard. Warnings (no inference key, etc.) don't fail the run.
7. **Recruit Pip** -- paper-mode `noop` agent, `--alloc 1000`,
   `--tick-secs 5`. Reused on re-run via name lookup so you don't get
   duplicate agents.
8. **Mark Pip runnable** and tail decisions every 2s until SIGINT.

Output is themed for the project -- see the [cast](/brand/cast).

## Sample output

```
❄  Permafrost demo -- launching expedition…
  ✓ preflight: docker + go present
  ✓ binaries built (./bin/{permafrost,permafrostd})
  ✓ stack up (Postgres + permafrostd)
  ✓ config: .permafrost-demo/config.yaml
  ✓ keystore unlocked (passphrase from .permafrost-demo/env)
Permafrost preflight ───────────────────────────────────────────
  ✓  go version                 go1.25.5
  ✓  docker                     28.0.0
  ✓  config loaded              env=dev
  ✓  database reachable         postgres://REDACTED@localhost:5432/...
  ✓  keystore passphrase env    PERMAFROST_KEYSTORE_PASSPHRASE set
  ⚠  inference providers        none configured
  ─  solana RPC                 not configured
  ✓  strategies registered      dca_buy, market_maker_basic, noop
─────────────────────────────────────────────────────────────────
  ✓ Pip is on the ice

❄  🐧  The Floe -- funding rates

[tick 1] confidence=0.00 swaps=0 orders=0 notes="noop"
[tick 2] confidence=0.00 swaps=0 orders=0 notes="noop"
…
```

## Teardown

```bash
make demo-clean
```

Stops the docker stack and removes `.permafrost-demo/`. Your real
`~/.permafrost/` (if you've gone past the demo) is untouched.

## Troubleshooting

- **Port 5432 already in use.** Another Postgres container is bound.
  `docker ps | grep 5432` to find it; stop it or move it before
  re-running.
- **Port 8080 already in use.** Another service is bound. Same fix.
- **Daemon health-wait times out.** The 30-second wait is rarely
  enough on a very slow Docker host. Re-run `make demo`; the script
  picks up where it left off without rebuilding.

## Next steps

- [The init wizard + doctor](/getting-started/init-and-doctor) -- for
  driving the same setup steps interactively without `make demo`.
- [Running noop](/getting-started/running-noop) -- manual end-to-end
  walkthrough.
- [Configuration reference](/getting-started/configuration).
