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
// without a Signer. Place and Cancel require WithSigner — under the hood
// they delegate to sonirico/go-hyperliquid for the EIP-712 action signing
// (msgpack → keccak256 → EIP-712 → secp256k1). We do not roll our own.
type Venue struct {
	cfg    Config
	client *Client
	signer wallet.Signer
	sdk    *sdkClient // lazily initialised when signer is set

	// orderSymbols remembers the coin (Symbol) of orders we've placed so
	// Cancel(id) can supply it without callers having to plumb it back.
	// This is in-memory only; restart loses it. Callers that need
	// restart-resilience should track (oid → coin) themselves.
	orderSymMu sync.Mutex
	orderSym   map[types.OrderID]string

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
		cfg:      cfg,
		client:   NewClient(ep),
		orderSym: make(map[types.OrderID]string),
	}
	for _, opt := range opts {
		opt(v)
	}
	if hlSigner, ok := v.signer.(*wallet.HyperliquidSigner); ok {
		v.sdk = newSDKClient(hlSigner, cfg.Network, v.SzDecimals)
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
		mark, _ := decimal.NewFromString(snap.Ctxs[i].MarkPx)
		out = append(out, types.FundingRate{
			Time:      now,
			Venue:     VenueName,
			Symbol:    coin,
			Rate:      rate,
			Interval:  time.Hour,
			MarkPrice: mark,
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

// Place submits an order via the EIP-712-signed action endpoint.
//
// Requires a *wallet.HyperliquidSigner installed via WithSigner(). If a
// generic wallet.Signer is supplied (i.e. not a HyperliquidSigner), the
// adapter cannot extract the secp256k1 key required for action signing
// and ErrSignerRequired is returned.
func (v *Venue) Place(ctx context.Context, intent types.OrderIntent) (types.OrderAck, error) {
	if v.sdk == nil {
		return types.OrderAck{}, ErrSignerRequired
	}
	ack, err := v.sdk.Place(ctx, intent)
	if err == nil && ack.ID != "" {
		v.rememberOrder(ack.ID, intent.Symbol)
	}
	return ack, err
}

// Cancel removes a resting order. The Hyperliquid /exchange API requires
// both the venue oid and the coin (symbol). We remember each (oid, symbol)
// pair we placed so callers don't have to plumb it back; if the order
// wasn't placed by this Venue instance the caller must supply symbol via
// CancelWithSymbol.
func (v *Venue) Cancel(ctx context.Context, id types.OrderID) error {
	if v.sdk == nil {
		return ErrSignerRequired
	}
	v.orderSymMu.Lock()
	sym, ok := v.orderSym[id]
	v.orderSymMu.Unlock()
	if !ok {
		return fmt.Errorf("hyperliquid: cancel: unknown order %q in this Venue's session — use CancelWithSymbol", id)
	}
	if err := v.sdk.Cancel(ctx, id, sym); err != nil {
		return err
	}
	v.forgetOrder(id)
	return nil
}

// CancelWithSymbol cancels an order placed by another process / restart
// where this Venue doesn't know the symbol mapping.
func (v *Venue) CancelWithSymbol(ctx context.Context, id types.OrderID, symbol string) error {
	if v.sdk == nil {
		return ErrSignerRequired
	}
	if err := v.sdk.Cancel(ctx, id, symbol); err != nil {
		return err
	}
	v.forgetOrder(id)
	return nil
}

func (v *Venue) rememberOrder(id types.OrderID, symbol string) {
	v.orderSymMu.Lock()
	defer v.orderSymMu.Unlock()
	v.orderSym[id] = symbol
}

func (v *Venue) forgetOrder(id types.OrderID) {
	v.orderSymMu.Lock()
	defer v.orderSymMu.Unlock()
	delete(v.orderSym, id)
}

// SzDecimals returns the size-precision decimal count for a coin from the
// cached metaAndAssetCtxs (refreshes lazily). Used by the SDK adapter to
// round order sizes to the venue's accepted precision before submission.
// Returns -1 if the symbol is unknown.
func (v *Venue) SzDecimals(ctx context.Context, coin string) (int, error) {
	if err := v.refreshMeta(ctx, false); err != nil {
		return -1, err
	}
	v.metaMu.Lock()
	defer v.metaMu.Unlock()
	idx, ok := v.metaIndex[coin]
	if !ok {
		return -1, fmt.Errorf("hyperliquid: unknown coin %q", coin)
	}
	return v.metaSnapshot.Meta.Universe[idx].SzDecimals, nil
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
