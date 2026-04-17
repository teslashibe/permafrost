-- +goose Up
-- +goose StatementBegin

-- Assets — synced from internal/assets/registry.yaml on startup or via
-- `permafrost assets sync`. Symbol is the canonical name (e.g. "WIF").
CREATE TABLE IF NOT EXISTS assets (
    symbol            text PRIMARY KEY,
    perp_venue        text,                         -- NULL if quote-only (e.g. USDC)
    perp_symbol       text,
    spot_chain        text NOT NULL,
    spot_mint         text NOT NULL,
    spot_decimals     int  NOT NULL CHECK (spot_decimals >= 0 AND spot_decimals <= 30),
    hedge_ratio       numeric(10,4) NOT NULL DEFAULT 1.0,
    notes             text,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (spot_chain, spot_mint)
);

CREATE INDEX IF NOT EXISTS assets_perp_idx ON assets (perp_venue, perp_symbol)
    WHERE perp_venue IS NOT NULL;

-- Wallets — operator-controlled signing addresses, populated by the wallet
-- package. Address book; NEVER stores keys.
CREATE TABLE IF NOT EXISTS wallets (
    id          bigserial PRIMARY KEY,
    chain       text NOT NULL,
    address     text NOT NULL,
    label       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (chain, address)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS assets;
-- +goose StatementEnd
