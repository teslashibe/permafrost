package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/pnl"
	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Deps bundles the dependencies a Runtime needs to execute one Agent.
// The intention is one Runtime per Agent; the Supervisor builds Deps from
// the global registries (venues, inference, etc.).
type Deps struct {
	Strategy strategy.Strategy
	Perp     exchange.Venue

	// Swaps routes a SwapIntent to the appropriate on-chain venue by
	// the intent's Chain field. v1 supports:
	//   - types.ChainSolana    → Jupiter (internal/swap/jupiter)
	//   - types.ChainEthereum  → 1inch
	//   - types.ChainBase      → 1inch
	//   - types.ChainAvalanche → 1inch
	//   - types.ChainBSC       → 1inch
	// Empty map / missing entry for a given chain → the runtime
	// downgrades that intent to paper-spot bookkeeping.
	Swaps map[types.ChainID]swap.SwapVenue

	// Inference is the resolved LLM Provider for this agent. nil when
	// the agent has no inference configured (a.Inference == "") or
	// when no inference Registry was supplied to BuildDeps.
	Inference inference.Provider
	// InferenceModel is the model id parsed from a.Inference (the part
	// after ":"). Surfaced to strategies via Services so they can use
	// the operator's chosen model as a default.
	InferenceModel string
	// Registry is the curated asset registry. Always non-nil when set
	// by BuildDeps; surfaced to strategies via Services.
	Registry assets.Registry

	Risk   risk.Engine
	Store  *Store
	Logger *slog.Logger
}

// SwapVenueForChain returns the venue routed for the given chain or nil
// if none is configured.
func (d Deps) SwapVenueForChain(c types.ChainID) swap.SwapVenue {
	if d.Swaps == nil {
		return nil
	}
	return d.Swaps[c]
}

// Runtime drives the tick loop for one Agent.
type Runtime struct {
	agent Agent
	deps  Deps
	now   func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

	// warmupOnce guarantees Strategy.Warmup is invoked exactly once per
	// Runtime regardless of which entrypoint runs first (Start, TickOnce
	// from a foreground CLI, or a backtest/test harness). warmupErr
	// captures the result so subsequent callers see the same outcome.
	warmupOnce sync.Once
	warmupErr  error

	// openBasis tracks paper-mode (and to-be-fully-persisted live-mode)
	// basis positions across ticks within a Run. Keyed by underlying symbol.
	// This is in-memory state only; v1.1 should drive this off a
	// strategy_positions row in the database so it survives restarts.
	posMu     sync.Mutex
	openBasis map[string]types.BasisPosition
}

// NewRuntime constructs a Runtime.
//
// If Deps.Store is set, NewRuntime hydrates openBasis from any persisted
// strategy_positions rows so an agent that's restarted picks up where it
// left off. Hydration errors are logged but don't fail construction.
func NewRuntime(a Agent, d Deps) *Runtime {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	r := &Runtime{
		agent:     a,
		deps:      d,
		now:       func() time.Time { return time.Now().UTC() },
		openBasis: make(map[string]types.BasisPosition),
	}
	if d.Store != nil {
		if existing, err := d.Store.LoadOpenBasis(context.Background(), a.ID); err != nil {
			d.Logger.Warn("hydrate open basis failed", "agent_id", a.ID, "err", err)
		} else {
			for _, p := range existing {
				r.openBasis[strings.ToUpper(p.Underlying)] = p
			}
			if len(existing) > 0 {
				d.Logger.Info("hydrated open basis", "agent_id", a.ID, "count", len(existing))
			}
		}
	}
	return r
}

// SetClock overrides the runtime's clock function. For tests only.
func (r *Runtime) SetClock(fn func() time.Time) { r.now = fn }

// IsRunning reports whether the tick loop is active.
func (r *Runtime) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Start launches the tick loop and returns immediately. The supplied parent
// context controls overall lifetime. Use Stop() for graceful shutdown.
//
// Start does NOT mutate the persisted status — that is an operator action
// performed via the CLI / API. This keeps Stop+restart semantics clean: a
// graceful daemon shutdown leaves status='running' so the next boot resumes
// the agent automatically.
func (r *Runtime) Start(parent context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return errors.New("runtime: already running")
	}
	if err := r.Warmup(parent); err != nil {
		return fmt.Errorf("runtime: strategy warmup: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.running = true
	go r.loop(ctx)
	return nil
}

// Warmup invokes Strategy.Warmup with the per-agent context and the
// framework Services bag. It is safe and cheap to call multiple times:
// the underlying invocation runs at most once per Runtime via sync.Once,
// and subsequent calls return the cached error (nil on success).
//
// Start calls Warmup automatically. Foreground CLI paths (`agent run`,
// `agent tick`) and any test harness that drives the runtime via
// TickOnce should not need to call it explicitly — TickOnce performs
// the same once-guarded call before the first decision so strategies
// always see a Warmup-completed receiver. Calling Warmup directly is
// useful when a caller wants to surface the error before the first tick.
func (r *Runtime) Warmup(ctx context.Context) error {
	r.warmupOnce.Do(func() {
		r.warmupErr = r.deps.Strategy.Warmup(ctx, strategy.WarmupInput{
			AgentID:        r.agent.ID,
			Universe:       r.agent.Universe,
			Config:         r.agent.Config,
			Now:            r.now(),
			Services:       r.services(),
		})
	})
	return r.warmupErr
}

// services builds the Services bag the runtime exposes to the strategy.
// Centralised so any future Service field gets wired in one place.
func (r *Runtime) services() strategy.Services {
	return strategy.Services{
		Logger:         r.deps.Logger,
		Inference:      r.deps.Inference,
		InferenceModel: r.deps.InferenceModel,
		Registry:       r.deps.Registry,
	}
}

// Stop gracefully halts the tick loop. It does NOT change the persisted
// agent status — see the Start docstring for the rationale. Operators that
// want a permanent stop should use the CLI/API to set status='halted'.
func (r *Runtime) Stop(_ context.Context, reason string) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = false
	r.cancel()
	r.mu.Unlock()
	r.deps.Logger.Info("agent stopped", "agent_id", r.agent.ID, "reason", reason)
	return nil
}

func (r *Runtime) loop(ctx context.Context) {
	tick := time.NewTicker(r.agent.Interval())
	defer tick.Stop()

	// run an immediate tick on start
	r.tickOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.tickOnce(ctx)
		}
	}
}

// TickOnce executes a single decision tick. Exported for tests and for
// "permafrost agent tick" debugging.
func (r *Runtime) TickOnce(ctx context.Context) (strategy.Decision, error) {
	return r.tickOnce(ctx)
}

func (r *Runtime) tickOnce(ctx context.Context) (strategy.Decision, error) {
	if err := r.Warmup(ctx); err != nil {
		// Warmup is once-guarded; the same error surfaces every tick
		// until the operator restarts. Logged once at first occurrence
		// would be ideal, but for v1 we accept the per-tick log so the
		// failure stays visible in tail-the-log workflows.
		r.deps.Logger.Error("strategy warmup failed", "agent_id", r.agent.ID, "err", err)
		return strategy.Decision{}, fmt.Errorf("strategy warmup: %w", err)
	}
	now := r.now()
	in, err := r.buildDecisionInput(ctx, now)
	if err != nil {
		r.deps.Logger.Error("agent input failed", "agent_id", r.agent.ID, "err", err)
		return strategy.Decision{}, err
	}

	dec, err := r.deps.Strategy.Decide(ctx, in)
	if err != nil {
		r.deps.Logger.Error("agent decide failed", "agent_id", r.agent.ID, "err", err)
		return strategy.Decision{}, err
	}

	decisionID := uuid.NewString()
	r.persistDecision(ctx, now, decisionID, in, dec)

	r.executeSwapsThenOrders(ctx, now, decisionID, in, dec)

	// Persist a NAV snapshot on every tick. Failures are logged but do
	// not bubble up — telemetry shouldn't block trading.
	r.persistNAVSnapshot(ctx, now)

	// One concise per-tick log so operators can see the daemon working
	// without enabling debug. This is the only INFO line emitted per tick.
	// open_basis is the post-tick count (after reconcile), not the
	// pre-tick snapshot — operators care about "what state did this tick
	// leave us in".
	r.deps.Logger.Info("tick",
		"agent_id", r.agent.ID,
		"decision_id", decisionID,
		"swaps", len(dec.Swaps),
		"orders", len(dec.Orders),
		"cancels", len(dec.Cancels),
		"open_basis", len(r.snapshotOpenBasis()),
	)
	return dec, nil
}

// persistNAVSnapshot computes a NAV snapshot using the pnl.Engine and
// writes it to agent_nav_snapshots. Best-effort — any failure (RPC,
// DB) is logged at WARN and does not affect the tick.
func (r *Runtime) persistNAVSnapshot(ctx context.Context, now time.Time) {
	if r.deps.Store == nil {
		return
	}
	engine := pnl.New(r.deps.Perp, r.deps.Swaps)
	hist, err := r.deps.Store.AggregatesForAgent(ctx, r.agent.ID)
	if err != nil {
		r.deps.Logger.Warn("nav: aggregates failed", "agent_id", r.agent.ID, "err", err)
		return
	}
	nav, err := engine.ValueAgent(ctx, r.agent.ID, pnl.History{
		OpenPositions:     r.snapshotOpenBasis(),
		RealizedPnLUSDC:   hist.RealizedPnLUSDC,
		CumulativeGasUSDC: hist.CumulativeGasUSDC,
	})
	if err != nil {
		r.deps.Logger.Warn("nav: valuation failed", "agent_id", r.agent.ID, "err", err)
		return
	}
	posJSON, _ := json.Marshal(nav.Positions)
	if err := r.deps.Store.PersistNAVSnapshot(ctx, PersistNAVSnapshotInput{
		Time:               now,
		AgentID:            r.agent.ID,
		NAVUSDC:            nav.NAVUSDC,
		SpotValueUSDC:      nav.SpotValueUSDC,
		PerpUnrealizedUSDC: nav.PerpUnrealizedUSDC,
		FundingAccruedUSDC: nav.FundingAccruedUSDC,
		RealizedPnLUSDC:    nav.RealizedPnLUSDC,
		CumulativeGasUSDC:  nav.CumulativeGasUSDC,
		OpenPositions:      nav.OpenPositions,
		PositionsJSON:      posJSON,
	}); err != nil {
		r.deps.Logger.Warn("nav: persist failed", "agent_id", r.agent.ID, "err", err)
	}
}

// buildDecisionInput pulls current state from venues + the configured
// universe and bundles it for Strategy.Decide. PerpPositions and Balances
// are only fetched if the venue supports them (per-account reads can fail
// gracefully when no Address is configured — see hyperliquid.ErrAddressRequired).
func (r *Runtime) buildDecisionInput(ctx context.Context, now time.Time) (strategy.DecisionInput, error) {
	in := strategy.DecisionInput{
		AgentID:        r.agent.ID,
		Now:            now,
		Cash:           map[string]types.Balance{},
		Limits:         types.RiskLimits{},
		BasisPositions: r.snapshotOpenBasis(),
	}
	if r.deps.Perp != nil {
		if pos, err := r.deps.Perp.Positions(ctx); err == nil {
			in.PerpPositions = pos
		}
		if bals, err := r.deps.Perp.Balances(ctx); err == nil {
			for _, b := range bals {
				in.Cash["hyperliquid"] = b
			}
		}
		if rates, err := r.deps.Perp.FundingRates(ctx, r.agent.Universe); err == nil {
			snap := types.MarketSnapshot{Time: now, Symbols: map[string]types.SymbolSnap{}}
			for _, fr := range rates {
				snap.Symbols[fr.Symbol] = types.SymbolSnap{Funding: fr}
			}
			in.Market = snap
		}
	}
	return in, nil
}

// snapshotOpenBasis returns a defensive copy of the runtime's currently-open
// basis positions, in deterministic order.
func (r *Runtime) snapshotOpenBasis() []types.BasisPosition {
	r.posMu.Lock()
	defer r.posMu.Unlock()
	out := make([]types.BasisPosition, 0, len(r.openBasis))
	for _, p := range r.openBasis {
		out = append(out, p)
	}
	return out
}

func (r *Runtime) persistDecision(ctx context.Context, now time.Time, decisionID string, in strategy.DecisionInput, dec strategy.Decision) {
	if r.deps.Store == nil {
		return
	}
	hash := decisionInputHash(in)
	if err := r.deps.Store.PersistDecision(ctx, PersistDecisionInput{
		Time:       now,
		AgentID:    r.agent.ID,
		DecisionID: decisionID,
		InputHash:  hash,
		Decision:   dec,
		Rationale:  dec.Notes,
	}); err != nil {
		r.deps.Logger.Warn("persist decision failed", "agent_id", r.agent.ID, "err", err)
	}
}

// executeSwapsThenOrders runs swaps first (spot leg), then orders (perp leg)
// — this is the spot-first invariant the runtime guarantees.
//
// Each leg's success is tracked per-symbol so reconcileOpenBasis only
// marks a basis "open" when BOTH legs succeeded. A failed swap or order
// is recorded in the audit ledger (orders / swaps tables) with a failed
// status but does not produce a basis-position row — leaving the strategy
// free to retry on the next tick instead of believing it already has the
// position.
//
// Risk PreTrade is evaluated per intent against a fresh PortfolioSnapshot
// that includes any in-flight opens accepted earlier in this tick. This
// matters for MaxConcurrentPositions: with cap=2 and the strategy
// emitting 5 opens, the first 2 pass and the 3rd-5th get blocked.
//
// Inflight + success are tracked per BasisKey when the strategy supplies
// it (multi-chain pairs like ETH-BASE need an explicit key because the
// spot OutToken.Symbol differs from the perp Symbol). Falls back to the
// symbol-based key for legacy/single-chain strategies that don't set
// BasisKey.
func (r *Runtime) executeSwapsThenOrders(ctx context.Context, now time.Time, decisionID string, _ strategy.DecisionInput, dec strategy.Decision) {
	swapOK := map[string]bool{}        // keyed by basis key (or out-token symbol if missing)
	orderOK := map[string]bool{}       // keyed by basis key (or perp symbol if missing)
	inflightOpens := map[string]bool{} // basis keys (or symbols) accepted this tick

	for i, s := range dec.Swaps {
		snap := r.riskSnapshot(inflightOpens, "")
		if r.executeSwap(ctx, now, decisionID, i, s, snap) {
			key := swapBasisKey(s)
			swapOK[key] = true
			if isOpeningSwap(s) {
				inflightOpens[key] = true
			}
		}
	}
	for i, o := range dec.Orders {
		// If this order's perp leg is paired with a swap already accepted
		// in this tick (same BasisKey), exclude that key from the inflight
		// count — the swap leg already claimed the cap slot for this basis.
		key := orderBasisKey(o)
		exclude := ""
		if !o.ReduceOnly && o.Side == types.SideSell && inflightOpens[key] {
			exclude = key
		}
		snap := r.riskSnapshot(inflightOpens, exclude)
		if r.executeOrder(ctx, now, decisionID, i, o, snap) {
			orderOK[key] = true
			// If this order opened WITHOUT a paired swap (no inflight
			// match), it counts as a brand-new open for subsequent
			// intents in this tick.
			if !o.ReduceOnly && o.Side == types.SideSell && !swapOK[key] {
				inflightOpens[key] = true
			}
		}
	}
	for _, c := range dec.Cancels {
		r.executeCancel(ctx, c)
	}
	r.reconcileOpenBasis(ctx, now, decisionID, dec, swapOK, orderOK)
}

// swapBasisKey returns the canonical basis-pair key for a swap intent.
// Strategies that emit paired swap+order intents set BasisKey; we
// fall back to OutToken.Symbol so older single-chain code still works.
func swapBasisKey(s types.SwapIntent) string {
	if s.BasisKey != "" {
		return strings.ToUpper(s.BasisKey)
	}
	return strings.ToUpper(s.OutToken.Symbol)
}

// orderBasisKey returns the canonical basis-pair key for an order intent.
// Strategies that emit paired swap+order intents set BasisKey; we fall
// back to Symbol so single-symbol-equals-everything strategies still work.
func orderBasisKey(o types.OrderIntent) string {
	if o.BasisKey != "" {
		return strings.ToUpper(o.BasisKey)
	}
	return strings.ToUpper(o.Symbol)
}

// riskSnapshot builds a PortfolioSnapshot for risk evaluation. It includes
// the runtime's currently-tracked open basis positions PLUS phantom
// "opening" entries for symbols accepted earlier in this tick, so
// MaxConcurrentPositions accumulates intra-tick. excludeSymbol (uppercase)
// lets the perp leg of a paired open avoid double-counting its own swap.
func (r *Runtime) riskSnapshot(inflightOpens map[string]bool, excludeSymbol string) types.PortfolioSnapshot {
	snap := types.PortfolioSnapshot{
		Time:      r.now(),
		OpenBasis: r.snapshotOpenBasis(),
	}
	for sym := range inflightOpens {
		if sym == excludeSymbol {
			continue
		}
		snap.OpenBasis = append(snap.OpenBasis, types.BasisPosition{
			Underlying: sym,
			State:      types.BasisStateOpening,
		})
	}
	return snap
}

// isOpeningSwap reports whether a SwapIntent represents the spot leg of
// opening a new basis (USDC → non-USDC). Mirrors the helper in
// risk/policy.go so the runtime can count opens consistently.
func isOpeningSwap(s types.SwapIntent) bool {
	return s.InToken.Symbol == "USDC" && s.OutToken.Symbol != "USDC"
}

// reconcileOpenBasis maintains the runtime's in-memory view of open basis
// positions based on the intents this decision emitted AND per-leg success
// flags from the executor. Pairs are matched by the underlying symbol;
// only pairs where BOTH the spot swap and the perp order succeeded are
// marked open. When a Store is configured, every open is upserted and
// every close is recorded — so a daemon restart hydrates the same
// position set on the next NewRuntime.
//
// swapOK / orderOK are keyed by uppercased symbol (out-token for swaps,
// perp symbol for orders). nil maps mean "treat all as success" — used
// by tests that don't go through the executor.
func (r *Runtime) reconcileOpenBasis(ctx context.Context, now time.Time, decisionID string, dec strategy.Decision, swapOK, orderOK map[string]bool) {
	r.posMu.Lock()
	defer r.posMu.Unlock()

	// Closes first — for any reduce-only order where we already track an
	// open basis on that BasisKey (or perp symbol fallback), drop it.
	// This is the framework-side mirror of a typical basis-strategy close
	// (reduce-only buy + spot→USDC swap).
	for _, o := range dec.Orders {
		if !o.ReduceOnly {
			continue
		}
		key := orderBasisKey(o)
		bp, ok := r.openBasis[key]
		if !ok {
			continue
		}
		// Only close on a successful reduce-only order; a failed close
		// means the position is still open at the venue, so we must not
		// drop our local view.
		if orderOK != nil && !orderOK[key] {
			r.deps.Logger.Warn("close intent failed; basis remains open in our view",
				"agent_id", r.agent.ID, "underlying", bp.Underlying)
			continue
		}
		delete(r.openBasis, key)
		if r.deps.Store != nil {
			if err := r.deps.Store.CloseBasis(ctx, r.agent.ID, bp.Underlying); err != nil && !errors.Is(err, ErrNoOpenBasis) {
				r.deps.Logger.Warn("persist close basis failed",
					"agent_id", r.agent.ID, "underlying", bp.Underlying, "err", err)
			}
		}
	}

	// Opens — pair USDC→token swaps with non-reduce-only sell orders by
	// BasisKey. Strategies that don't set BasisKey fall back to symbol
	// matching (out-token symbol == perp symbol), which still works for
	// single-chain Solana entries where they coincide.
	swapsByKey := map[string]types.SwapIntent{}
	for _, s := range dec.Swaps {
		if s.InToken.Symbol == "USDC" && s.OutToken.Symbol != "USDC" {
			swapsByKey[swapBasisKey(s)] = s
		}
	}
	for _, o := range dec.Orders {
		if o.ReduceOnly || o.Side != types.SideSell {
			continue
		}
		key := orderBasisKey(o)
		s, ok := swapsByKey[key]
		if !ok {
			continue
		}
		if _, alreadyOpen := r.openBasis[key]; alreadyOpen {
			continue
		}
		// Both legs must have succeeded. A half-open basis (e.g. spot
		// filled but perp rejected) is dangerous to record as "open" —
		// the strategy would think it's hedged when it isn't. Log a
		// warning so an operator can intervene.
		spotSucceeded := swapOK == nil || swapOK[key]
		perpSucceeded := orderOK == nil || orderOK[key]
		if !spotSucceeded || !perpSucceeded {
			r.deps.Logger.Warn("partial open; skipping basis reconcile",
				"agent_id", r.agent.ID, "basis_key", key,
				"spot_ok", spotSucceeded, "perp_ok", perpSucceeded)
			continue
		}
		// underlying is the BasisKey when the strategy supplied one
		// (so multi-chain entries are uniquely named like ETH-BASE);
		// otherwise the perp symbol, preserving v1 behaviour.
		underlying := o.Symbol
		if o.BasisKey != "" {
			underlying = o.BasisKey
		}
		bp := types.BasisPosition{
			ID:         "bp:" + r.agent.ID + ":" + key + ":" + safeShortID(decisionID),
			AgentID:    r.agent.ID,
			Underlying: underlying,
			State:      types.BasisStateOpen,
			OpenedAt:   now,
			Legs: []types.BasisLeg{
				{Kind: types.BasisLegSpot, Asset: s.OutToken, Qty: s.InAmount},
				{Kind: types.BasisLegPerp, Symbol: o.Symbol, Qty: o.Size, AvgPrice: o.Price},
			},
		}
		r.openBasis[key] = bp
		if r.deps.Store != nil {
			if err := r.deps.Store.UpsertOpenBasis(ctx, bp); err != nil {
				r.deps.Logger.Warn("persist open basis failed",
					"agent_id", r.agent.ID, "underlying", o.Symbol, "err", err)
			}
		}
	}
}

// executeSwap returns true iff the swap was successfully recorded (paper)
// or confirmed on chain (live). A return of false means the basis pair
// MUST NOT be reconciled into the open-basis set — the position would be
// half-open at best, which is worse than retrying on the next tick.
func (r *Runtime) executeSwap(ctx context.Context, now time.Time, decisionID string, slot int, s types.SwapIntent, snap types.PortfolioSnapshot) bool {
	if s.ClientID == "" {
		s.ClientID = clientIDFromSwap(r.agent.ID, decisionID, slot, s)
	}
	if r.deps.Risk != nil {
		v := r.deps.Risk.PreTrade(ctx, r.agent.ID, s, snap)
		if v.IsBlock() {
			r.deps.Logger.Warn("swap blocked by risk",
				"agent_id", r.agent.ID,
				"reason", v.Reason,
				"detail", v.Detail,
				"out_token", s.OutToken.Symbol)
			return false
		}
	}
	venue := r.deps.SwapVenueForChain(s.Chain)
	if r.agent.Mode == ModePaper || venue == nil {
		dex := ""
		if venue != nil {
			dex = venue.Name()
		}
		r.recordSwapWithDEX(ctx, now, decisionID, s, dex, "", types.SwapStatusConfirmed, true, s.InAmount, decimal.Zero)
		return true
	}

	q, err := venue.Quote(ctx, types.QuoteRequest{
		InToken: s.InToken, OutToken: s.OutToken, Amount: s.InAmount, Mode: types.QuoteExactIn,
	})
	if err != nil {
		r.deps.Logger.Error("swap quote failed", "chain", s.Chain, "err", err)
		r.recordSwapWithDEX(ctx, now, decisionID, s, venue.Name(), "", types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	tx, err := venue.Swap(ctx, q, s.SlippageBps)
	if err != nil {
		r.deps.Logger.Error("swap submit failed", "chain", s.Chain, "err", err)
		r.recordSwapWithDEX(ctx, now, decisionID, s, venue.Name(), "", types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	res, err := venue.WaitConfirm(ctx, tx)
	if err != nil {
		r.deps.Logger.Error("swap confirm failed", "chain", s.Chain, "err", err)
		r.recordSwapWithDEX(ctx, now, decisionID, s, venue.Name(), string(tx), types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	r.recordSwapWithDEX(ctx, now, decisionID, s, venue.Name(), string(res.TxHash), res.Status, false, res.OutAmount, res.GasNative)
	return res.Status == types.SwapStatusConfirmed
}

// executeOrder returns true iff the order was accepted (paper "open" or
// live ack with non-rejected status). False means the basis pair must
// not be reconciled.
func (r *Runtime) executeOrder(ctx context.Context, now time.Time, decisionID string, slot int, o types.OrderIntent, snap types.PortfolioSnapshot) bool {
	if o.ClientID == "" {
		o.ClientID = types.DeriveClientID(r.agent.ID, decisionID, slot, o.Venue, o.Symbol, o.Side, o.Size)
	}
	o.DecisionID = decisionID
	o.Slot = slot
	if r.deps.Risk != nil {
		v := r.deps.Risk.PreTrade(ctx, r.agent.ID, o, snap)
		if v.IsBlock() {
			r.deps.Logger.Warn("order blocked by risk",
				"agent_id", r.agent.ID,
				"reason", v.Reason,
				"detail", v.Detail,
				"symbol", o.Symbol)
			return false
		}
	}
	if r.agent.Mode == ModePaper || r.deps.Perp == nil {
		r.recordOrder(ctx, now, decisionID, o, "", string(types.OrderStatusOpen), true)
		return true
	}
	ack, err := r.deps.Perp.Place(ctx, o)
	if err != nil {
		r.deps.Logger.Error("order place failed", "err", err)
		r.recordOrder(ctx, now, decisionID, o, "", string(types.OrderStatusRejected), false)
		return false
	}
	r.recordOrder(ctx, now, decisionID, o, string(ack.ID), string(ack.Status), false)
	return ack.Status != types.OrderStatusRejected
}

func (r *Runtime) executeCancel(ctx context.Context, id types.OrderID) {
	if r.agent.Mode == ModePaper || r.deps.Perp == nil {
		return
	}
	if err := r.deps.Perp.Cancel(ctx, id); err != nil {
		r.deps.Logger.Error("order cancel failed", "id", id, "err", err)
	}
}

func (r *Runtime) recordSwap(ctx context.Context, now time.Time, decisionID string, s types.SwapIntent, txHash string, status types.SwapStatus, paper bool, outAmt, gas decimal.Decimal) {
	r.recordSwapWithDEX(ctx, now, decisionID, s, "", txHash, status, paper, outAmt, gas)
}

// recordSwapWithDEX persists a swap with an explicit DEX label (the
// venue's Name() if known). The variant exists so multi-chain routing
// can record the right venue per chain instead of always pointing at
// the first one in the map.
func (r *Runtime) recordSwapWithDEX(ctx context.Context, now time.Time, decisionID string, s types.SwapIntent, dex, txHash string, status types.SwapStatus, paper bool, outAmt, gas decimal.Decimal) {
	if r.deps.Store == nil {
		return
	}
	if dex == "" {
		dex = "noop"
	}
	if err := r.deps.Store.PersistSwap(ctx, PersistSwapInput{
		Time:        now,
		AgentID:     r.agent.ID,
		DecisionID:  decisionID,
		Chain:       string(s.Chain),
		DEX:         dex,
		InToken:     s.InToken.Symbol,
		OutToken:    s.OutToken.Symbol,
		InAmount:    s.InAmount,
		OutAmount:   outAmt,
		SlippageBps: s.SlippageBps,
		GasPaid:     gas,
		TxHash:      txHash,
		Status:      string(status),
		Paper:       paper,
		ClientID:    s.ClientID, // deterministic per-intent (clientIDFromSwap)
	}); err != nil {
		r.deps.Logger.Warn("persist swap failed", "err", err)
	}
}

func (r *Runtime) recordOrder(ctx context.Context, now time.Time, decisionID string, o types.OrderIntent, venueID, status string, paper bool) {
	if r.deps.Store == nil {
		return
	}
	if err := r.deps.Store.PersistOrder(ctx, PersistOrderInput{
		Time:       now,
		AgentID:    r.agent.ID,
		DecisionID: decisionID,
		Venue:      o.Venue,
		Symbol:     o.Symbol,
		Side:       string(o.Side),
		Type:       string(o.Type),
		Price:      o.Price,
		Size:       o.Size,
		TIF:        string(o.TIF),
		ReduceOnly: o.ReduceOnly,
		ClientID:   string(o.ClientID),
		VenueID:    venueID,
		Status:     status,
		Paper:      paper,
	}); err != nil {
		r.deps.Logger.Warn("persist order failed", "err", err)
	}
}

// decisionInputHash produces a short fingerprint of the input so an operator
// can join orders/decisions to their input snapshot in audits.
func decisionInputHash(in strategy.DecisionInput) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%d", in.AgentID, in.Now.UnixNano(), len(in.PerpPositions), len(in.SpotBalances), len(in.BasisPositions))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// safeShortID returns the first 8 chars of id, or all of id if shorter.
// Avoids panicking on short ids during tests / unusual decisionIDs.
func safeShortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// clientIDFromSwap creates a deterministic client_id for a SwapIntent.
func clientIDFromSwap(agentID, decisionID string, slot int, s types.SwapIntent) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%s|%s|%s",
		agentID, decisionID, slot, s.Chain, s.InToken.Mint, s.OutToken.Mint)
	return "sw_" + hex.EncodeToString(h.Sum(nil))[:24]
}
