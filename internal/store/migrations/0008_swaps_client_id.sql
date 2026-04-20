-- +goose Up
-- +goose StatementBegin

-- swaps.client_id closes a per-decision idempotency footgun. The original
-- ON CONFLICT key was (agent_id, time, in_token, out_token), which would
-- silently drop legitimate same-instant duplicate swaps for the same pair
-- and offered no alignment with the runtime's deterministic client_id
-- (clientIDFromSwap in internal/agent/runtime.go).
--
-- The new key is (agent_id, time, client_id). Same logical intent inside
-- the same tick → same client_id → DB-level dedupe. TimescaleDB requires
-- the partitioning column (time) in any UNIQUE constraint, so we keep
-- time in the key.
--
-- Backfill strategy: client_id is added NULL-able so existing rows
-- (from pre-v0.1 deployments) survive the migration unchanged. The
-- partial unique index covers only rows with a non-null client_id,
-- which is everything new code will write going forward.

ALTER TABLE swaps ADD COLUMN IF NOT EXISTS client_id text;

-- Partial unique index. The application-level invariant is that every
-- new row carries a deterministic client_id; legacy NULL rows are
-- exempt from the constraint so the migration is non-destructive.
CREATE UNIQUE INDEX IF NOT EXISTS swaps_client_id_unique
    ON swaps (agent_id, time, client_id)
    WHERE client_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS swaps_client_id_unique;
ALTER TABLE swaps DROP COLUMN IF EXISTS client_id;
-- +goose StatementEnd
