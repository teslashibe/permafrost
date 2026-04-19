// Package vault models the off-chain accounting layer: deposits,
// withdrawals, lockups, and NAV snapshots. v1 is single-asset (USDC) and
// single-vault per deployment; the multi-asset / multi-tenant story is v2
// (out of scope for v1; on-chain vault contracts are deferred to v2).
package vault

import (
	"time"

	"github.com/shopspring/decimal"
)

// Vault is a row in the vaults table.
type Vault struct {
	ID        int64
	Name      string
	Asset     string // accounting unit, e.g. "USDC"
	CreatedAt time.Time
}

// Deposit is one ledger entry into the vault.
type Deposit struct {
	ID        int64
	VaultID   int64
	Amount    decimal.Decimal
	Source    string
	Note      string
	CreatedAt time.Time
}

// Withdrawal is one ledger entry out of the vault.
type Withdrawal struct {
	ID          int64
	VaultID     int64
	Amount      decimal.Decimal
	Destination string
	Note        string
	CreatedAt   time.Time
}

// Lockup freezes a portion of vault capital until UnlockAt.
type Lockup struct {
	ID        int64
	VaultID   int64
	Amount    decimal.Decimal
	UnlockAt  time.Time
	Note      string
	CreatedAt time.Time
}

// IsActive reports whether the lockup is still in force at t.
func (l Lockup) IsActive(t time.Time) bool { return t.Before(l.UnlockAt) }

// NAVSnapshot is one observation of vault state.
type NAVSnapshot struct {
	Time            time.Time
	VaultID         int64
	NAV             decimal.Decimal // cash + positions_value
	Cash            decimal.Decimal
	PositionsValue  decimal.Decimal
	HighWaterMark   decimal.Decimal
	DepositTotal    decimal.Decimal // cumulative deposits
	WithdrawalTotal decimal.Decimal // cumulative withdrawals
}

// Inputs to ComputeNAV (see nav.go).
type NAVInputs struct {
	Cash            decimal.Decimal
	PositionsValue  decimal.Decimal
	DepositTotal    decimal.Decimal
	WithdrawalTotal decimal.Decimal
	PrevHWM         decimal.Decimal
}
