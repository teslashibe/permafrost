package assets

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sync reconciles the assets table with the supplied Registry. It performs
// an upsert per row (no deletes), since removing an entry from registry.yaml
// while live positions reference it would orphan accounting.
//
// Returns the number of rows inserted/updated.
func Sync(ctx context.Context, pool *pgxpool.Pool, r Registry) (int, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("assets: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup

	const stmt = `
INSERT INTO assets (symbol, perp_venue, perp_symbol, spot_chain, spot_mint, spot_decimals, hedge_ratio, notes, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (symbol) DO UPDATE
SET perp_venue    = EXCLUDED.perp_venue,
    perp_symbol   = EXCLUDED.perp_symbol,
    spot_chain    = EXCLUDED.spot_chain,
    spot_mint     = EXCLUDED.spot_mint,
    spot_decimals = EXCLUDED.spot_decimals,
    hedge_ratio   = EXCLUDED.hedge_ratio,
    notes         = EXCLUDED.notes,
    updated_at    = now();
`
	count := 0
	for _, a := range r.Assets {
		var perpVenue, perpSymbol any
		if a.Perp != nil {
			perpVenue = a.Perp.Venue
			perpSymbol = a.Perp.Symbol
		}
		if _, err := tx.Exec(ctx, stmt,
			a.Symbol, perpVenue, perpSymbol,
			string(a.Spot.Chain), a.Spot.Mint, a.Spot.Decimals,
			a.HedgeRatio, a.Notes,
		); err != nil {
			return 0, fmt.Errorf("assets: upsert %s: %w", a.Symbol, err)
		}
		count++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("assets: commit: %w", err)
	}
	return count, nil
}
