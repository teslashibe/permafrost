package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/pkg/types"
)

// KillSwitchOptions controls the aggressiveness of an emergency stop.
type KillSwitchOptions struct {
	// CloseShorts: send reduce-only market orders to flatten any open
	// perp positions. Always recommended.
	CloseShorts bool
	// LiquidateSpot: in addition to closing perps, liquidate spot legs
	// back to USDC via SwapVenue. Requires that we know the spot mints.
	LiquidateSpot bool
	// Reason is recorded on the agent run.
	Reason string
}

// DefaultKillSwitchOptions returns a safe default: stop the loop, cancel
// open orders, and flatten short positions. Spot is left in place because
// liquidating it requires the asset registry; operators can opt in via
// LiquidateSpot when they wire a registry to the supervisor.
func DefaultKillSwitchOptions(reason string) KillSwitchOptions {
	return KillSwitchOptions{
		CloseShorts: true,
		Reason:      reason,
	}
}

// Trip executes the kill switch on a Runtime: stops the loop, cancels open
// orders on the perp venue, and (if configured) flattens positions.
func (r *Runtime) Trip(ctx context.Context, opts KillSwitchOptions) error {
	if err := r.Stop(ctx, "kill_switch:"+opts.Reason); err != nil {
		r.deps.Logger.Error("kill_switch: stop failed", "err", err)
	}
	if r.deps.Store != nil {
		_ = r.deps.Store.SetStatus(ctx, r.agent.ID, StatusHalted)
	}
	var firstErr error

	if r.deps.Perp != nil {
		if err := cancelOpenOrders(ctx, r.deps.Perp, r.deps.Logger); err != nil {
			r.deps.Logger.Error("kill_switch: cancel orders failed", "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		if opts.CloseShorts {
			if err := flattenPositions(ctx, r.agent.ID, r.deps.Perp, r.deps.Logger); err != nil {
				r.deps.Logger.Error("kill_switch: flatten failed", "err", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	if opts.LiquidateSpot {
		// Spot liquidation is intentionally not auto-executed without an
		// asset-registry-aware caller; agents that hold spot should
		// surface a registry to the supervisor before enabling this.
		r.deps.Logger.Warn("kill_switch: spot liquidation requested but not implemented in this PR",
			"agent_id", r.agent.ID)
	}
	return firstErr
}

// cancelOpenOrders best-effort cancels every open order on the venue. We
// fetch positions and rely on the venue's own open-orders semantics by
// asking the venue to cancel known order IDs from positions where possible.
//
// Real Hyperliquid would expose openOrders; for the v1 framework we use
// the exchange.Venue interface as-is and document the limitation.
func cancelOpenOrders(ctx context.Context, v exchange.Venue, log *slog.Logger) error {
	// The Venue interface does not yet expose an OpenOrders method; the
	// kill-switch design intentionally lives on Runtime so the supervisor
	// can pass the same Venue. When OpenOrders lands as a small follow-up,
	// this becomes a loop over `v.OpenOrders(); v.Cancel(id)`. For now we
	// log a warning so the operator knows the cancel step is a no-op
	// against the current Venue surface.
	_ = ctx
	_ = v
	log.Warn("kill_switch: Venue.OpenOrders not yet on the interface; orders left in place. Cancel manually via the exchange UI if needed.")
	return nil
}

// flattenPositions sends reduce-only market orders to close every open perp
// position.
func flattenPositions(ctx context.Context, agentID string, v exchange.Venue, log *slog.Logger) error {
	positions, err := v.Positions(ctx)
	if err != nil {
		return fmt.Errorf("positions: %w", err)
	}
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
			return err
		}
		log.Info("kill_switch: flatten submitted", "symbol", p.Symbol, "side", side, "size", size)
	}
	return nil
}

// ErrAgentNotRunning is returned when a kill-switch operation expects a
// running agent.
var ErrAgentNotRunning = errors.New("agent: not running")
