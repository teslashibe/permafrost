package vault

import (
	"time"

	"github.com/shopspring/decimal"
)

// ComputeNAV produces a NAVSnapshot from the supplied inputs. NAV is simply
// cash + positions value; the high-water mark is the running maximum.
//
// This function is pure and stateless to keep tests trivial; the database
// layer in service.go wraps it.
func ComputeNAV(now time.Time, vaultID int64, in NAVInputs) NAVSnapshot {
	nav := in.Cash.Add(in.PositionsValue)
	hwm := in.PrevHWM
	if nav.GreaterThan(hwm) {
		hwm = nav
	}
	return NAVSnapshot{
		Time:            now,
		VaultID:         vaultID,
		NAV:             nav,
		Cash:            in.Cash,
		PositionsValue:  in.PositionsValue,
		HighWaterMark:   hwm,
		DepositTotal:    in.DepositTotal,
		WithdrawalTotal: in.WithdrawalTotal,
	}
}

// AvailableCapital returns the cash that is not currently locked.
//
// Lockups are applied by amount: if total lockups exceed cash, the excess is
// considered to be locked in positions and is not available for new
// allocations.
func AvailableCapital(cash decimal.Decimal, lockups []Lockup, now time.Time) decimal.Decimal {
	locked := decimal.Zero
	for _, l := range lockups {
		if l.IsActive(now) {
			locked = locked.Add(l.Amount)
		}
	}
	available := cash.Sub(locked)
	if available.IsNegative() {
		return decimal.Zero
	}
	return available
}

// SimpleReturn returns NAV / (deposits - withdrawals) - 1 — the all-time
// total return on net contributed capital. Returns zero if no capital was
// contributed.
func SimpleReturn(s NAVSnapshot) decimal.Decimal {
	contributed := s.DepositTotal.Sub(s.WithdrawalTotal)
	if contributed.IsZero() || contributed.IsNegative() {
		return decimal.Zero
	}
	return s.NAV.Div(contributed).Sub(decimal.NewFromInt(1))
}
