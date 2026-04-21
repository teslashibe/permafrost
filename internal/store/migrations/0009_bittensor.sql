-- +goose Up
-- Bittensor subnet pool state time-series for backtesting and charting.

CREATE TABLE subnet_pool_snapshots (
    time            TIMESTAMPTZ NOT NULL,
    netuid          SMALLINT    NOT NULL,
    tao_reserve     NUMERIC     NOT NULL,
    alpha_reserve   NUMERIC     NOT NULL,
    alpha_price_tao NUMERIC     NOT NULL
);

SELECT create_hypertable('subnet_pool_snapshots', 'time');

CREATE INDEX idx_subnet_pool_netuid ON subnet_pool_snapshots (netuid, time DESC);

-- Extend swaps table so Bittensor alpha swaps record which subnet they target.
ALTER TABLE swaps ADD COLUMN IF NOT EXISTS netuid SMALLINT;

-- +goose Down
DROP TABLE IF EXISTS subnet_pool_snapshots;
ALTER TABLE swaps DROP COLUMN IF EXISTS netuid;
