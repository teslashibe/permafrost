package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/store"
	"github.com/teslashibe/permafrost/internal/vault"
)

const defaultVaultName = "permafrost"
const vaultAsset = "USDC"

func init() { addCommandFactory(newVaultCmd) }

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Vault accounting (deposits, withdrawals, NAV)",
	}
	cmd.AddCommand(
		newVaultInitCmd(),
		newVaultDepositCmd(),
		newVaultWithdrawCmd(),
		newVaultLockupCmd(),
		newVaultStatusCmd(),
		newVaultRecordNAVCmd(),
		newVaultNAVHistoryCmd(),
	)
	return cmd
}

func openVaultService(c *cobra.Command) (*vault.Service, *store.DB, vault.Vault, error) {
	g := FromContext(c.Context())
	if g == nil {
		return nil, nil, vault.Vault{}, errors.New("globals not initialised")
	}
	db, err := store.Open(c.Context(), g.Config.Database)
	if err != nil {
		return nil, nil, vault.Vault{}, fmt.Errorf("open db: %w", err)
	}
	svc := vault.NewService(db.Pool)
	v, err := svc.Init(c.Context(), defaultVaultName, vaultAsset)
	if err != nil {
		db.Close()
		return nil, nil, vault.Vault{}, err
	}
	return svc, db, v, nil
}

func newVaultInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the default vault if it does not exist",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			_ = svc
			fmt.Printf("vault id=%d name=%s asset=%s\n", v.ID, v.Name, v.Asset)
			return nil
		},
	}
}

func newVaultDepositCmd() *cobra.Command {
	var amount, source, note string
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: "Record a deposit into the vault",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			amt, err := decimal.NewFromString(amount)
			if err != nil {
				return fmt.Errorf("amount: %w", err)
			}
			d, err := svc.Deposit(c.Context(), v.ID, amt, source, note)
			if err != nil {
				return err
			}
			fmt.Printf("deposit id=%d amount=%s source=%s\n", d.ID, d.Amount, d.Source)
			return nil
		},
	}
	cmd.Flags().StringVar(&amount, "amount", "", "USDC amount (e.g. 10000)")
	cmd.Flags().StringVar(&source, "source", "manual", "source label")
	cmd.Flags().StringVar(&note, "note", "", "freeform note")
	_ = cmd.MarkFlagRequired("amount")
	return cmd
}

func newVaultWithdrawCmd() *cobra.Command {
	var amount, dest, note string
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: "Record a withdrawal from the vault",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			amt, err := decimal.NewFromString(amount)
			if err != nil {
				return err
			}
			w, err := svc.Withdraw(c.Context(), v.ID, amt, dest, note)
			if err != nil {
				return err
			}
			fmt.Printf("withdrawal id=%d amount=%s dest=%s\n", w.ID, w.Amount, w.Destination)
			return nil
		},
	}
	cmd.Flags().StringVar(&amount, "amount", "", "USDC amount")
	cmd.Flags().StringVar(&dest, "dest", "manual", "destination label")
	cmd.Flags().StringVar(&note, "note", "", "freeform note")
	_ = cmd.MarkFlagRequired("amount")
	return cmd
}

func newVaultLockupCmd() *cobra.Command {
	var amount, until, note string
	cmd := &cobra.Command{
		Use:   "lockup",
		Short: "Add a time-based lockup against vault capital",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			amt, err := decimal.NewFromString(amount)
			if err != nil {
				return err
			}
			ts, err := time.Parse(time.RFC3339, until)
			if err != nil {
				return fmt.Errorf("--until must be RFC3339 (e.g. 2027-01-01T00:00:00Z): %w", err)
			}
			l, err := svc.AddLockup(c.Context(), v.ID, amt, ts, note)
			if err != nil {
				return err
			}
			fmt.Printf("lockup id=%d amount=%s unlock_at=%s\n", l.ID, l.Amount, l.UnlockAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&amount, "amount", "", "USDC amount")
	cmd.Flags().StringVar(&until, "until", "", "unlock time (RFC3339)")
	cmd.Flags().StringVar(&note, "note", "", "freeform note")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("until")
	return cmd
}

func newVaultStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show vault summary (deposits, withdrawals, latest NAV)",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			dep, wit, err := svc.LedgerTotals(c.Context(), v.ID)
			if err != nil {
				return err
			}
			fmt.Printf("vault:        %s (%s)\n", v.Name, v.Asset)
			fmt.Printf("deposits:     %s\n", dep)
			fmt.Printf("withdrawals:  %s\n", wit)
			fmt.Printf("net contrib:  %s\n", dep.Sub(wit))
			snap, err := svc.LatestNAV(c.Context(), v.ID)
			if err != nil {
				fmt.Println("nav:          (no snapshots yet — run `permafrost vault record-nav`)")
				return nil
			}
			fmt.Printf("nav:          %s (cash=%s positions=%s)\n", snap.NAV, snap.Cash, snap.PositionsValue)
			fmt.Printf("hwm:          %s\n", snap.HighWaterMark)
			fmt.Printf("return:       %s\n", vault.SimpleReturn(snap))
			return nil
		},
	}
}

func newVaultRecordNAVCmd() *cobra.Command {
	var cash, positions string
	cmd := &cobra.Command{
		Use:   "record-nav",
		Short: "Persist a NAV snapshot from supplied cash + positions",
		Long:  "v1 takes cash and positions value as arguments. The agent runtime will populate these automatically in M8.",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			cv, err := decimal.NewFromString(cash)
			if err != nil {
				return fmt.Errorf("cash: %w", err)
			}
			pv, err := decimal.NewFromString(positions)
			if err != nil {
				return fmt.Errorf("positions: %w", err)
			}
			snap, err := svc.RecordNAV(c.Context(), v.ID, cv, pv)
			if err != nil {
				return err
			}
			fmt.Printf("nav=%s cash=%s pos=%s hwm=%s\n", snap.NAV, snap.Cash, snap.PositionsValue, snap.HighWaterMark)
			return nil
		},
	}
	cmd.Flags().StringVar(&cash, "cash", "0", "current cash balance")
	cmd.Flags().StringVar(&positions, "positions", "0", "current positions value")
	return cmd
}

func newVaultNAVHistoryCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "nav",
		Short: "Print recent NAV snapshots",
		RunE: func(c *cobra.Command, _ []string) error {
			svc, db, v, err := openVaultService(c)
			if err != nil {
				return err
			}
			defer db.Close()
			dur, err := time.ParseDuration(since)
			if err != nil {
				return fmt.Errorf("--since must be a duration (e.g. 24h): %w", err)
			}
			rows, err := svc.NAVHistory(c.Context(), v.ID, time.Now().Add(-dur))
			if err != nil {
				return err
			}
			for _, r := range rows {
				fmt.Printf("%s  nav=%s  cash=%s  pos=%s\n",
					r.Time.Format(time.RFC3339), r.NAV, r.Cash, r.PositionsValue)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "168h", "lookback duration (e.g. 24h, 7d→168h)")
	return cmd
}
