package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/pkg/types"
)

// KillSwitchOptions controls the aggressiveness of an emergency stop.
//
// The metaphor (per epic #30): Frostbite the kraken stirring from his
// sleep beneath the ice. CloseShorts is "wake quietly and recall the
// expedition"; LiquidateSpot is "full Whiteout — leave nothing on the
// field."
type KillSwitchOptions struct {
	// CloseShorts: send reduce-only market orders to flatten any open
	// perp positions. Always recommended.
	CloseShorts bool
	// LiquidateSpot: in addition to closing perps, swap every open
	// spot leg back to USDC via the configured SwapVenue for that
	// chain. Requires SwapVenues to be wired into Deps; if a chain
	// has no SwapVenue, the corresponding leg logs a warning and is
	// left in place (operator decides whether to liquidate manually).
	LiquidateSpot bool
	// SpotSlippageBps caps the slippage tolerance on liquidation
	// swaps. Zero means "use the agent's max_spot_slippage_bps risk
	// limit". A killswitch is by definition urgent — slippage budgets
	// here are typically wider than the strategy's normal entry/exit.
	SpotSlippageBps int
	// Reason is recorded on the agent run.
	Reason string
}

// DefaultKillSwitchOptions returns a safe default: stop the loop, cancel
// open orders, and flatten short positions. Spot is left in place by
// default — operators flip LiquidateSpot true once they've configured a
// SwapVenue for every chain they hold spot on.
func DefaultKillSwitchOptions(reason string) KillSwitchOptions {
	return KillSwitchOptions{
		CloseShorts: true,
		Reason:      reason,
	}
}

// Trip executes the kill switch on a Runtime: stops the loop, cancels
// open orders on the perp venue, and (per opts) flattens positions
// and liquidates spot.
//
// Best-effort: failures in any step are logged but do not abort the
// remaining steps. The first error encountered is returned so the
// caller knows something went wrong; the rest is in the log.
func (r *Runtime) Trip(ctx context.Context, opts KillSwitchOptions) error {
	if err := r.Stop(ctx, "kill_switch:"+opts.Reason); err != nil {
		r.deps.Logger.Error("kill_switch: stop failed", "err", err)
	}
	if r.deps.Store != nil {
		_ = r.deps.Store.SetStatus(ctx, r.agent.ID, StatusHalted)
	}
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if r.deps.Perp != nil {
		if err := cancelOpenOrders(ctx, r.deps.Perp, r.deps.Logger); err != nil {
			r.deps.Logger.Error("kill_switch: cancel orders failed", "err", err)
			record(err)
		}
		if opts.CloseShorts {
			if err := flattenPositions(ctx, r.agent.ID, r.deps.Perp, r.deps.Logger); err != nil {
				r.deps.Logger.Error("kill_switch: flatten failed", "err", err)
				record(err)
			}
		}
	}

	if opts.LiquidateSpot {
		if err := r.liquidateSpot(ctx, opts); err != nil {
			r.deps.Logger.Error("kill_switch: spot liquidation failed", "err", err)
			record(err)
		}
	}

	return firstErr
}

// cancelOpenOrders enumerates every open order on the venue and cancels
// each one. Errors per-order are logged and the loop continues; the
// first error is returned so the caller knows something failed.
//
// Venues that haven't yet implemented OpenOrders return an empty slice;
// in that case this is a no-op, which is the right answer (we'd rather
// silently no-op than abort the rest of the killswitch sequence).
func cancelOpenOrders(ctx context.Context, v exchange.Venue, log *slog.Logger) error {
	orders, err := v.OpenOrders(ctx)
	if err != nil {
		return fmt.Errorf("open_orders: %w", err)
	}
	if len(orders) == 0 {
		log.Info("kill_switch: no open orders to cancel")
		return nil
	}
	var firstErr error
	for _, o := range orders {
		// Some venues (Hyperliquid) need (oid, symbol) for cancel. The
		// venue's session-memory rememberOrder hook (populated by
		// OpenOrders or Place) covers the common case; v.Cancel handles
		// any additional plumbing internally.
		if err := v.Cancel(ctx, o.ID); err != nil {
			log.Error("kill_switch: cancel failed", "order_id", o.ID, "symbol", o.Symbol, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Info("kill_switch: cancelled", "order_id", o.ID, "symbol", o.Symbol, "side", o.Side)
	}
	return firstErr
}

// flattenPositions sends reduce-only market orders to close every open
// perp position. Best-effort: a failure on one symbol does not stop the
// rest.
func flattenPositions(ctx context.Context, agentID string, v exchange.Venue, log *slog.Logger) error {
	positions, err := v.Positions(ctx)
	if err != nil {
		return fmt.Errorf("positions: %w", err)
	}
	var firstErr error
	for _, p := range positions {
		if p.IsFlat() {
			continue
		}
		side := types.SideSell
		size := p.Qty
		if p.IsShort() {
			side = types.SideBuy
			size = p.Qty.Abs()
		}
		intent := types.OrderIntent{
			Venue:      v.Name(),
			Symbol:     p.Symbol,
			Side:       side,
			Type:       types.OrderTypeMarket,
			Size:       size,
			ReduceOnly: true,
			ClientID:   types.DeriveClientID(agentID, "kill_switch", 0, v.Name(), p.Symbol, side, size),
		}
		if _, err := v.Place(ctx, intent); err != nil {
			log.Error("kill_switch: flatten place failed", "symbol", p.Symbol, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Info("kill_switch: flatten submitted", "symbol", p.Symbol, "side", side, "size", size)
	}
	return firstErr
}

// liquidateSpot swaps every open spot leg back to USDC via the
// configured SwapVenue for that chain. Best-effort per-leg.
//
// Each leg becomes a SwapIntent with:
//   - InToken  = the spot asset
//   - OutToken = USDC on the same chain (resolved via assets.USDCMintFor)
//   - InAmount = the leg's quantity
//   - SlippageBps = max(opts.SpotSlippageBps, agent's MaxSpotSlippageBps)
//   - Tag      = "kill_switch:liquidate"
//
// If a chain has no SwapVenue configured (paper-spot fallback or just
// not wired), the leg logs a warning and is left in place — the operator
// can liquidate manually. If no USDC mapping exists for a chain, same.
func (r *Runtime) liquidateSpot(ctx context.Context, opts KillSwitchOptions) error {
	if r.deps.Store == nil {
		return errors.New("liquidate_spot: store not configured (no open basis to enumerate)")
	}
	open, err := r.deps.Store.LoadOpenBasis(ctx, r.agent.ID)
	if err != nil {
		return fmt.Errorf("load open basis: %w", err)
	}
	if len(open) == 0 {
		r.deps.Logger.Info("kill_switch: no open spot legs to liquidate")
		return nil
	}

	// Slippage: explicit opts wins; else fall back to the agent's
	// MaxSpotSlippageBps risk limit; else 100bps as a safety ceiling.
	slip := opts.SpotSlippageBps
	if slip == 0 && r.deps.Risk != nil {
		if l := r.deps.Risk.Limits().MaxSpotSlippageBps; l > 0 {
			slip = l
		}
	}
	if slip == 0 {
		slip = 100
	}

	var firstErr error
	for _, bp := range open {
		for _, leg := range bp.Legs {
			if leg.Kind != types.BasisLegSpot || leg.Qty.IsZero() {
				continue
			}
			venue := r.deps.SwapVenueForChain(leg.Asset.Chain)
			if venue == nil {
				r.deps.Logger.Warn("kill_switch: no SwapVenue for chain; spot leg left in place",
					"chain", leg.Asset.Chain, "basis_key", bp.Underlying, "qty", leg.Qty)
				continue
			}
			usdc, ok := assets.USDCAsset(leg.Asset.Chain)
			if !ok {
				r.deps.Logger.Warn("kill_switch: no USDC mapping for chain; spot leg left in place",
					"chain", leg.Asset.Chain, "basis_key", bp.Underlying)
				continue
			}
			// Quote → Swap (we don't WaitConfirm here; killswitch is
			// fire-and-forget by design — operator can inspect tx
			// hashes from the decision log if they care to follow up).
			quote, err := venue.Quote(ctx, types.QuoteRequest{
				InToken:  leg.Asset,
				OutToken: usdc,
				Amount:   leg.Qty,
				Mode:     types.QuoteExactIn,
			})
			if err != nil {
				r.deps.Logger.Error("kill_switch: liquidate quote failed",
					"basis_key", bp.Underlying, "chain", leg.Asset.Chain, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			tx, err := venue.Swap(ctx, quote, slip)
			if err != nil {
				r.deps.Logger.Error("kill_switch: liquidate swap failed",
					"basis_key", bp.Underlying, "chain", leg.Asset.Chain, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			r.deps.Logger.Info("kill_switch: liquidate submitted",
				"basis_key", bp.Underlying, "chain", leg.Asset.Chain,
				"in_amount", leg.Qty, "slippage_bps", slip, "tx", tx)
		}
	}
	return firstErr
}

// _ keeps the swap import live even if liquidateSpot's closure compiles
// to a path that doesn't reference swap.SwapVenue directly.
var _ swap.SwapVenue = nil

// ErrAgentNotRunning is returned when a kill-switch operation expects a
// running agent.
var ErrAgentNotRunning = errors.New("agent: not running")
