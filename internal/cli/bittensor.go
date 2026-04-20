package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/spf13/cobra"
)

func init() {
	addCommandFactory(newBittensorCmd)
}

func newBittensorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bittensor",
		Short: "Bittensor subnet alpha token commands",
		Long:  "Interact with Bittensor subnets: list subnets, check balances, and manage alpha token positions.",
	}
	cmd.AddCommand(
		newBittensorSubnetsCmd(),
		newBittensorBalanceCmd(),
	)
	return cmd
}

func newBittensorSubnetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "subnets",
		Short: "List all Bittensor subnets with pool prices and emission rates",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			client := btchain.NewClient(rpcURL)

			chain, err := client.GetChain(c.Context())
			if err != nil {
				return fmt.Errorf("cannot connect to Subtensor at %s: %w", rpcURL, err)
			}
			fmt.Fprintf(os.Stdout, "Connected to: %s (%s)\n\n", chain, rpcURL)

			subnets, err := client.GetSubnetsInfo(c.Context())
			if err != nil {
				return fmt.Errorf("get subnets: %w", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NETUID\tNAME\tPRICE (TAO)\tTAO RESERVE\tALPHA RESERVE\tEMISSION %")
			fmt.Fprintln(w, "------\t----\t-----------\t-----------\t-------------\t----------")
			for _, sn := range subnets {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
					sn.Netuid,
					sn.Name,
					sn.AlphaPrice.StringFixed(6),
					sn.TaoReserve.StringFixed(2),
					sn.AlphaReserve.StringFixed(2),
					sn.Emission.StringFixed(4),
				)
			}
			return w.Flush()
		},
	}
}

func newBittensorBalanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "Show TAO and alpha token balances for the configured wallet",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			client := btchain.NewClient(rpcURL)

			ks, err := openKeystore(c)
			if err != nil {
				return fmt.Errorf("open keystore: %w", err)
			}

			signer, err := ks.Signer("bittensor")
			if err != nil {
				return fmt.Errorf("no Bittensor key in keystore (run: permafrost wallet create --chain bittensor): %w", err)
			}

			fmt.Fprintf(os.Stdout, "Address: %s\n\n", signer.Address())

			rao, err := client.GetBalance(c.Context(), signer.Address())
			if err != nil {
				return fmt.Errorf("get balance: %w", err)
			}

			tao := btchain.RAOToTAO(rao)
			fmt.Fprintf(os.Stdout, "TAO Balance: %s TAO (%d RAO)\n", tao.StringFixed(9), rao)

			return nil
		},
	}
}
