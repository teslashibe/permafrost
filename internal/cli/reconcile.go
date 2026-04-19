package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/reconcile"
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/inference/openai"
)

func init() { addCommandFactory(newReconcileCmd) }

func newReconcileCmd() *cobra.Command {
	var (
		agentID   string
		jsonOut   bool
		failOnDrift bool
	)
	cmd := &cobra.Command{
		Use:   "reconcile [agent-id]",
		Short: "Verify the agent's books match what the venues actually report",
		Long: `Walks each open basis position and checks both legs against the
venues. Catches half-open positions, missed fills, and bookkeeping
bugs early — before they bleed money.

Without an agent id, reconciles every agent. Returns a non-zero exit
code (when --fail-on-drift is set) so this can be wired into a cron or
CI job.`,
		RunE: func(c *cobra.Command, args []string) error {
			if agentID == "" && len(args) == 1 {
				agentID = args[0]
			}
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := c.Context()

			var agents []agent.Agent
			if agentID != "" {
				a, err := s.Get(ctx, agentID)
				if err != nil {
					return err
				}
				agents = []agent.Agent{a}
			} else {
				agents, err = s.List(ctx)
				if err != nil {
					return err
				}
			}

			g := FromContext(ctx)
			if g == nil {
				return errors.New("globals not initialised")
			}
			reg, err := assets.LoadEmbedded()
			if err != nil {
				return err
			}
			ks, _ := openKeystore(c) // best-effort
			infReg, _ := inference.NewRegistry(g.Config.Inference, openai.NewProvider)

			combined := reconcile.Report{}
			for _, a := range agents {
				deps, err := agent.BuildDeps(a, reg, s, ks, infReg, g.Log, agent.BuildOptions{
					Solana: solanaSpotFromConfig(g.Config.Solana),
					EVM:    evmSpotsFromConfig(g.Config.EVM, os.Getenv),
				})
				if err != nil {
					return fmt.Errorf("build deps for %s: %w", a.ID, err)
				}
				open, err := s.LoadOpenBasis(ctx, a.ID)
				if err != nil {
					return err
				}
				eng := reconcile.New(deps.Perp, deps.Swaps)
				rep, err := eng.Reconcile(ctx, a.ID, open)
				if err != nil {
					return err
				}
				combined.OpenBasis += rep.OpenBasis
				combined.OK += rep.OK
				combined.Drifts = append(combined.Drifts, rep.Drifts...)
				combined.AgentsCount++
			}
			combined.GeneratedAt = time.Now().UTC()

			if jsonOut {
				return writeJSON(combined)
			}
			renderReconcileReport(combined)
			if failOnDrift && combined.HasIssues() {
				return errors.New("reconcile: drift detected")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent id (alias for positional arg)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON report")
	cmd.Flags().BoolVar(&failOnDrift, "fail-on-drift", false, "exit non-zero when warnings/critical drift is found (cron-friendly)")
	return cmd
}

func renderReconcileReport(r reconcile.Report) {
	fmt.Printf("Reconciled %d agents — %d open positions — %d clean — %d drift entries\n\n",
		r.AgentsCount, r.OpenBasis, r.OK, len(r.Drifts))
	if len(r.Drifts) == 0 {
		fmt.Println("All clean.")
		return
	}
	sort.Slice(r.Drifts, func(i, j int) bool {
		// Critical first, then warning, then info; tiebreak by basis name.
		si := severityRank(r.Drifts[i].Severity)
		sj := severityRank(r.Drifts[j].Severity)
		if si != sj {
			return si < sj
		}
		return r.Drifts[i].BasisKey < r.Drifts[j].BasisKey
	})
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEV\tAGENT\tBASIS\tLEG\tFIELD\tEXPECTED\tACTUAL\tNOTE")
	for _, d := range r.Drifts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Severity, d.AgentID, d.BasisKey, d.Leg, d.Field,
			d.Expected.String(), d.Actual.String(), d.Note,
		)
	}
	_ = w.Flush()
}

func severityRank(s reconcile.Severity) int {
	switch s {
	case reconcile.SeverityCritical:
		return 0
	case reconcile.SeverityWarning:
		return 1
	default:
		return 2
	}
}
