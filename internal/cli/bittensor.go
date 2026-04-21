package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/pkg/types"
)

func init() {
	addCommandFactory(newBittensorCmd)
}

func newBittensorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bittensor",
		Short: "Bittensor subnet alpha token commands",
		Long: `Interact with Bittensor subnets: list subnets, check balances,
inspect alpha token positions.

All data is read directly from the Subtensor chain — no third-party APIs.
Configure the RPC endpoint via 'bittensor.rpc_url' in config.yaml or
PERMAFROST_BITTENSOR__RPC_URL env var. Defaults to the public Opentensor
endpoint.`,
	}
	cmd.AddCommand(
		newBittensorSubnetsCmd(),
		newBittensorBalanceCmd(),
		newBittensorPriceCmd(),
	)
	return cmd
}

func newBittensorSubnetsCmd() *cobra.Command {
	var max uint16
	cmd := &cobra.Command{
		Use:   "subnets",
		Short: "List all Bittensor subnets with current alpha prices",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			client := btchain.NewClient(rpcURL)
			defer client.Close()

			chain, err := client.GetChain(c.Context())
			if err != nil {
				return fmt.Errorf("cannot connect to Subtensor at %s: %w", rpcURL, err)
			}
			fmt.Fprintf(os.Stdout, "Connected to: %s (%s)\n\n", chain, rpcURL)

			summaries, err := client.ListSubnets(c.Context(), max)
			if err != nil {
				return fmt.Errorf("list subnets: %w", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NETUID\tSYMBOL\tALPHA PRICE (TAO)")
			fmt.Fprintln(w, "------\t------\t-----------------")
			for _, s := range summaries {
				fmt.Fprintf(w, "%d\tSN%d\t%s\n", s.Netuid, s.Netuid, s.AlphaPrice.StringFixed(9))
			}
			fmt.Fprintln(w, "")
			fmt.Fprintf(w, "Total active subnets: %d\n", len(summaries))
			return w.Flush()
		},
	}
	cmd.Flags().Uint16Var(&max, "max", 128, "highest netuid to query")
	return cmd
}

func newBittensorBalanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "Show TAO and alpha balances for the configured wallet",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			client := btchain.NewClient(rpcURL)
			defer client.Close()

			ks, err := openKeystore(c)
			if err != nil {
				return fmt.Errorf("open keystore: %w", err)
			}
			signer, err := ks.Signer(types.ChainBittensor)
			if err != nil {
				return fmt.Errorf("no Bittensor key in keystore (run: permafrost wallet generate --chain bittensor): %w", err)
			}

			fmt.Fprintf(os.Stdout, "Address: %s\n\n", signer.Address())

			rao, err := client.GetTAOBalance(c.Context(), signer.Address())
			if err != nil {
				return fmt.Errorf("get balance: %w", err)
			}
			tao := btchain.RAOToTAO(rao)
			fmt.Fprintf(os.Stdout, "TAO Balance: %s TAO (%d RAO)\n", tao.StringFixed(9), rao)
			return nil
		},
	}
}

func newBittensorPriceCmd() *cobra.Command {
	var netuid uint16
	cmd := &cobra.Command{
		Use:   "price",
		Short: "Show current alpha price for a subnet",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			client := btchain.NewClient(rpcURL)
			defer client.Close()

			price, err := client.CurrentAlphaPrice(c.Context(), netuid)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "SN%d/TAO: %s TAO per alpha\n", netuid, price.StringFixed(9))
			return nil
		},
	}
	cmd.Flags().Uint16Var(&netuid, "netuid", 8, "subnet netuid (e.g. 8 for SN8/Vanta)")
	return cmd
}
