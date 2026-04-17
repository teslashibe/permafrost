-- +goose Up
-- +goose StatementBegin

-- Vaults — single-operator MVP normally has exactly one row.
CREATE TABLE IF NOT EXISTS vaults (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    asset       text NOT NULL,                       -- accounting unit, USDC for v1
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Deposits / Withdrawals are append-only ledger entries.
CREATE TABLE IF NOT EXISTS deposits (
    id          bigserial PRIMARY KEY,
    vault_id    bigint NOT NULL REFERENCES vaults(id),
    amount      numeric(38,9) NOT NULL CHECK (amount > 0),
    source      text NOT NULL DEFAULT 'manual',
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS withdrawals (
    id          bigserial PRIMARY KEY,
    vault_id    bigint NOT NULL REFERENCES vaults(id),
    amount      numeric(38,9) NOT NULL CHECK (amount > 0),
    destination text NOT NULL DEFAULT 'manual',
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Lockups freeze a portion of vault capital until unlock_at.
CREATE TABLE IF NOT EXISTS lockups (
    id          bigserial PRIMARY KEY,
    vault_id    bigint NOT NULL REFERENCES vaults(id),
    amount      numeric(38,9) NOT NULL CHECK (amount > 0),
    unlock_at   timestamptz NOT NULL,
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS lockups_vault_unlock_idx ON lockups (vault_id, unlock_at);

-- NAV snapshots — Timescale hypertable, partitioned on time.
CREATE TABLE IF NOT EXISTS nav_snapshots (
    time             timestamptz NOT NULL,
    vault_id         bigint      NOT NULL REFERENCES vaults(id),
    nav              numeric(38,9) NOT NULL,
    cash             numeric(38,9) NOT NULL,
    positions_value  numeric(38,9) NOT NULL,
    high_water_mark  numeric(38,9) NOT NULL,
    deposit_total    numeric(38,9) NOT NULL,
    withdrawal_total numeric(38,9) NOT NULL,
    PRIMARY KEY (vault_id, time)
);

SELECT create_hypertable('nav_snapshots', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS nav_snapshots_vault_time_desc_idx
    ON nav_snapshots (vault_id, time DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS nav_snapshots;
DROP TABLE IF EXISTS lockups;
DROP TABLE IF EXISTS withdrawals;
DROP TABLE IF EXISTS deposits;
DROP TABLE IF EXISTS vaults;
-- +goose StatementEnd
