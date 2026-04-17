package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/store"
)

func init() { addCommandFactory(newAssetsCmd) }

func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assets",
		Short: "Inspect and synchronise the asset registry",
	}
	cmd.AddCommand(newAssetsListCmd(), newAssetsSyncCmd())
	return cmd
}

func newAssetsListCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Print all entries in the curated asset registry",
		RunE: func(c *cobra.Command, _ []string) error {
			r, err := loadRegistry(path)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmdOut(c), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SYMBOL\tPERP\tCHAIN\tMINT\tDECIMALS\tTRADABLE")
			for _, a := range r.Assets {
				perp := "-"
				if a.Perp != nil {
					perp = a.Perp.Venue + ":" + a.Perp.Symbol
				}
				tradable := "no"
				if a.Tradable() {
					tradable = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
					a.Symbol, perp, a.Spot.Chain, truncMint(a.Spot.Mint), a.Spot.Decimals, tradable)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&path, "file", "", "registry YAML path (default: embedded)")
	return cmd
}

func newAssetsSyncCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Upsert the asset registry into the database",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			r, err := loadRegistry(path)
			if err != nil {
				return err
			}
			db, err := store.Open(c.Context(), g.Config.Database)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			n, err := assets.Sync(c.Context(), db.Pool, r)
			if err != nil {
				return err
			}
			g.Log.Info("assets synced", "rows", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "file", "", "registry YAML path (default: embedded)")
	return cmd
}

func loadRegistry(path string) (assets.Registry, error) {
	if path == "" {
		return assets.LoadEmbedded()
	}
	return assets.Load(path)
}

func truncMint(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:6] + "…" + s[len(s)-4:]
}

// cmdOut returns the cobra command's stdout (or os.Stdout). Split out so
// future tests can capture output by injecting a buffer.
func cmdOut(c *cobra.Command) (out interface {
	Write(p []byte) (n int, err error)
}) {
	return c.OutOrStdout()
}
