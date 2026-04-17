package hyperliquid

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
	hl "github.com/sonirico/go-hyperliquid"

	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// sdkClient lazily wraps sonirico/go-hyperliquid for write operations
// (Place / Cancel). Read-only methods (Subscribe, Positions, Balances,
// FundingRates) stay on our own thin client to avoid pulling sonirico's
// full surface area into hot paths.
//
// All msgpack/EIP-712/secp256k1 signing is handled by sonirico's SDK,
// which mirrors the official Hyperliquid Python SDK's `sign_l1_action`.
// We do NOT roll our own signing.
type sdkClient struct {
	mu       sync.Mutex
	exchange *hl.Exchange
	signer   *wallet.HyperliquidSigner
	network  Network
	address  string
}

func newSDKClient(s *wallet.HyperliquidSigner, network Network) *sdkClient {
	return &sdkClient{signer: s, network: network, address: s.Address()}
}

// init lazily constructs the sonirico Exchange. Done at first use so
// agents that only fetch funding rates don't pay for the SDK init.
func (c *sdkClient) init(ctx context.Context) (*hl.Exchange, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exchange != nil {
		return c.exchange, nil
	}

	keyBytes := c.signer.PrivateKeyBytes()
	defer func() {
		// Best-effort zeroise so the raw key isn't left in our heap.
		// Note: go-ethereum makes its own copy inside ToECDSA.
		for i := range keyBytes {
			keyBytes[i] = 0
		}
	}()
	priv, err := ethcrypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: convert key: %w", err)
	}

	apiURL := hl.MainnetAPIURL
	if c.network == NetworkTestnet {
		apiURL = hl.TestnetAPIURL
	}

	// vaultAddress empty = trade on the operator's own account.
	// accountAddress is the signer's address; meta/spotMeta/perpDexs left
	// nil so the SDK fetches them on first use.
	c.exchange = hl.NewExchange(ctx, priv, apiURL, nil, "", c.address, nil, nil)
	return c.exchange, nil
}

// Place submits a real order via the SDK. Maps types.OrderIntent →
// sonirico CreateOrderRequest.
func (c *sdkClient) Place(ctx context.Context, intent types.OrderIntent) (types.OrderAck, error) {
	ex, err := c.init(ctx)
	if err != nil {
		return types.OrderAck{}, err
	}

	isBuy := intent.Side == types.SideBuy
	size, _ := intent.Size.Float64()
	price, _ := intent.Price.Float64()

	var orderType hl.OrderType
	switch intent.Type {
	case types.OrderTypeLimit:
		tif := mapTIF(intent.TIF, hl.TifGtc)
		orderType.Limit = &hl.LimitOrderType{Tif: tif}
	case types.OrderTypeMarket:
		// Hyperliquid has no native "market" type — the standard pattern
		// is an IOC limit at a slippage-adjusted price. v1 leaves the
		// caller to set Price; richer implementations should price this
		// off the current mid via SlippagePrice.
		orderType.Limit = &hl.LimitOrderType{Tif: hl.TifIoc}
	default:
		return types.OrderAck{}, fmt.Errorf("hyperliquid: unsupported order type %q", intent.Type)
	}

	req := hl.CreateOrderRequest{
		Coin:       intent.Symbol,
		IsBuy:      isBuy,
		Size:       size,
		Price:      price,
		ReduceOnly: intent.ReduceOnly,
		OrderType:  orderType,
	}
	if intent.ClientID != "" {
		s := string(intent.ClientID)
		req.ClientOrderID = &s
	}

	status, err := ex.Order(ctx, req, nil)
	if err != nil {
		return types.OrderAck{}, fmt.Errorf("hyperliquid: place: %w", err)
	}
	return mapOrderStatus(status, intent), nil
}

// Cancel submits a real cancel via the SDK.
func (c *sdkClient) Cancel(ctx context.Context, id types.OrderID, symbol string) error {
	ex, err := c.init(ctx)
	if err != nil {
		return err
	}
	if symbol == "" {
		return fmt.Errorf("hyperliquid: cancel requires the order's symbol (coin)")
	}
	oid, err := parseOID(string(id))
	if err != nil {
		return err
	}
	if _, err := ex.Cancel(ctx, symbol, oid); err != nil {
		return fmt.Errorf("hyperliquid: cancel: %w", err)
	}
	return nil
}

// mapTIF translates our TimeInForce to sonirico's typed Tif. Empty
// defaults to fallback. Anything unrecognised falls back to fallback.
func mapTIF(tif types.TimeInForce, fallback hl.Tif) hl.Tif {
	switch types.TimeInForce(strings.ToLower(string(tif))) {
	case types.TIFGTC:
		return hl.TifGtc
	case types.TIFIOC:
		return hl.TifIoc
	case types.TIFFOK:
		// HL has no native FOK; closest is IOC with the caller checking
		// for full-fill in the response.
		return hl.TifIoc
	case types.TIFALO:
		return hl.TifAlo
	}
	return fallback
}

// mapOrderStatus turns sonirico's OrderStatus union into our typed Ack.
func mapOrderStatus(s hl.OrderStatus, intent types.OrderIntent) types.OrderAck {
	ack := types.OrderAck{
		ClientID:   intent.ClientID,
		AcceptedAt: time.Now().UTC(),
		Status:     types.OrderStatusOpen,
		Remaining:  intent.Size,
	}
	switch {
	case s.Resting != nil:
		ack.ID = types.OrderID(fmt.Sprintf("%d", s.Resting.Oid))
	case s.Filled != nil:
		ack.ID = types.OrderID(fmt.Sprintf("%d", s.Filled.Oid))
		ack.Status = types.OrderStatusFilled
		ack.Filled = decimalFromString(s.Filled.TotalSz)
		ack.AvgPrice = decimalFromString(s.Filled.AvgPx)
		ack.Remaining = decimal.Zero
	case s.Error != nil && *s.Error != "":
		ack.Status = types.OrderStatusRejected
	}
	return ack
}

// decimalFromString parses an SDK decimal string (the SDK preserves
// precision by passing fills as strings).
func decimalFromString(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// parseOID parses a Hyperliquid order id (numeric).
func parseOID(s string) (int64, error) {
	if strings.HasPrefix(s, "0x") {
		return 0, fmt.Errorf("hyperliquid: client-order-id cancellation needs CancelByCloid; pass the venue oid")
	}
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("hyperliquid: parse oid %q: %w", s, err)
	}
	return n, nil
}
