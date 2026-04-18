-- +goose Up
-- +goose StatementBegin

-- Per-agent NAV snapshots (separate from the vault-level nav_snapshots from
-- migration 0003). One row per (agent, tick), recording every dollar:
--
--   nav_usdc             = total value if everything closed right now
--   spot_value_usdc      = sum of open spot legs valued at current marks
--   perp_unrealized_usdc = sum of open perp legs' unrealized P&L
--   funding_accrued_usdc = funding payments received since each position open
--                          (currently 0; populated when HL userFunding wiring lands)
--   realized_pnl_usdc    = cumulative realized P&L since agent inception
--                          (from strategy_positions where state='closed')
--   cumulative_gas_usdc  = cumulative gas + fees across every swap/order
--   open_positions       = count of basis positions currently in 'open' state
--   positions            = per-basis breakdown (BasisValuation array as JSONB)
--                          for ad-hoc inspection without joins
CREATE TABLE IF NOT EXISTS agent_nav_snapshots (
    time                  timestamptz   NOT NULL,
    agent_id              text          NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    nav_usdc              numeric(38,9) NOT NULL,
    spot_value_usdc       numeric(38,9) NOT NULL,
    perp_unrealized_usdc  numeric(38,9) NOT NULL,
    funding_accrued_usdc  numeric(38,9) NOT NULL,
    realized_pnl_usdc     numeric(38,9) NOT NULL,
    cumulative_gas_usdc   numeric(38,9) NOT NULL,
    open_positions        int           NOT NULL,
    positions             jsonb,
    PRIMARY KEY (agent_id, time)
);

SELECT create_hypertable('agent_nav_snapshots', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS agent_nav_snapshots_agent_time_desc_idx
    ON agent_nav_snapshots (agent_id, time DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS agent_nav_snapshots;
-- +goose StatementEnd
