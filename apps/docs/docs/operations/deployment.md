---
sidebar_position: 1
---

# Deployment

v1 of Permafrost is local-first. The recommended deploy is `docker-compose up` on a host you control (a laptop, a small VPS, a colo'd server). Two containers: TimescaleDB and `permafrostd`.

## Local (laptop / dev)

```bash
git clone https://github.com/teslashibe/permafrost.git
cd permafrost
cp config.example.yaml config.yaml      # edit
make up                                  # docker-compose up + migrate + start
```

Logs stream to stdout (or to JSON if `logging.format: json` is set). The CLI talks to the daemon over `localhost`.

## Server / VPS

The same docker-compose works on a server. Notes:

- **TimescaleDB** is configured with a small footprint by default. For production, mount a real volume and tune `shared_buffers` / `work_mem`.
- **Persistence.** Mount `/var/lib/postgresql/data` as a named volume (compose does this) and add it to your snapshot/backup schedule.
- **Reverse proxy.** The Fiber API listens on `:8080` by default. Front it with caddy / nginx + TLS if exposed publicly. Most operators don't expose it — they SSH in and use the CLI directly.
- **Process supervision.** Systemd is the easiest non-Docker path: `ExecStart=/usr/local/bin/permafrostd`, set `KEYSTORE_PASSPHRASE` via `EnvironmentFile`, restart on failure.

## Building a custom binary

Anyone running real strategies will rebuild `permafrostd` to include their strategies:

```bash
go build -o /usr/local/bin/permafrostd ./cmd/permafrostd
```

Two files determine which strategies are linked into the binary:

- `cmd/permafrostd/strategies.go` — committed, blank-imports community strategies.
- `cmd/permafrostd/strategies_local.go` — gitignored, blank-imports your private strategies.

Rebuild whenever you add or remove a strategy. The binary is statically linked Go — easy to scp to a server and run.

## Docker image

The compose flow builds an image from the included `Dockerfile`. To build standalone:

```bash
docker build -t permafrost:local .
```

Mount your `config.yaml` and keystore at runtime:

```bash
docker run --rm -it \
    -v $(pwd)/config.yaml:/etc/permafrost/config.yaml:ro \
    -v $(pwd)/keystore.json:/etc/permafrost/keystore.json:ro \
    -e PERMAFROST_CONFIG=/etc/permafrost/config.yaml \
    -e KEYSTORE_PASSPHRASE \
    permafrost:local
```

For private strategies in a container build, the Dockerfile builds with whatever's in `cmd/permafrostd/`, so add your `strategies_local.go` and the `strategies/private/` directory to the build context.

## Migrations

Schema migrations are managed with goose. Run them manually:

```bash
permafrost db migrate up
permafrost db migrate status
```

`make up` runs them automatically on boot.

## Backups

The two things to back up:

1. **The database** — `pg_dump` the entire `permafrost` database. Includes agent definitions, decisions, positions, PnL history, vault state.
2. **The keystore** — encrypted, but lose it (or the passphrase) and the funds are gone.

Plus, if you have private strategies, [back those up](/strategies/private-strategies#backups-you-have-to-think-about-this).

## Next steps

- [Keystore + backups](/operations/keystore-and-backups)
- [Killswitch tuning](/operations/killswitch-tuning)
