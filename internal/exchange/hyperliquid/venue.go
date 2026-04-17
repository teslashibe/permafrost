package hyperliquid

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// Venue is the Hyperliquid implementation of exchange.Venue.
//
// Read-only methods (Subscribe, Positions, Balances, FundingRates) work
// without a Signer. Place and Cancel require WithSigner; the EIP-712
// action-signing flow itself lands in M4.
type Venue struct {
	cfg    Config
	client *Client
	signer wallet.Signer

	// metaCache caches the universe ↔ index mapping; refreshed lazily.
	metaMu       sync.Mutex
	metaIndex    map[string]int        // coin → universe index
	metaSnapshot *metaAndAssetCtxsResp // last fetched
	metaAt       time.Time
}

// Option configures a Venue at construction time.
type Option func(*Venue)

// WithSigner installs a chain signer. Only required for Place/Cancel.
func WithSigner(s wallet.Signer) Option {
	return func(v *Venue) { v.signer = s }
}

// New constructs a Venue from config. Returns an error on invalid config or
// unknown network.
func New(cfg Config, opts ...Option) (*Venue, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	ep, err := cfg.endpoints()
	if err != nil {
		return nil, err
	}
	v := &Venue{
		cfg:    cfg,
		client: NewClient(ep),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v, nil
}

// Compile-time check.
var _ exchange.Venue = (*Venue)(nil)

func (v *Venue) Name() string { return VenueName }

// Positions returns currently-open positions across all coins. Requires
// Address to be configured.
func (v *Venue) Positions(ctx context.Context) ([]types.Position, error) {
	if v.cfg.Address == "" {
		return nil, ErrAddressRequired
	}
	var out clearinghouseStateResp
	if err := v.client.info(ctx, infoRequest{Type: "clearinghouseState", User: v.cfg.Address}, &out); err != nil {
		return nil, err
	}
	now := time.Unix(0, out.Time*int64(time.Millisecond)).UTC()
	positions := make([]types.Position, 0, len(out.AssetPositions))
	for _, ap := range out.AssetPositions {
		qty, _ := decimal.NewFromString(ap.Position.Szi)
		if qty.IsZero() {
			continue
		}
		entry, _ := decimal.NewFromString(ap.Position.EntryPx)
		liq, _ := decimal.NewFromString(ap.Position.LiquidationPx)
		margin, _ := decimal.NewFromString(ap.Position.MarginUsed)
		upnl, _ := decimal.NewFromString(ap.Position.UnrealizedPnl)
		positions = append(positions, types.Position{
			Venue:        VenueName,
			Symbol:       ap.Position.Coin,
			Qty:          qty,
			EntryPrice:   entry,
			LiqPrice:     liq,
			Margin:       margin,
			UnrealizedPx: upnl,
			Leverage:     decimal.NewFromInt(int64(ap.Position.Leverage.Value)),
			UpdatedAt:    now,
		})
	}
	return positions, nil
}

// Balances returns the venue-side USDC balance. Hyperliquid is a USDC-margined
// venue, so we synthesise a single Balance from the clearinghouse summary.
// Requires Address to be configured.
func (v *Venue) Balances(ctx context.Context) ([]types.Balance, error) {
	if v.cfg.Address == "" {
		return nil, ErrAddressRequired
	}
	var out clearinghouseStateResp
	if err := v.client.info(ctx, infoRequest{Type: "clearinghouseState", User: v.cfg.Address}, &out); err != nil {
		return nil, err
	}
	withdrawable, _ := decimal.NewFromString(out.Withdrawable)
	used, _ := decimal.NewFromString(out.CrossMarginSummary.TotalMarginUsed)
	return []types.Balance{
		{
			Asset:  "USDC",
			Venue:  VenueName,
			Free:   withdrawable,
			Locked: used,
		},
	}, nil
}

// FundingRates returns the current per-symbol funding rate. If symbols is
// empty, all symbols in the universe are returned. Rates are reported as
// per-hour (Hyperliquid's native interval).
func (v *Venue) FundingRates(ctx context.Context, symbols []string) ([]types.FundingRate, error) {
	if err := v.refreshMeta(ctx, false); err != nil {
		return nil, err
	}
	v.metaMu.Lock()
	snap := v.metaSnapshot
	idx := v.metaIndex
	v.metaMu.Unlock()

	now := time.Now().UTC()
	want := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		want[s] = struct{}{}
	}

	out := make([]types.FundingRate, 0, len(snap.Meta.Universe))
	for coin, i := range idx {
		if len(want) > 0 {
			if _, ok := want[coin]; !ok {
				continue
			}
		}
		if i >= len(snap.Ctxs) {
			continue
		}
		rate, _ := decimal.NewFromString(snap.Ctxs[i].Funding)
		out = append(out, types.FundingRate{
			Time:     now,
			Venue:    VenueName,
			Symbol:   coin,
			Rate:     rate,
			Interval: time.Hour,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

// Subscribe opens a WebSocket and streams MarketEvents for the given symbols.
// See ws.go for the implementation.
func (v *Venue) Subscribe(ctx context.Context, symbols []string) (<-chan types.MarketEvent, error) {
	ep, err := v.cfg.endpoints()
	if err != nil {
		return nil, err
	}
	return subscribe(ctx, ep.WS, symbols)
}

// Place is gated on Signer + signing implementation (M4).
func (v *Venue) Place(ctx context.Context, intent types.OrderIntent) (types.OrderAck, error) {
	if v.signer == nil {
		return types.OrderAck{}, ErrSignerRequired
	}
	_, err := signAction(ctx, v.signer, intent)
	return types.OrderAck{}, err
}

// Cancel is gated on Signer + signing implementation (M4).
func (v *Venue) Cancel(ctx context.Context, id types.OrderID) error {
	if v.signer == nil {
		return ErrSignerRequired
	}
	_, err := signAction(ctx, v.signer, id)
	return err
}

// refreshMeta reloads metaAndAssetCtxs if the cached snapshot is older than
// 30s or force is true.
func (v *Venue) refreshMeta(ctx context.Context, force bool) error {
	v.metaMu.Lock()
	if !force && v.metaSnapshot != nil && time.Since(v.metaAt) < 30*time.Second {
		v.metaMu.Unlock()
		return nil
	}
	v.metaMu.Unlock()

	var resp metaAndAssetCtxsResp
	if err := v.client.info(ctx, infoRequest{Type: "metaAndAssetCtxs"}, &resp); err != nil {
		return fmt.Errorf("metaAndAssetCtxs: %w", err)
	}
	idx := make(map[string]int, len(resp.Meta.Universe))
	for i, u := range resp.Meta.Universe {
		idx[u.Name] = i
	}
	v.metaMu.Lock()
	v.metaSnapshot = &resp
	v.metaIndex = idx
	v.metaAt = time.Now()
	v.metaMu.Unlock()
	return nil
}
