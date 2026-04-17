package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/swap/jupiter"
	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

func init() { addCommandFactory(newSwapCmd) }

func newSwapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap",
		Short: "Smoke-test on-chain swap routes (Solana / Jupiter)",
	}
	cmd.AddCommand(newSwapQuoteCmd())
	return cmd
}

func newSwapQuoteCmd() *cobra.Command {
	var (
		inMint   string
		outMint  string
		inDec    int32
		outDec   int32
		amount   string
		slippage int
	)
	cmd := &cobra.Command{
		Use:   "quote",
		Short: "Get a Jupiter quote for a Solana swap (no transaction signed)",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			if g.Config.Solana.RPCURL == "" {
				return errors.New("solana.rpc_url is not set in config")
			}
			ks, err := openKeystore(c)
			if err != nil {
				return err
			}
			signer, err := ks.Signer(types.ChainSolana)
			if err != nil {
				return fmt.Errorf("solana signer: %w", err)
			}
			amt, err := decimal.NewFromString(amount)
			if err != nil {
				return fmt.Errorf("amount: %w", err)
			}

			venueCfg := jupiter.Config{
				RPCURL:                  g.Config.Solana.RPCURL,
				JupiterAPIKey:           g.Config.Solana.JupiterAPIKey,
				JupiterBaseURL:          g.Config.Solana.JupiterBaseURL,
				Mode:                    jupiter.SubmitMode(defaultStr(g.Config.Solana.SubmitMode, string(jupiter.SubmitJito))),
				JitoBundleURL:           g.Config.Solana.JitoBundleURL,
				PriorityFeeMicroLamports: g.Config.Solana.PriorityFeeMicroLamports,
				PollTimeout:             time.Duration(g.Config.Solana.ConfirmationTimeoutSecs) * time.Second,
			}
			v, err := jupiter.New(venueCfg, signer)
			if err != nil {
				return err
			}
			req := types.QuoteRequest{
				InToken:  types.Asset{Symbol: "in", Chain: types.ChainSolana, Mint: inMint, Decimals: inDec},
				OutToken: types.Asset{Symbol: "out", Chain: types.ChainSolana, Mint: outMint, Decimals: outDec},
				Amount:   amt,
				Mode:     types.QuoteExactIn,
			}
			q, err := v.Quote(c.Context(), req)
			if err != nil {
				return err
			}
			_ = slippage // (slippage is not yet plumbed into Quote; reserved for Swap)
			fmt.Printf("route:        %s\nin:           %s %s\nout:          %s %s\nprice impact: %s\nexpires:      %s\n",
				q.Route, q.InAmount, inMint, q.OutAmount, outMint, q.PriceImpact, q.ExpiresAt.Format(time.RFC3339))
			_ = wallet.Signer(signer)
			return nil
		},
	}
	cmd.Flags().StringVar(&inMint, "in-mint", "", "input mint (e.g. EPjFW...USDC)")
	cmd.Flags().Int32Var(&inDec, "in-decimals", 6, "input decimals")
	cmd.Flags().StringVar(&outMint, "out-mint", "", "output mint")
	cmd.Flags().Int32Var(&outDec, "out-decimals", 6, "output decimals")
	cmd.Flags().StringVar(&amount, "amount", "1", "amount in human units (e.g. 1.5)")
	cmd.Flags().IntVar(&slippage, "slippage-bps", 50, "slippage budget (bps)")
	_ = cmd.MarkFlagRequired("in-mint")
	_ = cmd.MarkFlagRequired("out-mint")
	return cmd
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
