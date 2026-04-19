package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// FundingRate is a single funding observation for a perpetual symbol.
//
// Rate is the per-interval rate (NOT annualised). Annualise via:
//   AnnualPct = Rate * (365*24h / Interval) * 100
//
// MarkPrice is the venue's published mark for the symbol at the time of
// the observation. Strategies use it to translate USDC notional into
// base-asset size (size = notional_usdc / MarkPrice). It may be zero if
// the venue did not return one in the same call.
type FundingRate struct {
	Time          time.Time       `json:"time"`
	Venue         string          `json:"venue"`
	Symbol        string          `json:"symbol"`
	Rate          decimal.Decimal `json:"rate"`            // per Interval, fractional (0.0001 = 1bp)
	Interval      time.Duration   `json:"interval"`        // typically 1h or 8h
	PredictedNext decimal.Decimal `json:"predicted_next"`  // venue-published forecast for next interval
	MarkPrice     decimal.Decimal `json:"mark_price"`      // mark price in USDC at observation time
}

// Annualised returns the funding rate scaled to one year. Returns zero if
// Interval is not positive.
func (f FundingRate) Annualised() decimal.Decimal {
	if f.Interval <= 0 {
		return decimal.Zero
	}
	year := decimal.NewFromInt(int64(time.Hour * 24 * 365))
	intvl := decimal.NewFromInt(int64(f.Interval))
	return f.Rate.Mul(year.Div(intvl))
}

// FundingPayment is a realised funding settlement for an open perp position.
type FundingPayment struct {
	Time   time.Time       `json:"time"`
	Venue  string          `json:"venue"`
	Symbol string          `json:"symbol"`
	Amount decimal.Decimal `json:"amount"` // signed: positive = received, negative = paid
	Rate   decimal.Decimal `json:"rate"`
}
