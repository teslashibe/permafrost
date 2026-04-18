package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// OrderID is a venue-assigned identifier for a placed order.
type OrderID string

// ClientOrderID is a deterministic identifier supplied by the framework.
// Its value uniquely identifies an order intent for idempotent retries.
type ClientOrderID string

// Side is the buy/sell direction of an order.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType captures the order's posting style.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce controls how long a resting order remains active.
type TimeInForce string

const (
	TIFGTC TimeInForce = "gtc"  // good-til-cancelled
	TIFIOC TimeInForce = "ioc"  // immediate-or-cancel
	TIFFOK TimeInForce = "fok"  // fill-or-kill
	TIFALO TimeInForce = "alo"  // add-liquidity-only (post-only)
)

// OrderIntent is what a Strategy proposes. The agent runtime is responsible
// for stamping ClientID (deterministic), validating with Risk, and submitting
// to the Venue.
//
// BasisKey, when set, identifies the basis position this order belongs to.
// Strategies that emit paired swap+order intents set the same BasisKey on
// both legs so the runtime can reconcile them — necessary for multi-chain
// where the spot OutToken.Symbol (e.g. "ETH-BASE") differs from the perp
// Symbol (e.g. "ETH"). Single-chain strategies may leave it empty.
type OrderIntent struct {
	Venue       string          `json:"venue"`
	Symbol      string          `json:"symbol"`
	Side        Side            `json:"side"`
	Type        OrderType       `json:"type"`
	Price       decimal.Decimal `json:"price"`            // ignored for market orders
	Size        decimal.Decimal `json:"size"`             // base-asset units
	TIF         TimeInForce     `json:"tif"`
	ReduceOnly  bool            `json:"reduce_only"`
	ClientID    ClientOrderID   `json:"client_id"`        // set by runtime if empty
	PositionID  string          `json:"position_id"`      // links order to a strategy_position
	DecisionID  string          `json:"decision_id"`      // links order to an agent_decision
	Slot        int             `json:"slot"`             // ordinal within a decision
	BasisKey    string          `json:"basis_key,omitempty"`
	Tag         string          `json:"tag,omitempty"`    // strategy-defined annotation
}

// Notional returns the absolute notional value (price * size). For market
// orders Price will typically be zero and Notional reports zero.
func (o OrderIntent) Notional() decimal.Decimal {
	return o.Price.Mul(o.Size).Abs()
}

// OrderAck is what a Venue returns after accepting an order.
type OrderAck struct {
	ID         OrderID       `json:"id"`
	ClientID   ClientOrderID `json:"client_id"`
	AcceptedAt time.Time     `json:"accepted_at"`
	Status     OrderStatus   `json:"status"`
	Filled     decimal.Decimal `json:"filled"`
	Remaining  decimal.Decimal `json:"remaining"`
	AvgPrice   decimal.Decimal `json:"avg_price"`
}

// OrderStatus is the lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusOpen      OrderStatus = "open"
	OrderStatusPartial   OrderStatus = "partial"
	OrderStatusFilled    OrderStatus = "filled"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusRejected  OrderStatus = "rejected"
)

// Fill is a single execution against an order.
type Fill struct {
	OrderID  OrderID         `json:"order_id"`
	Time     time.Time       `json:"time"`
	Price    decimal.Decimal `json:"price"`
	Size     decimal.Decimal `json:"size"`
	Fee      decimal.Decimal `json:"fee"`
	FeeAsset string          `json:"fee_asset"`
}

// DeriveClientID computes the deterministic client_id used for idempotent
// order placement. The output is a stable function of (agentID, decisionID,
// slot, venue, symbol, side, size). Strategies should normally leave
// OrderIntent.ClientID blank and let the runtime stamp it; this helper is
// exported for tests and adapters that need to reproduce IDs.
func DeriveClientID(agentID, decisionID string, slot int, venue, symbol string, side Side, size decimal.Decimal) ClientOrderID {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%s|%s|%s|%s",
		agentID, decisionID, slot, venue, symbol, side, size.String())
	return ClientOrderID("pf_" + hex.EncodeToString(h.Sum(nil))[:24])
}
