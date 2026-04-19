package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/inference/openai"
)

func init() { addCommandFactory(newInferenceCmd) }

func newInferenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inference",
		Short: "Inference provider utilities",
	}
	cmd.AddCommand(newInferenceTestCmd(), newInferenceListCmd())
	return cmd
}

func newInferenceTestCmd() *cobra.Command {
	var (
		providerName string
		model        string
		prompt       string
	)
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a one-shot completion to a configured provider",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			reg, err := inference.NewRegistry(g.Config.Inference, openai.NewProvider)
			if err != nil {
				return err
			}
			var p inference.Provider
			if providerName == "" {
				p, err = reg.Default()
			} else {
				p, err = reg.Get(providerName)
			}
			if err != nil {
				return err
			}
			resp, err := p.Complete(c.Context(), inference.Request{
				Model: model,
				Messages: []inference.Message{
					{Role: inference.RoleUser, Content: prompt},
				},
				MaxTokens: 200,
			})
			if err != nil {
				return err
			}
			fmt.Printf("provider:    %s\nmodel:       %s\nfinish:      %s\nlatency_ms:  %d\ntokens_in:   %d\ntokens_out:  %d\ncost_usd:    %.6f\n\n%s\n",
				resp.Provider, resp.Model, resp.FinishReason, resp.LatencyMS,
				resp.TokensIn, resp.TokensOut, resp.CostUSD, resp.Content)
			return nil
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "provider name (defaults to inference.default)")
	cmd.Flags().StringVar(&model, "model", "", "model identifier (provider-specific). If empty, the provider's API will use its own default model — handy for a quick connectivity smoke test.")
	cmd.Flags().StringVar(&prompt, "prompt", "Say hello in exactly five words.", "prompt to send")
	// --model is deliberately optional: a no-model smoke test exercises
	// the auth + transport path even if the provider rejects the empty
	// model field with a clear error. Providers that require a model
	// will surface their own message; that's still more useful than
	// "missing required flag" before any network call happens.
	return cmd
}

func newInferenceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured inference providers",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			reg, err := inference.NewRegistry(g.Config.Inference, openai.NewProvider)
			if err != nil {
				return err
			}
			defName := reg.DefaultName()
			for _, n := range reg.Names() {
				marker := " "
				if n == defName {
					marker = "*"
				}
				fmt.Printf("%s %s\n", marker, n)
			}
			return nil
		},
	}
}
