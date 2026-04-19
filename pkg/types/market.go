package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// Tick is a top-of-book quote.
type Tick struct {
	Time   time.Time       `json:"time"`
	Venue  string          `json:"venue"`
	Symbol string          `json:"symbol"`
	Bid    decimal.Decimal `json:"bid"`
	Ask    decimal.Decimal `json:"ask"`
	Last   decimal.Decimal `json:"last"`
	Volume decimal.Decimal `json:"volume"` // 24h
}

// Mid returns (Bid+Ask)/2 when both are set, else Last.
func (t Tick) Mid() decimal.Decimal {
	if !t.Bid.IsZero() && !t.Ask.IsZero() {
		return t.Bid.Add(t.Ask).Div(decimal.NewFromInt(2))
	}
	return t.Last
}

// BookLevel is a single price level in an orderbook side.
type BookLevel struct {
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

// BookSnapshot captures aggregated bids and asks for a symbol.
type BookSnapshot struct {
	Time   time.Time   `json:"time"`
	Venue  string      `json:"venue"`
	Symbol string      `json:"symbol"`
	Bids   []BookLevel `json:"bids"` // sorted descending by price
	Asks   []BookLevel `json:"asks"` // sorted ascending by price
}

// MarketEventKind tags the union members of MarketEvent.
type MarketEventKind string

const (
	EventTick    MarketEventKind = "tick"
	EventBook    MarketEventKind = "book"
	EventTrade   MarketEventKind = "trade"
	EventFunding MarketEventKind = "funding"
)

// Trade is a public trade print.
type Trade struct {
	Time   time.Time       `json:"time"`
	Venue  string          `json:"venue"`
	Symbol string          `json:"symbol"`
	Price  decimal.Decimal `json:"price"`
	Size   decimal.Decimal `json:"size"`
	Side   Side            `json:"side"` // taker side
}

// MarketEvent is a typed envelope sent on the venue subscription channel.
// Exactly one of Tick/Book/Trade/Funding will be populated based on Kind.
type MarketEvent struct {
	Kind    MarketEventKind `json:"kind"`
	Tick    *Tick           `json:"tick,omitempty"`
	Book    *BookSnapshot   `json:"book,omitempty"`
	Trade   *Trade          `json:"trade,omitempty"`
	Funding *FundingRate    `json:"funding,omitempty"`
}

// MarketSnapshot is the consolidated view of one symbol at decision time.
type MarketSnapshot struct {
	Time     time.Time              `json:"time"`
	Symbols  map[string]SymbolSnap  `json:"symbols"` // keyed by symbol
}

// SymbolSnap collects everything a strategy needs to know about one symbol.
type SymbolSnap struct {
	Tick    Tick         `json:"tick"`
	Book    BookSnapshot `json:"book"`
	Funding FundingRate  `json:"funding"`
}
