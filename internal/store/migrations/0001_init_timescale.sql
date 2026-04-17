-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Sanity check table — confirms migrations and the Timescale extension are wired.
CREATE TABLE IF NOT EXISTS schema_meta (
    key         text PRIMARY KEY,
    value       text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO schema_meta (key, value)
VALUES ('initialised_at', now()::text)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS schema_meta;
-- Note: we do not drop the timescaledb extension; other databases on the
-- cluster may rely on it.
-- +goose StatementEnd
