package vault

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Service wraps DB access for the vault accounting layer.
type Service struct {
	pool *pgxpool.Pool
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ─── vaults ─────────────────────────────────────────────────────────────────

// Init creates a vault if it does not already exist and returns it. v1 uses
// a single canonical vault per deployment.
func (s *Service) Init(ctx context.Context, name, asset string) (Vault, error) {
	if name == "" || asset == "" {
		return Vault{}, errors.New("vault: name and asset required")
	}
	const q = `
INSERT INTO vaults (name, asset)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET asset = vaults.asset
RETURNING id, name, asset, created_at;`
	var v Vault
	if err := s.pool.QueryRow(ctx, q, name, asset).
		Scan(&v.ID, &v.Name, &v.Asset, &v.CreatedAt); err != nil {
		return Vault{}, fmt.Errorf("vault: init: %w", err)
	}
	return v, nil
}

// Get returns the vault by name.
func (s *Service) Get(ctx context.Context, name string) (Vault, error) {
	const q = `SELECT id, name, asset, created_at FROM vaults WHERE name = $1`
	var v Vault
	if err := s.pool.QueryRow(ctx, q, name).Scan(&v.ID, &v.Name, &v.Asset, &v.CreatedAt); err != nil {
		return Vault{}, fmt.Errorf("vault: get: %w", err)
	}
	return v, nil
}

// ─── ledger ─────────────────────────────────────────────────────────────────

// Deposit appends a deposit entry.
func (s *Service) Deposit(ctx context.Context, vaultID int64, amount decimal.Decimal, source, note string) (Deposit, error) {
	if !amount.IsPositive() {
		return Deposit{}, errors.New("vault: deposit amount must be positive")
	}
	const q = `INSERT INTO deposits (vault_id, amount, source, note)
VALUES ($1, $2, $3, $4) RETURNING id, created_at;`
	var d = Deposit{VaultID: vaultID, Amount: amount, Source: source, Note: note}
	if err := s.pool.QueryRow(ctx, q, vaultID, amount, source, note).Scan(&d.ID, &d.CreatedAt); err != nil {
		return Deposit{}, fmt.Errorf("vault: deposit: %w", err)
	}
	return d, nil
}

// Withdraw appends a withdrawal entry. Does NOT enforce against current NAV;
// risk + balance checks are the agent runtime's job.
func (s *Service) Withdraw(ctx context.Context, vaultID int64, amount decimal.Decimal, dest, note string) (Withdrawal, error) {
	if !amount.IsPositive() {
		return Withdrawal{}, errors.New("vault: withdrawal amount must be positive")
	}
	const q = `INSERT INTO withdrawals (vault_id, amount, destination, note)
VALUES ($1, $2, $3, $4) RETURNING id, created_at;`
	w := Withdrawal{VaultID: vaultID, Amount: amount, Destination: dest, Note: note}
	if err := s.pool.QueryRow(ctx, q, vaultID, amount, dest, note).Scan(&w.ID, &w.CreatedAt); err != nil {
		return Withdrawal{}, fmt.Errorf("vault: withdraw: %w", err)
	}
	return w, nil
}

// AddLockup records a time-based lockup against vault capital.
func (s *Service) AddLockup(ctx context.Context, vaultID int64, amount decimal.Decimal, until time.Time, note string) (Lockup, error) {
	if !amount.IsPositive() {
		return Lockup{}, errors.New("vault: lockup amount must be positive")
	}
	if !until.After(time.Now()) {
		return Lockup{}, errors.New("vault: unlock time must be in the future")
	}
	const q = `INSERT INTO lockups (vault_id, amount, unlock_at, note)
VALUES ($1, $2, $3, $4) RETURNING id, created_at;`
	l := Lockup{VaultID: vaultID, Amount: amount, UnlockAt: until, Note: note}
	if err := s.pool.QueryRow(ctx, q, vaultID, amount, until, note).Scan(&l.ID, &l.CreatedAt); err != nil {
		return Lockup{}, fmt.Errorf("vault: lockup: %w", err)
	}
	return l, nil
}

// ActiveLockups returns lockups still in force at t.
func (s *Service) ActiveLockups(ctx context.Context, vaultID int64, t time.Time) ([]Lockup, error) {
	const q = `SELECT id, vault_id, amount, unlock_at, COALESCE(note,''), created_at
FROM lockups WHERE vault_id = $1 AND unlock_at > $2 ORDER BY unlock_at`
	rows, err := s.pool.Query(ctx, q, vaultID, t)
	if err != nil {
		return nil, fmt.Errorf("vault: lockups: %w", err)
	}
	defer rows.Close()
	out := make([]Lockup, 0)
	for rows.Next() {
		var l Lockup
		if err := rows.Scan(&l.ID, &l.VaultID, &l.Amount, &l.UnlockAt, &l.Note, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LedgerTotals returns cumulative deposits and withdrawals for the vault.
func (s *Service) LedgerTotals(ctx context.Context, vaultID int64) (depositTotal, withdrawalTotal decimal.Decimal, err error) {
	row := s.pool.QueryRow(ctx, `
SELECT
  COALESCE((SELECT SUM(amount) FROM deposits     WHERE vault_id = $1), 0),
  COALESCE((SELECT SUM(amount) FROM withdrawals  WHERE vault_id = $1), 0)`, vaultID)
	if err = row.Scan(&depositTotal, &withdrawalTotal); err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("vault: totals: %w", err)
	}
	return
}

// ─── NAV ────────────────────────────────────────────────────────────────────

// LatestNAV returns the most recent NAV snapshot for a vault, or
// pgx.ErrNoRows if none exists.
func (s *Service) LatestNAV(ctx context.Context, vaultID int64) (NAVSnapshot, error) {
	const q = `SELECT time, vault_id, nav, cash, positions_value, high_water_mark, deposit_total, withdrawal_total
FROM nav_snapshots WHERE vault_id = $1 ORDER BY time DESC LIMIT 1`
	var s2 NAVSnapshot
	if err := s.pool.QueryRow(ctx, q, vaultID).Scan(
		&s2.Time, &s2.VaultID, &s2.NAV, &s2.Cash, &s2.PositionsValue,
		&s2.HighWaterMark, &s2.DepositTotal, &s2.WithdrawalTotal,
	); err != nil {
		return NAVSnapshot{}, err
	}
	return s2, nil
}

// RecordNAV computes a NAV snapshot from current cash + positions value and
// persists it. The previous HWM is read from latest_nav (or zero).
func (s *Service) RecordNAV(ctx context.Context, vaultID int64, cash, positionsValue decimal.Decimal) (NAVSnapshot, error) {
	depositTotal, withdrawalTotal, err := s.LedgerTotals(ctx, vaultID)
	if err != nil {
		return NAVSnapshot{}, err
	}
	prevHWM := decimal.Zero
	if prev, err := s.LatestNAV(ctx, vaultID); err == nil {
		prevHWM = prev.HighWaterMark
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return NAVSnapshot{}, err
	}
	snap := ComputeNAV(time.Now().UTC(), vaultID, NAVInputs{
		Cash:            cash,
		PositionsValue:  positionsValue,
		DepositTotal:    depositTotal,
		WithdrawalTotal: withdrawalTotal,
		PrevHWM:         prevHWM,
	})
	const ins = `INSERT INTO nav_snapshots (time, vault_id, nav, cash, positions_value, high_water_mark, deposit_total, withdrawal_total)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (vault_id, time) DO NOTHING;`
	if _, err := s.pool.Exec(ctx, ins,
		snap.Time, snap.VaultID, snap.NAV, snap.Cash, snap.PositionsValue,
		snap.HighWaterMark, snap.DepositTotal, snap.WithdrawalTotal,
	); err != nil {
		return NAVSnapshot{}, fmt.Errorf("vault: record nav: %w", err)
	}
	return snap, nil
}

// NAVHistory returns NAV snapshots since the given time, oldest first.
func (s *Service) NAVHistory(ctx context.Context, vaultID int64, since time.Time) ([]NAVSnapshot, error) {
	const q = `SELECT time, vault_id, nav, cash, positions_value, high_water_mark, deposit_total, withdrawal_total
FROM nav_snapshots WHERE vault_id = $1 AND time >= $2 ORDER BY time ASC`
	rows, err := s.pool.Query(ctx, q, vaultID, since)
	if err != nil {
		return nil, fmt.Errorf("vault: nav history: %w", err)
	}
	defer rows.Close()
	out := make([]NAVSnapshot, 0)
	for rows.Next() {
		var s2 NAVSnapshot
		if err := rows.Scan(&s2.Time, &s2.VaultID, &s2.NAV, &s2.Cash, &s2.PositionsValue,
			&s2.HighWaterMark, &s2.DepositTotal, &s2.WithdrawalTotal); err != nil {
			return nil, err
		}
		out = append(out, s2)
	}
	return out, rows.Err()
}
