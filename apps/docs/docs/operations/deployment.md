---
sidebar_position: 1
---

# Deployment

v1 of Permafrost is local-first. Three flavours of deploy, in increasing order of ceremony:

1. **`make demo`** on a laptop — one command, ephemeral. See [run the demo](/getting-started/make-demo).
2. **`make up`** on a host you control (laptop / VPS / colo) — persistent compose stack.
3. **GHCR-published image** pulled into your own orchestration (compose, k3s, ECS).

## Local (laptop / dev)

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
permafrost init                    # generates config + keystore passphrase
set -a; source ~/.permafrost/env; set +a
make up                            # docker-compose up + migrate + start
```

Logs stream to stdout (or to JSON if `logging.format: json` is set). The CLI talks to the daemon over `localhost:8080`.

## Server / VPS (compose)

The same `docker-compose.yml` works on a server. Notes:

- **TimescaleDB** is configured with a small footprint by default. For production, mount a real volume and tune `shared_buffers` / `work_mem`.
- **Persistence.** Mount `/var/lib/postgresql/data` as a named volume (compose does this) and add it to your snapshot/backup schedule.
- **Reverse proxy.** The Fiber API listens on `:8080` by default. Front it with caddy / nginx + TLS if exposed publicly. Most operators don't expose it — they SSH in and use the CLI directly.
- **Process supervision.** The compose `restart: unless-stopped` policy handles crash recovery. Systemd is the easiest non-Docker path: `ExecStart=/usr/local/bin/permafrostd`, set `PERMAFROST_KEYSTORE_PASSPHRASE` via `EnvironmentFile`, restart on failure.

## The `cli` compose service

For one-off CLI commands without installing Go on the host:

```bash
docker compose -f deploy/compose/docker-compose.yml \
  --profile cli run --rm cli agent list
```

Notes on the `cli` service:

- Doesn't auto-start with `make up` (gated behind `profiles: [cli]`).
- Wired to talk to the compose Postgres on the internal network.
- Operator's keystore + config are mounted from `${PERMAFROST_CONFIG_DIR:-./.permafrost-cli}` read-only. Override with the env var.
- `PERMAFROST_KEYSTORE_PASSPHRASE` is plumbed through from the host.
- Image source is overridable via `PERMAFROST_IMAGE` so you can pin a published GHCR tag instead of building locally:

```bash
PERMAFROST_IMAGE=ghcr.io/teslashibe/permafrost:v0.1.0 \
  docker compose -f deploy/compose/docker-compose.yml --profile cli run --rm cli agent list
```

## GHCR image (multi-arch)

Tagged releases publish a multi-arch image to `ghcr.io/teslashibe/permafrost`:

```bash
docker pull ghcr.io/teslashibe/permafrost:latest

docker run --rm --entrypoint permafrost ghcr.io/teslashibe/permafrost:latest version
docker run --rm --entrypoint permafrost ghcr.io/teslashibe/permafrost:latest doctor
```

Built for `linux/amd64` + `linux/arm64`. Tags: the version itself + `:latest` for non-pre-release versions (anything without a hyphen → `:latest`; pre-releases like `v0.1.0-rc1` get only the version tag).

The image is **dual-binary** — both `permafrost` (CLI) and `permafrostd` (daemon) live at `/usr/local/bin/`. Override the entrypoint to pick:

```bash
docker run --rm --entrypoint permafrostd ghcr.io/teslashibe/permafrost:latest \
    --config /etc/permafrost/config.yaml
```

The release workflow is in `.github/workflows/release.yml`. Triggers on `v*` tag pushes and `workflow_dispatch`. Uses the built-in `GITHUB_TOKEN` for GHCR auth — no extra secrets to configure.

## Building a custom binary

Anyone running real strategies will rebuild `permafrostd` to include their strategies:

```bash
go build -o /usr/local/bin/permafrostd ./cmd/permafrostd
```

Two files determine which strategies are linked into the binary:

- `cmd/permafrostd/strategies.go` — committed, blank-imports community + reference strategies (`noop`, `dca_buy`, `market_maker_basic`).
- `cmd/permafrostd/strategies_local.go` — gitignored, blank-imports your private strategies.

Rebuild whenever you add or remove a strategy. Or use `permafrost strategy-new <name>` to scaffold + register in one shot — see [scaffolding](/strategies/scaffolding).

## Building a custom Docker image (with private strategies)

The shipped `Dockerfile` builds with whatever's in `cmd/permafrostd/`, so add your `strategies_local.go` and `strategies/private/` to the build context:

```bash
docker build -t my-registry/permafrost:0.1.0 -f deploy/docker/Dockerfile .
docker push my-registry/permafrost:0.1.0
```

Mount your `config.yaml` and keystore at runtime:

```bash
docker run --rm -it \
    -v $(pwd)/config.yaml:/etc/permafrost/config.yaml:ro \
    -v $(pwd)/keystore.json:/etc/permafrost/keystore.json:ro \
    -e PERMAFROST_CONFIG=/etc/permafrost/config.yaml \
    -e PERMAFROST_KEYSTORE_PASSPHRASE \
    my-registry/permafrost:0.1.0
```

## Migrations

Schema migrations are managed with goose. `make up` runs them automatically on boot. Manual escape hatch:

```bash
permafrost db migrate up
permafrost db migrate status
```

## Backups

The two things to back up:

1. **The database** — `pg_dump` the entire `permafrost` database. Includes agent definitions, decisions, positions, PnL history, vault state.
2. **The keystore** — encrypted, but lose it (or the passphrase) and the funds are gone.

Plus, if you have private strategies, [back those up](/strategies/private-strategies#backups-you-have-to-think-about-this).

## Next steps

- [Trading Desk UI](/operations/trading-desk-ui)
- [Keystore + backups](/operations/keystore-and-backups)
- [Killswitch tuning](/operations/killswitch-tuning)
