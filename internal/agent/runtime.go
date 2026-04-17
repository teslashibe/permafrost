package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/internal/strategy"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/types"
)

// Deps bundles the dependencies a Runtime needs to execute one Agent.
// The intention is one Runtime per Agent; the Supervisor builds Deps from
// the global registries (venues, inference, etc.).
type Deps struct {
	Strategy   strategy.Strategy
	Perp       exchange.Venue
	Swap       swap.SwapVenue
	Inference  inference.Provider
	Risk       risk.Engine
	Store      *Store
	Logger     *slog.Logger
}

// Runtime drives the tick loop for one Agent.
type Runtime struct {
	agent Agent
	deps  Deps
	now   func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

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
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.running = true
	go r.loop(ctx)
	return nil
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

	// One concise per-tick log so operators can see the daemon working
	// without enabling debug. This is the only INFO line emitted per tick.
	r.deps.Logger.Info("tick",
		"agent_id", r.agent.ID,
		"decision_id", decisionID,
		"swaps", len(dec.Swaps),
		"orders", len(dec.Orders),
		"cancels", len(dec.Cancels),
		"open_basis", len(in.BasisPositions),
	)
	return dec, nil
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
// — this is the spot-first invariant called for in SCOPE.md §11.
//
// Each leg's success is tracked per-symbol so reconcileOpenBasis only
// marks a basis "open" when BOTH legs succeeded. A failed swap or order
// is recorded in the audit ledger (orders / swaps tables) with a failed
// status but does not produce a basis-position row — leaving the strategy
// free to retry on the next tick instead of believing it already has the
// position.
func (r *Runtime) executeSwapsThenOrders(ctx context.Context, now time.Time, decisionID string, _ strategy.DecisionInput, dec strategy.Decision) {
	swapOK := map[string]bool{}  // keyed by uppercased OUT-token symbol
	orderOK := map[string]bool{} // keyed by uppercased perp symbol

	for i, s := range dec.Swaps {
		if r.executeSwap(ctx, now, decisionID, i, s) {
			swapOK[strings.ToUpper(s.OutToken.Symbol)] = true
		}
	}
	for i, o := range dec.Orders {
		if r.executeOrder(ctx, now, decisionID, i, o) {
			orderOK[strings.ToUpper(o.Symbol)] = true
		}
	}
	for _, c := range dec.Cancels {
		r.executeCancel(ctx, c)
	}
	r.reconcileOpenBasis(ctx, now, decisionID, dec, swapOK, orderOK)
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
	// open basis on that symbol, drop it. This matches funding_arb_basic's
	// closeIntents shape (reduce-only buy + spot→USDC swap).
	for _, o := range dec.Orders {
		if !o.ReduceOnly {
			continue
		}
		key := strings.ToUpper(o.Symbol)
		if _, ok := r.openBasis[key]; !ok {
			continue
		}
		// Only close on a successful reduce-only order; a failed close
		// means the position is still open at the venue, so we must not
		// drop our local view.
		if orderOK != nil && !orderOK[key] {
			r.deps.Logger.Warn("close intent failed; basis remains open in our view",
				"agent_id", r.agent.ID, "underlying", o.Symbol)
			continue
		}
		delete(r.openBasis, key)
		if r.deps.Store != nil {
			if err := r.deps.Store.CloseBasis(ctx, r.agent.ID, o.Symbol); err != nil && !errors.Is(err, ErrNoOpenBasis) {
				r.deps.Logger.Warn("persist close basis failed",
					"agent_id", r.agent.ID, "underlying", o.Symbol, "err", err)
			}
		}
	}

	// Opens — pair non-reduce-only sell orders with USDC→token swaps in the
	// same Decision.
	swapsByOut := map[string]types.SwapIntent{}
	for _, s := range dec.Swaps {
		if s.InToken.Symbol == "USDC" && s.OutToken.Symbol != "USDC" {
			swapsByOut[strings.ToUpper(s.OutToken.Symbol)] = s
		}
	}
	for _, o := range dec.Orders {
		if o.ReduceOnly || o.Side != types.SideSell {
			continue
		}
		key := strings.ToUpper(o.Symbol)
		s, ok := swapsByOut[key]
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
				"agent_id", r.agent.ID, "underlying", o.Symbol,
				"spot_ok", spotSucceeded, "perp_ok", perpSucceeded)
			continue
		}
		bp := types.BasisPosition{
			ID:         "bp:" + r.agent.ID + ":" + key + ":" + safeShortID(decisionID),
			AgentID:    r.agent.ID,
			Underlying: o.Symbol,
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
func (r *Runtime) executeSwap(ctx context.Context, now time.Time, decisionID string, slot int, s types.SwapIntent) bool {
	if s.ClientID == "" {
		s.ClientID = clientIDFromSwap(r.agent.ID, decisionID, slot, s)
	}
	if r.deps.Risk != nil {
		v := r.deps.Risk.PreTrade(ctx, r.agent.ID, s, types.PortfolioSnapshot{})
		if v.IsBlock() {
			r.deps.Logger.Warn("swap blocked by risk", "agent_id", r.agent.ID, "reason", v.Reason)
			return false
		}
	}
	if r.agent.Mode == ModePaper || r.deps.Swap == nil {
		r.recordSwap(ctx, now, decisionID, s, "", types.SwapStatusConfirmed, true, s.InAmount, decimal.Zero)
		return true
	}

	q, err := r.deps.Swap.Quote(ctx, types.QuoteRequest{
		InToken: s.InToken, OutToken: s.OutToken, Amount: s.InAmount, Mode: types.QuoteExactIn,
	})
	if err != nil {
		r.deps.Logger.Error("swap quote failed", "err", err)
		r.recordSwap(ctx, now, decisionID, s, "", types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	tx, err := r.deps.Swap.Swap(ctx, q, s.SlippageBps)
	if err != nil {
		r.deps.Logger.Error("swap submit failed", "err", err)
		r.recordSwap(ctx, now, decisionID, s, "", types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	res, err := r.deps.Swap.WaitConfirm(ctx, tx)
	if err != nil {
		r.deps.Logger.Error("swap confirm failed", "err", err)
		r.recordSwap(ctx, now, decisionID, s, string(tx), types.SwapStatusFailed, false, decimal.Zero, decimal.Zero)
		return false
	}
	r.recordSwap(ctx, now, decisionID, s, string(res.TxHash), res.Status, false, res.OutAmount, res.GasNative)
	return res.Status == types.SwapStatusConfirmed
}

// executeOrder returns true iff the order was accepted (paper "open" or
// live ack with non-rejected status). False means the basis pair must
// not be reconciled.
func (r *Runtime) executeOrder(ctx context.Context, now time.Time, decisionID string, slot int, o types.OrderIntent) bool {
	if o.ClientID == "" {
		o.ClientID = types.DeriveClientID(r.agent.ID, decisionID, slot, o.Venue, o.Symbol, o.Side, o.Size)
	}
	o.DecisionID = decisionID
	o.Slot = slot
	if r.deps.Risk != nil {
		v := r.deps.Risk.PreTrade(ctx, r.agent.ID, o, types.PortfolioSnapshot{})
		if v.IsBlock() {
			r.deps.Logger.Warn("order blocked by risk", "agent_id", r.agent.ID, "reason", v.Reason)
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
	if r.deps.Store == nil {
		return
	}
	dex := "noop"
	if r.deps.Swap != nil {
		dex = r.deps.Swap.Name()
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
