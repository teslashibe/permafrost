package cli

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
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
		newBittensorNoiseTraderCmd(),
	)
	return cmd
}

// newBittensorNoiseTraderCmd runs a small loop that periodically submits
// random buy/sell extrinsics across a configured subnet universe.
//
// Purpose: create realistic volatility in the AMM pools of a local
// devnet so momentum + yield strategies have a meaningful signal to
// react to. Without this, the only price pressure on devnet comes
// from agents themselves — which on a fresh chain is a single DCA
// strategy buying uniformly across subnets, producing little
// per-subnet divergence.
//
// Behaviour per cycle:
//  1. Pick a random subnet from --subnets.
//  2. Pick a random direction (BUY weighted heavier than SELL because
//     fresh wallets start with TAO not alpha).
//  3. Pick a random TAO amount in [--min-tao .. --max-tao].
//  4. Submit add_stake_limit (BUY) or remove_stake_limit (SELL).
//  5. Sleep --interval-secs (jittered ±50%).
//  6. Loop until SIGINT.
//
// Devnet helper. Refuses to run against non-localhost RPCs.
func newBittensorNoiseTraderCmd() *cobra.Command {
	var (
		fromURI       string
		subnetsCSV    string
		intervalSecs  int
		minTAOFloat   float64
		maxTAOFloat   float64
		buyWeight     int
	)
	cmd := &cobra.Command{
		Use:   "noise-trader",
		Short: "Random buy/sell loop to create volatility on devnet AMM pools (devnet only)",
		Long: `noise-trader runs a loop that submits random buy/sell extrinsics
across a chosen subnet universe. It exists to give momentum + yield
strategies a realistic price signal to trade against on a local devnet.

Refuses to run against non-localhost RPCs. Stop with SIGINT.`,
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return fmt.Errorf("globals not initialised")
			}
			rpcURL := g.Config.Bittensor.ResolvedRPCURL()
			if !strings.Contains(rpcURL, "localhost") && !strings.Contains(rpcURL, "127.0.0.1") {
				return fmt.Errorf("noise-trader is a devnet helper; refusing to run against %q", rpcURL)
			}
			if subnetsCSV == "" {
				return fmt.Errorf("--subnets is required (e.g. --subnets 2,3,4)")
			}
			subnets, err := parseSubnetsCSV(subnetsCSV)
			if err != nil {
				return err
			}
			if intervalSecs < 1 {
				return fmt.Errorf("--interval-secs must be >= 1")
			}
			if minTAOFloat <= 0 || maxTAOFloat < minTAOFloat {
				return fmt.Errorf("--min-tao must be > 0 and --max-tao must be >= --min-tao")
			}
			if buyWeight < 0 || buyWeight > 100 {
				return fmt.Errorf("--buy-weight must be in [0..100]")
			}

			signer, err := wallet.NewBittensorSignerFromURI(fromURI)
			if err != nil {
				return fmt.Errorf("derive signer from URI %q: %w", fromURI, err)
			}
			fmt.Fprintf(os.Stdout, "noise-trader starting as %s (URI %s)\n", signer.Address(), fromURI)
			fmt.Fprintf(os.Stdout, "  subnets:       %v\n", subnets)
			fmt.Fprintf(os.Stdout, "  interval:      %ds (±50%% jitter)\n", intervalSecs)
			fmt.Fprintf(os.Stdout, "  amount range:  %.3f .. %.3f TAO\n", minTAOFloat, maxTAOFloat)
			fmt.Fprintf(os.Stdout, "  buy weight:    %d%% (rest sells)\n\n", buyWeight)

			client := btchain.NewClient(rpcURL)
			defer client.Close()

			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			cycle := 0
			for {
				if c.Context().Err() != nil {
					return nil
				}
				cycle++
				netuid := subnets[rng.Intn(len(subnets))]
				isBuy := rng.Intn(100) < buyWeight
				amtRange := maxTAOFloat - minTAOFloat
				amt := minTAOFloat + rng.Float64()*amtRange
				rao := uint64(amt * 1e9)
				if rao == 0 {
					rao = 1
				}

				start := time.Now()
				var (
					action string
					err    error
				)
				if isBuy {
					action = "BUY "
					_, err = client.AddStake(c.Context(), signer, netuid, signer.PublicKey(), rao, btchain.SubmitOptions{
						WaitTimeout: 20 * time.Second,
					})
				} else {
					action = "SELL"
					_, err = client.RemoveStake(c.Context(), signer, netuid, signer.PublicKey(), rao, btchain.SubmitOptions{
						WaitTimeout: 20 * time.Second,
					})
				}
				dur := time.Since(start).Truncate(time.Millisecond)
				if err != nil {
					// Sells will commonly fail on a hotkey with zero
					// alpha. That's fine — log and keep going.
					fmt.Fprintf(os.Stdout, "[%04d] %s SN%d %.4f TAO  err=%v (%s)\n",
						cycle, action, netuid, amt, truncErr(err), dur)
				} else {
					fmt.Fprintf(os.Stdout, "[%04d] %s SN%d %.4f TAO  ok (%s)\n",
						cycle, action, netuid, amt, dur)
				}

				// Jitter sleep ±50%.
				base := time.Duration(intervalSecs) * time.Second
				jitter := time.Duration(rng.Int63n(int64(base)) - int64(base/2))
				select {
				case <-c.Context().Done():
					return nil
				case <-time.After(base + jitter):
				}
			}
		},
	}
	cmd.Flags().StringVar(&fromURI, "from-uri", "//Alice", "Substrate URI for the noise-trader signer")
	cmd.Flags().StringVar(&subnetsCSV, "subnets", "", "comma-separated netuids to trade across (e.g. 2,3,4)")
	cmd.Flags().IntVar(&intervalSecs, "interval-secs", 5, "base seconds between submissions (jittered ±50%)")
	cmd.Flags().Float64Var(&minTAOFloat, "min-tao", 0.05, "minimum TAO amount per submission")
	cmd.Flags().Float64Var(&maxTAOFloat, "max-tao", 0.4, "maximum TAO amount per submission")
	cmd.Flags().IntVar(&buyWeight, "buy-weight", 65, "percentage chance of BUY (rest is SELL)")
	return cmd
}

// parseSubnetsCSV parses "2,3,4" into []uint16{2,3,4}.
func parseSubnetsCSV(s string) ([]uint16, error) {
	parts := strings.Split(s, ",")
	out := make([]uint16, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("subnets: %q is not a valid u16: %w", p, err)
		}
		out = append(out, uint16(n))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("subnets: at least one netuid required")
	}
	return out, nil
}

// truncErr collapses the gsrpc/Substrate error spam to one line.
func truncErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
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
