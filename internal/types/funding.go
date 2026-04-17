package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// FundingRate is a single funding observation for a perpetual symbol.
//
// Rate is the per-interval rate (NOT annualised). Annualise via:
//   AnnualPct = Rate * (365*24h / Interval) * 100
type FundingRate struct {
	Time          time.Time       `json:"time"`
	Venue         string          `json:"venue"`
	Symbol        string          `json:"symbol"`
	Rate          decimal.Decimal `json:"rate"`            // per Interval, fractional (0.0001 = 1bp)
	Interval      time.Duration   `json:"interval"`        // typically 1h or 8h
	PredictedNext decimal.Decimal `json:"predicted_next"`  // venue-published forecast for next interval
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
