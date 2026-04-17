package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/types"
)

// Store wraps DB access for agent lifecycle and the decision/order/swap
// audit logs.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts a new agent and returns it. ID must be non-empty.
func (s *Store) Create(ctx context.Context, a Agent) (Agent, error) {
	if a.ID == "" {
		return Agent{}, errors.New("agent: id required")
	}
	if a.Strategy == "" {
		return Agent{}, errors.New("agent: strategy required")
	}
	if a.Mode == "" {
		a.Mode = ModePaper
	}
	if a.Status == "" {
		a.Status = StatusStopped
	}
	if a.Network == "" {
		a.Network = NetworkMainnet
	}
	if err := a.Network.Validate(); err != nil {
		return Agent{}, err
	}
	cfgBytes, _ := json.Marshal(orZero(a.Config))
	const q = `
INSERT INTO agents (id, name, strategy, mode, network, perp_venue, spot_venue, inference, universe, allocation_usdc, tick_secs, status, config)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING created_at, updated_at;`
	if err := s.pool.QueryRow(ctx, q,
		a.ID, a.Name, a.Strategy, string(a.Mode), string(a.Network),
		a.PerpVenue, a.SpotVenue, a.Inference,
		a.Universe, a.AllocationUSDC, a.TickSecs, string(a.Status), cfgBytes,
	).Scan(&a.CreatedAt, &a.UpdatedAt); err != nil {
		return Agent{}, fmt.Errorf("agent: create: %w", err)
	}
	return a, nil
}

// Get returns an agent by id.
func (s *Store) Get(ctx context.Context, id string) (Agent, error) {
	const q = `SELECT id, name, strategy, mode, network, perp_venue, spot_venue, inference, universe, allocation_usdc, tick_secs, status, config, created_at, updated_at
FROM agents WHERE id = $1`
	var (
		a   Agent
		cfg []byte
	)
	if err := s.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.Name, &a.Strategy, (*string)(&a.Mode), (*string)(&a.Network),
		&a.PerpVenue, &a.SpotVenue, &a.Inference, &a.Universe,
		&a.AllocationUSDC, &a.TickSecs, (*string)(&a.Status),
		&cfg, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Agent{}, fmt.Errorf("agent: get: %w", err)
	}
	if len(cfg) > 0 {
		_ = json.Unmarshal(cfg, &a.Config)
	}
	return a, nil
}

// List returns all agents (newest first).
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, strategy, mode, network, status, allocation_usdc, created_at FROM agents ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Agent, 0)
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Strategy, (*string)(&a.Mode), (*string)(&a.Network), (*string)(&a.Status), &a.AllocationUSDC, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetNetwork updates an agent's persisted network.
func (s *Store) SetNetwork(ctx context.Context, id string, network Network) error {
	if err := network.Validate(); err != nil {
		return err
	}
	if network == "" {
		return errors.New("agent: network required")
	}
	res, err := s.pool.Exec(ctx,
		`UPDATE agents SET network = $1, updated_at = now() WHERE id = $2`,
		string(network), id)
	if err != nil {
		return fmt.Errorf("agent: set network: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("agent %q not found", id)
	}
	return nil
}

// SetStatus updates an agent's persisted lifecycle status.
func (s *Store) SetStatus(ctx context.Context, id string, status Status) error {
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET status = $1, updated_at = now() WHERE id = $2`,
		string(status), id); err != nil {
		return fmt.Errorf("agent: set status: %w", err)
	}
	return nil
}

// StartRun records the start of an agent run.
func (s *Store) StartRun(ctx context.Context, id string) (int64, error) {
	var runID int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO agent_runs (agent_id) VALUES ($1) RETURNING id`, id).Scan(&runID); err != nil {
		return 0, fmt.Errorf("agent: start run: %w", err)
	}
	return runID, nil
}

// EndRun records the end of an agent run with a freeform reason.
func (s *Store) EndRun(ctx context.Context, runID int64, reason string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE agent_runs SET ended_at = now(), exit_reason = $2 WHERE id = $1`,
		runID, reason); err != nil {
		return fmt.Errorf("agent: end run: %w", err)
	}
	return nil
}

// ─── decision / order / swap persistence ────────────────────────────────────

// PersistDecisionInput is the data set persisted per Decide() call.
type PersistDecisionInput struct {
	Time       time.Time
	AgentID    string
	DecisionID string
	InputHash  string
	Decision   any // serialised as JSONB
	Rationale  string
	Provider   string
	Model      string
	TokensIn   int
	TokensOut  int
	LatencyMS  int64
	CostUSD    float64
}

// PersistDecision writes one row to agent_decisions.
func (s *Store) PersistDecision(ctx context.Context, in PersistDecisionInput) error {
	body, err := json.Marshal(in.Decision)
	if err != nil {
		return fmt.Errorf("agent: marshal decision: %w", err)
	}
	const q = `INSERT INTO agent_decisions
(time, agent_id, decision_id, input_hash, decision, rationale, provider, model, tokens_in, tokens_out, latency_ms, cost_usd)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (agent_id, time, decision_id) DO NOTHING;`
	if _, err := s.pool.Exec(ctx, q,
		in.Time.UTC(), in.AgentID, in.DecisionID, in.InputHash, body, in.Rationale,
		nz(in.Provider), nz(in.Model), in.TokensIn, in.TokensOut, in.LatencyMS, in.CostUSD,
	); err != nil {
		return fmt.Errorf("agent: persist decision: %w", err)
	}
	return nil
}

// PersistOrderInput is the order ledger row.
type PersistOrderInput struct {
	Time       time.Time
	AgentID    string
	DecisionID string
	Venue      string
	Symbol     string
	Side       string
	Type       string
	Price      decimal.Decimal
	Size       decimal.Decimal
	TIF        string
	ReduceOnly bool
	ClientID   string
	VenueID    string
	Status     string
	Paper      bool
}

// PersistOrder appends a row to orders.
func (s *Store) PersistOrder(ctx context.Context, in PersistOrderInput) error {
	const q = `INSERT INTO orders
(time, agent_id, decision_id, venue, symbol, side, type, price, size, tif, reduce_only, client_id, venue_id, status, paper)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (agent_id, time, client_id) DO NOTHING;`
	if _, err := s.pool.Exec(ctx, q,
		in.Time.UTC(), in.AgentID, in.DecisionID, in.Venue, in.Symbol, in.Side, in.Type,
		in.Price, in.Size, in.TIF, in.ReduceOnly, in.ClientID, nz(in.VenueID), in.Status, in.Paper,
	); err != nil {
		return fmt.Errorf("agent: persist order: %w", err)
	}
	return nil
}

// PersistSwapInput is the swap ledger row.
type PersistSwapInput struct {
	Time        time.Time
	AgentID     string
	DecisionID  string
	Chain       string
	DEX         string
	InToken     string
	OutToken    string
	InAmount    decimal.Decimal
	OutAmount   decimal.Decimal
	SlippageBps int
	GasPaid     decimal.Decimal
	TxHash      string
	Status      string
	Paper       bool
}

// PersistSwap appends a row to swaps.
func (s *Store) PersistSwap(ctx context.Context, in PersistSwapInput) error {
	const q = `INSERT INTO swaps
(time, agent_id, decision_id, chain, dex, in_token, out_token, in_amount, out_amount, slippage_bps, gas_paid, tx_hash, status, paper)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (agent_id, time, in_token, out_token) DO NOTHING;`
	if _, err := s.pool.Exec(ctx, q,
		in.Time.UTC(), in.AgentID, in.DecisionID, in.Chain, in.DEX, in.InToken, in.OutToken,
		in.InAmount, in.OutAmount, in.SlippageBps, in.GasPaid, nz(in.TxHash), in.Status, in.Paper,
	); err != nil {
		return fmt.Errorf("agent: persist swap: %w", err)
	}
	return nil
}

// PersistInferenceCallInput is the audit row for an LLM call.
type PersistInferenceCallInput struct {
	Time      time.Time
	AgentID   string
	Provider  string
	Model     string
	LatencyMS int64
	TokensIn  int
	TokensOut int
	CostUSD   float64
	Status    string
}

// PersistInferenceCall appends a row to inference_calls.
func (s *Store) PersistInferenceCall(ctx context.Context, in PersistInferenceCallInput) error {
	const q = `INSERT INTO inference_calls
(time, agent_id, provider, model, latency_ms, tokens_in, tokens_out, cost_usd, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (provider, time, model) DO NOTHING;`
	if _, err := s.pool.Exec(ctx, q,
		in.Time.UTC(), nz(in.AgentID), in.Provider, in.Model, in.LatencyMS,
		in.TokensIn, in.TokensOut, in.CostUSD, in.Status,
	); err != nil {
		return fmt.Errorf("agent: persist inference call: %w", err)
	}
	return nil
}

// ─── strategy_positions persistence ─────────────────────────────────────────

// UpsertOpenBasis records or refreshes an OPEN basis position. Returns an
// error if a different open position already exists for this underlying
// (the unique partial index enforces at-most-one-open-per-underlying).
func (s *Store) UpsertOpenBasis(ctx context.Context, p types.BasisPosition) error {
	legs, err := json.Marshal(p.Legs)
	if err != nil {
		return fmt.Errorf("agent: marshal legs: %w", err)
	}
	const q = `
INSERT INTO strategy_positions
    (id, agent_id, underlying, state, legs, opened_at, realized_funding, realized_basis_pnl, realized_fees, realized_gas)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE
SET state              = EXCLUDED.state,
    legs               = EXCLUDED.legs,
    realized_funding   = EXCLUDED.realized_funding,
    realized_basis_pnl = EXCLUDED.realized_basis_pnl,
    realized_fees      = EXCLUDED.realized_fees,
    realized_gas       = EXCLUDED.realized_gas,
    updated_at         = now();
`
	state := string(p.State)
	if state == "" {
		state = string(types.BasisStateOpen)
	}
	openedAt := p.OpenedAt
	if openedAt.IsZero() {
		openedAt = time.Now().UTC()
	}
	if _, err := s.pool.Exec(ctx, q,
		p.ID, p.AgentID, p.Underlying, state, legs, openedAt,
		p.RealizedFunding, p.RealizedBasisPnL, p.RealizedFees, p.RealizedGas,
	); err != nil {
		return fmt.Errorf("agent: upsert basis: %w", err)
	}
	return nil
}

// CloseBasis marks the OPEN basis for (agentID, underlying) as 'closed'
// and stamps closed_at. Returns ErrNoOpenBasis if no matching row exists.
func (s *Store) CloseBasis(ctx context.Context, agentID, underlying string) error {
	res, err := s.pool.Exec(ctx, `
UPDATE strategy_positions
SET state = 'closed', closed_at = now(), updated_at = now()
WHERE agent_id = $1 AND underlying = $2 AND state IN ('open', 'opening', 'closing')`,
		agentID, underlying)
	if err != nil {
		return fmt.Errorf("agent: close basis: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrNoOpenBasis
	}
	return nil
}

// ErrNoOpenBasis is returned by CloseBasis when no open row exists for the
// supplied agent + underlying. Callers may treat this as a no-op.
var ErrNoOpenBasis = errors.New("agent: no open basis to close")

// LoadOpenBasis returns all currently-open BasisPositions for the agent.
// Used by the runtime to hydrate its in-memory view on startup.
func (s *Store) LoadOpenBasis(ctx context.Context, agentID string) ([]types.BasisPosition, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, agent_id, underlying, state, legs, opened_at, closed_at,
       realized_funding, realized_basis_pnl, realized_fees, realized_gas
FROM strategy_positions
WHERE agent_id = $1 AND state IN ('open', 'opening', 'closing')
ORDER BY opened_at`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: load open basis: %w", err)
	}
	defer rows.Close()
	out := make([]types.BasisPosition, 0)
	for rows.Next() {
		var (
			p        types.BasisPosition
			legsJSON []byte
			closedAt *time.Time
		)
		if err := rows.Scan(
			&p.ID, &p.AgentID, &p.Underlying, (*string)(&p.State), &legsJSON,
			&p.OpenedAt, &closedAt,
			&p.RealizedFunding, &p.RealizedBasisPnL, &p.RealizedFees, &p.RealizedGas,
		); err != nil {
			return nil, err
		}
		if len(legsJSON) > 0 {
			_ = json.Unmarshal(legsJSON, &p.Legs)
		}
		p.ClosedAt = closedAt
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecentDecisions returns the most recent decisions for an agent, oldest first.
func (s *Store) RecentDecisions(ctx context.Context, agentID string, since time.Time, limit int) ([]DecisionRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT time, decision_id, COALESCE(rationale,''), COALESCE(provider,''), COALESCE(model,''), tokens_in, tokens_out, cost_usd
FROM agent_decisions WHERE agent_id = $1 AND time >= $2 ORDER BY time DESC LIMIT $3`,
		agentID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DecisionRow, 0)
	for rows.Next() {
		var r DecisionRow
		if err := rows.Scan(&r.Time, &r.DecisionID, &r.Rationale, &r.Provider, &r.Model, &r.TokensIn, &r.TokensOut, &r.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecisionRow is a row-shaped subset of agent_decisions used by the CLI.
type DecisionRow struct {
	Time       time.Time
	DecisionID string
	Rationale  string
	Provider   string
	Model      string
	TokensIn   int
	TokensOut  int
	CostUSD    float64
}

func orZero(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nz(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
