-- +goose Up
-- +goose StatementBegin

-- Agents — one row per configured strategy instance.
CREATE TABLE IF NOT EXISTS agents (
    id              text PRIMARY KEY,                  -- caller-supplied or generated uuid
    name            text NOT NULL,
    strategy        text NOT NULL,                     -- registered strategy name
    mode            text NOT NULL DEFAULT 'paper',     -- 'paper' | 'live'
    perp_venue      text NOT NULL,                     -- e.g. 'hyperliquid'
    spot_venue      text NOT NULL DEFAULT 'jupiter',
    inference       text NOT NULL,                     -- provider:model
    universe        text[] NOT NULL DEFAULT ARRAY[]::text[],
    allocation_usdc numeric(38,9) NOT NULL DEFAULT 0,
    tick_secs       int NOT NULL DEFAULT 60,
    status          text NOT NULL DEFAULT 'stopped',   -- 'stopped' | 'running' | 'halted'
    config          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS agents_status_idx ON agents (status);

-- agent_runs — supervisor lifecycle ledger.
CREATE TABLE IF NOT EXISTS agent_runs (
    id          bigserial PRIMARY KEY,
    agent_id    text NOT NULL REFERENCES agents(id),
    started_at  timestamptz NOT NULL DEFAULT now(),
    ended_at    timestamptz,
    exit_reason text
);

CREATE INDEX IF NOT EXISTS agent_runs_agent_idx ON agent_runs (agent_id, started_at DESC);

-- agent_decisions — every Strategy.Decide invocation, with the resulting
-- decision as JSONB and any LLM provenance.
CREATE TABLE IF NOT EXISTS agent_decisions (
    time         timestamptz NOT NULL,
    agent_id     text NOT NULL,
    decision_id  text NOT NULL,
    input_hash   text NOT NULL,
    decision     jsonb NOT NULL,
    rationale    text,
    model        text,
    provider     text,
    tokens_in    int,
    tokens_out   int,
    latency_ms   int,
    cost_usd     numeric(12,6),
    PRIMARY KEY (agent_id, time, decision_id)
);

SELECT create_hypertable('agent_decisions', 'time', if_not_exists => TRUE);

-- orders — all order intents the runtime submitted (or paper-recorded).
CREATE TABLE IF NOT EXISTS orders (
    time         timestamptz NOT NULL,
    agent_id     text NOT NULL,
    decision_id  text,
    venue        text NOT NULL,
    symbol       text NOT NULL,
    side         text NOT NULL,
    type         text NOT NULL,
    price        numeric(38,9),
    size         numeric(38,9) NOT NULL,
    tif          text,
    reduce_only  boolean NOT NULL DEFAULT false,
    client_id    text NOT NULL,
    venue_id     text,
    status       text NOT NULL,
    paper        boolean NOT NULL DEFAULT false,
    PRIMARY KEY (agent_id, time, client_id)
);

SELECT create_hypertable('orders', 'time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS orders_decision_idx ON orders (decision_id);

-- swaps — all swap intents the runtime submitted.
CREATE TABLE IF NOT EXISTS swaps (
    time         timestamptz NOT NULL,
    agent_id     text NOT NULL,
    decision_id  text,
    chain        text NOT NULL,
    dex          text NOT NULL,
    in_token     text NOT NULL,
    out_token    text NOT NULL,
    in_amount    numeric(38,9) NOT NULL,
    out_amount   numeric(38,9) NOT NULL DEFAULT 0,
    slippage_bps int,
    gas_paid     numeric(38,9) NOT NULL DEFAULT 0,
    tx_hash      text,
    status       text NOT NULL,
    paper        boolean NOT NULL DEFAULT false,
    PRIMARY KEY (agent_id, time, in_token, out_token)
);

SELECT create_hypertable('swaps', 'time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS swaps_decision_idx ON swaps (decision_id);

-- inference_calls — LLM call audit log.
CREATE TABLE IF NOT EXISTS inference_calls (
    time        timestamptz NOT NULL,
    agent_id    text,
    provider    text NOT NULL,
    model       text NOT NULL,
    latency_ms  int NOT NULL DEFAULT 0,
    tokens_in   int NOT NULL DEFAULT 0,
    tokens_out  int NOT NULL DEFAULT 0,
    cost_usd    numeric(12,6) NOT NULL DEFAULT 0,
    status      text NOT NULL,
    PRIMARY KEY (provider, time, model)
);

SELECT create_hypertable('inference_calls', 'time', if_not_exists => TRUE);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS inference_calls;
DROP TABLE IF EXISTS swaps;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS agent_decisions;
DROP TABLE IF EXISTS agent_runs;
DROP TABLE IF EXISTS agents;
-- +goose StatementEnd
