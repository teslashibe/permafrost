// Package bittensor polls Subtensor on-chain pool reserves and emits
// synthetic types.MarketEvent ticks for each subnet alpha token.
//
// There is no orderbook — price is purely AMM-derived. The feed synthesises
// a Tick with mid = pool price and a spread derived from estimated slippage
// for a configurable reference notional size.
package bittensor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Feed polls Subtensor pool state and emits MarketEvent ticks.
type Feed struct {
	rpc             *btchain.Client
	interval        time.Duration
	referenceNotional decimal.Decimal // TAO amount used to estimate spread
	log             *slog.Logger
}

// Config for the Bittensor market feed.
type Config struct {
	RPCURL            string
	PollInterval      time.Duration   // default 12s (one Bittensor block)
	ReferenceNotional decimal.Decimal // TAO amount for slippage spread; default 10
}

// NewFeed constructs a Feed.
func NewFeed(cfg Config, log *slog.Logger) *Feed {
	interval := cfg.PollInterval
	if interval == 0 {
		interval = 12 * time.Second
	}
	refNotional := cfg.ReferenceNotional
	if refNotional.IsZero() {
		refNotional = decimal.NewFromInt(10)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Feed{
		rpc:             btchain.NewClient(cfg.RPCURL),
		interval:        interval,
		referenceNotional: refNotional,
		log:             log,
	}
}

// Subscribe starts polling and emits MarketEvents on the returned channel.
// The channel is closed when ctx is cancelled. The caller specifies which
// subnet netuids to track.
func (f *Feed) Subscribe(ctx context.Context, netuids []uint16) (<-chan types.MarketEvent, error) {
	ch := make(chan types.MarketEvent, len(netuids)*2)

	wanted := make(map[uint16]bool, len(netuids))
	for _, n := range netuids {
		wanted[n] = true
	}

	go func() {
		defer close(ch)
		tick := time.NewTicker(f.interval)
		defer tick.Stop()

		// Emit immediately on first call, then on every tick.
		f.poll(ctx, wanted, ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				f.poll(ctx, wanted, ch)
			}
		}
	}()

	return ch, nil
}

func (f *Feed) poll(ctx context.Context, wanted map[uint16]bool, ch chan<- types.MarketEvent) {
	subnets, err := f.rpc.GetSubnetsInfo(ctx)
	if err != nil {
		f.log.Warn("bittensor feed: poll failed", "err", err)
		return
	}

	now := time.Now().UTC()
	for _, sn := range subnets {
		if len(wanted) > 0 && !wanted[sn.Netuid] {
			continue
		}
		if sn.TaoReserve.IsZero() || sn.AlphaReserve.IsZero() {
			continue
		}

		mid := sn.AlphaPrice
		spread := estimateSpread(sn.TaoReserve, sn.AlphaReserve, f.referenceNotional)
		halfSpread := spread.Div(decimal.NewFromInt(2))

		symbol := fmt.Sprintf("SN%d/TAO", sn.Netuid)
		ev := types.MarketEvent{
			Kind: types.EventTick,
			Tick: &types.Tick{
				Time:   now,
				Venue:  "bittensor",
				Symbol: symbol,
				Bid:    mid.Sub(halfSpread),
				Ask:    mid.Add(halfSpread),
				Last:   mid,
			},
		}

		select {
		case ch <- ev:
		case <-ctx.Done():
			return
		default:
			// Drop if channel is full — caller is too slow.
		}
	}
}

// estimateSpread derives a synthetic bid-ask spread from the AMM curve.
// For a constant-product AMM, price impact ≈ amount / reserve. We use
// the reference notional to get a realistic spread.
func estimateSpread(taoReserve, alphaReserve, refNotional decimal.Decimal) decimal.Decimal {
	if taoReserve.IsZero() {
		return decimal.Zero
	}
	mid := taoReserve.Div(alphaReserve)
	impact := refNotional.Div(taoReserve)
	return mid.Mul(impact)
}
