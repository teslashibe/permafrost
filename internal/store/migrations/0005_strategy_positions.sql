-- +goose Up
-- +goose StatementBegin

-- Strategy-level paired positions (e.g. delta-neutral basis: long spot + short perp).
-- One row per opened basis; state transitions to 'closed' on unwind.
-- Per-leg detail (orders, fills, swaps) lives in those tables and joins
-- back via decision_id. Realised fees / funding / basis-pnl roll up here.
CREATE TABLE IF NOT EXISTS strategy_positions (
    id                  text PRIMARY KEY,
    agent_id            text NOT NULL REFERENCES agents(id),
    underlying          text NOT NULL,
    state               text NOT NULL DEFAULT 'open',  -- open | closing | closed | broken
    legs                jsonb NOT NULL,
    opened_at           timestamptz NOT NULL,
    closed_at           timestamptz,
    realized_funding    numeric(38,9) NOT NULL DEFAULT 0,
    realized_basis_pnl  numeric(38,9) NOT NULL DEFAULT 0,
    realized_fees       numeric(38,9) NOT NULL DEFAULT 0,
    realized_gas        numeric(38,9) NOT NULL DEFAULT 0,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- At most one OPEN position per (agent_id, underlying). Closed positions are
-- not constrained — historical record stays intact.
CREATE UNIQUE INDEX IF NOT EXISTS strategy_positions_one_open_per_underlying
    ON strategy_positions (agent_id, underlying)
    WHERE state IN ('open', 'opening', 'closing');

CREATE INDEX IF NOT EXISTS strategy_positions_agent_state_idx
    ON strategy_positions (agent_id, state);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS strategy_positions;
-- +goose StatementEnd
