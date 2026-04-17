-- +goose Up
-- +goose StatementBegin

-- Per-agent Hyperliquid network (mainnet | testnet).
-- Defaults to 'mainnet' so paper-mode agents see real funding rates by
-- default — paper is safe because no real signing is wired yet, and real
-- data is more useful than the sparse / synthetic testnet data.
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS network text NOT NULL DEFAULT 'mainnet';

ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_network_valid;
ALTER TABLE agents
    ADD CONSTRAINT agents_network_valid
    CHECK (network IN ('mainnet', 'testnet'));

CREATE INDEX IF NOT EXISTS agents_network_idx ON agents (network);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS agents_network_idx;
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_network_valid;
ALTER TABLE agents DROP COLUMN IF EXISTS network;
-- +goose StatementEnd
