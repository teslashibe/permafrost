package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/internal/wallet"
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
		newBittensorBootstrapCmd(),
	)
	return cmd
}

// newBittensorBootstrapCmd creates a brand-new tradeable subnet on the
// configured Subtensor endpoint. Mainly a devnet helper — community
// testers running their own subtensor docker image can use this to get
// an alpha-stakeable subnet without manually invoking 3+ extrinsics.
//
// Flow: register_network → start_call. The subnet's owner is the
// signer (default //Alice for devnet). Prints the new netuid so the
// caller can pipe it into agent configs.
func newBittensorBootstrapCmd() *cobra.Command {
	var (
		fromURI string
		count   int
	)
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create + activate fresh tradeable subnet(s) (devnet helper)",
		Long: `Bootstrap creates one or more new subnets via SubtensorModule.register_network
and activates each via start_call. Each new subnet has a populated AMM pool
and accepts add_stake immediately.

--count N creates N subnets in sequence so a multi-subnet universe can be
spun up in a single command (used by make demo-bittensor to give the
momentum/yield strategies a real choice of subnets to trade).

Use against a local subtensor-localnet docker image. The default signer
is //Alice (the well-known devnet sudo account).

Mainnet usage: don't. The registration burn cost on mainnet is several
hundred TAO per subnet.`,
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			if count < 1 {
				return fmt.Errorf("--count must be >= 1, got %d", count)
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			if !strings.Contains(rpcURL, "localhost") && !strings.Contains(rpcURL, "127.0.0.1") {
				fmt.Fprintf(os.Stderr, "WARNING: bootstrap is a devnet helper. RPC %q does not look local.\n", rpcURL)
				fmt.Fprintf(os.Stderr, "         Press Ctrl-C in 5s to abort, or wait to continue.\n\n")
				time.Sleep(5 * time.Second)
			}

			signer, err := wallet.NewBittensorSignerFromURI(fromURI)
			if err != nil {
				return fmt.Errorf("derive signer from URI %q: %w", fromURI, err)
			}
			fmt.Fprintf(os.Stdout, "Bootstrapping %d subnet(s) as %s (URI %s)\n",
				count, signer.Address(), fromURI)

			client := btchain.NewClient(rpcURL)
			defer client.Close()

			netuids := make([]uint16, 0, count)
			for i := 0; i < count; i++ {
				countBefore, err := client.SubnetCount(c.Context())
				if err != nil {
					return fmt.Errorf("subnet count: %w", err)
				}
				fmt.Fprintf(os.Stdout, "\n[%d/%d] → register_network …\n", i+1, count)
				if _, err := client.RegisterNetwork(c.Context(), signer, signer.PublicKey(), btchain.SubmitOptions{
					WaitTimeout: 30 * time.Second,
				}); err != nil {
					return fmt.Errorf("register_network: %w", err)
				}
				time.Sleep(2 * time.Second)

				countAfter, err := client.SubnetCount(c.Context())
				if err != nil {
					return fmt.Errorf("subnet count after: %w", err)
				}
				if countAfter <= countBefore {
					return fmt.Errorf("subnet count did not grow: %d → %d", countBefore, countAfter)
				}
				netuid := countAfter - 1
				fmt.Fprintf(os.Stdout, "      ✓ new subnet netuid = %d\n", netuid)

				fmt.Fprintln(os.Stdout, "      → start_call …")
				if _, err := client.StartCall(c.Context(), signer, netuid, btchain.SubmitOptions{
					WaitTimeout: 30 * time.Second,
				}); err != nil {
					return fmt.Errorf("start_call netuid=%d: %w", netuid, err)
				}
				time.Sleep(2 * time.Second)

				if price, err := client.CurrentAlphaPrice(c.Context(), netuid); err == nil {
					fmt.Fprintf(os.Stdout, "      ✓ AMM live: SN%d alpha price = %s TAO\n",
						netuid, price.StringFixed(9))
				}
				netuids = append(netuids, netuid)
			}

			// Build the subnets list as JSON for easy consumption by
			// scripts (`subnets: [2,3,4]`).
			parts := make([]string, len(netuids))
			for i, n := range netuids {
				parts[i] = fmt.Sprintf("%d", n)
			}
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintf(os.Stdout, "%d subnet(s) ready. Configure strategies with subnets: [%s]\n",
				len(netuids), strings.Join(parts, ","))
			return nil
		},
	}
	cmd.Flags().StringVar(&fromURI, "from-uri", "//Alice", "Substrate URI for the bootstrap signer")
	cmd.Flags().IntVar(&count, "count", 1, "number of subnets to create (default 1)")
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
