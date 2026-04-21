// Package bittensor polls Subtensor on-chain alpha prices and emits
// synthetic types.MarketEvent ticks for each subnet.
//
// There is no Bittensor orderbook — price is purely AMM-derived. The
// feed synthesises a Tick with mid = current alpha price (from
// swap_currentAlphaPrice) and spreads bid/ask by an estimated slippage
// for a configurable reference notional size. Optionally writes each
// poll to the subnet_pool_snapshots Timescale hypertable for backtest.
package bittensor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Feed polls Subtensor pool state and emits MarketEvent ticks.
type Feed struct {
	rpc               *btchain.Client
	interval          time.Duration
	referenceNotional decimal.Decimal // TAO amount used to estimate spread
	db                *pgxpool.Pool   // optional, for snapshot persistence
	log               *slog.Logger
}

// Config for the Bittensor market feed.
type Config struct {
	RPCURL            string
	PollInterval      time.Duration   // default 12s (one Bittensor block)
	ReferenceNotional decimal.Decimal // TAO amount for slippage spread; default 10
	DB                *pgxpool.Pool   // if set, every poll persists a snapshot
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
		rpc:               btchain.NewClient(cfg.RPCURL),
		interval:          interval,
		referenceNotional: refNotional,
		db:                cfg.DB,
		log:               log,
	}
}

// Subscribe starts polling and emits MarketEvents on the returned channel.
// The channel is closed when ctx is cancelled. Pass an empty netuids slice
// to track every active subnet.
func (f *Feed) Subscribe(ctx context.Context, netuids []uint16) (<-chan types.MarketEvent, error) {
	ch := make(chan types.MarketEvent, 256)

	go func() {
		defer close(ch)
		tick := time.NewTicker(f.interval)
		defer tick.Stop()

		f.poll(ctx, netuids, ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				f.poll(ctx, netuids, ch)
			}
		}
	}()

	return ch, nil
}

func (f *Feed) poll(ctx context.Context, netuids []uint16, ch chan<- types.MarketEvent) {
	now := time.Now().UTC()
	targets := netuids
	if len(targets) == 0 {
		// No explicit list: discover via SubnetCount and iterate.
		count, err := f.rpc.SubnetCount(ctx)
		if err != nil {
			f.log.Warn("bittensor feed: subnet count failed", "err", err)
			return
		}
		targets = make([]uint16, 0, count)
		for i := uint16(1); i <= count; i++ {
			targets = append(targets, i)
		}
	}

	for _, netuid := range targets {
		if ctx.Err() != nil {
			return
		}
		price, err := f.rpc.CurrentAlphaPrice(ctx, netuid)
		if err != nil {
			f.log.Debug("bittensor feed: skip subnet", "netuid", netuid, "err", err)
			continue
		}
		// Synthetic spread: AMM impact for our reference notional.
		// We use a simulation call to get the realised slippage rather
		// than guessing — this ensures the displayed spread matches
		// what an actual trade would experience.
		halfSpread := f.estimateHalfSpread(ctx, netuid, price)

		symbol := fmt.Sprintf("SN%d/TAO", netuid)
		ev := types.MarketEvent{
			Kind: types.EventTick,
			Tick: &types.Tick{
				Time:   now,
				Venue:  "bittensor",
				Symbol: symbol,
				Bid:    price.Sub(halfSpread),
				Ask:    price.Add(halfSpread),
				Last:   price,
			},
		}

		select {
		case ch <- ev:
		case <-ctx.Done():
			return
		default:
			// Drop if channel is full — caller is too slow.
		}

		// Persist snapshot if DB wired.
		if f.db != nil {
			f.writeSnapshot(ctx, now, netuid, price)
		}
	}
}

// estimateHalfSpread derives a half-spread by comparing the spot price
// against what the AMM would actually fill for the reference notional.
// Falls back to a small fraction of price if the simulation fails.
func (f *Feed) estimateHalfSpread(ctx context.Context, netuid uint16, spot decimal.Decimal) decimal.Decimal {
	refRao := btchain.TAOToRAO(f.referenceNotional)
	sim, err := f.rpc.SimSwapTaoForAlpha(ctx, netuid, refRao)
	if err != nil {
		// Fallback: 0.5% of mid as a reasonable guess for AMM venues.
		return spot.Mul(decimal.NewFromFloat(0.005))
	}
	if sim.AmountOut == 0 {
		return spot.Mul(decimal.NewFromFloat(0.005))
	}
	actualPrice := f.referenceNotional.Div(btchain.RAOToTAO(sim.AmountOut))
	if actualPrice.LessThanOrEqual(spot) {
		return decimal.Zero
	}
	return actualPrice.Sub(spot)
}

// writeSnapshot best-effort inserts a row into subnet_pool_snapshots.
// Errors are logged but never fail the feed — observability data is
// not critical-path.
func (f *Feed) writeSnapshot(ctx context.Context, t time.Time, netuid uint16, price decimal.Decimal) {
	const q = `INSERT INTO subnet_pool_snapshots (time, netuid, tao_reserve, alpha_reserve, alpha_price_tao)
	           VALUES ($1, $2, $3, $4, $5)`
	// We don't have the raw reserves cheaply (they would require a
	// separate getDynamicInfo call); record price only and put zeros
	// in the reserve columns. Reserve enrichment is a follow-up if
	// strategies actually need it.
	_, err := f.db.Exec(ctx, q, t, int16(netuid), 0, 0, price)
	if err != nil {
		f.log.Debug("bittensor feed: snapshot insert failed", "netuid", netuid, "err", err)
	}
}
