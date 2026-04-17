package funding_arb_basic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/teslashibe/permafrost/internal/inference"
)

// vetoSchema is the JSON Schema the model is asked to conform to.
const vetoSchemaJSON = `{
  "type": "object",
  "properties": {
    "veto":   {"type": "boolean"},
    "reason": {"type": "string", "maxLength": 200}
  },
  "required": ["veto", "reason"],
  "additionalProperties": false
}`

const vetoSystemPrompt = `You are an event-risk filter for a crypto funding-rate arbitrage agent.
The agent wants to OPEN a delta-neutral basis position: long spot on Solana,
short perpetual on Hyperliquid for the same token.

Your job: given the candidate token, decide whether there is a clear,
imminent reason NOT to open this position right now (e.g. token unlock,
governance vote, depeg or rug rumor, exchange listing/delisting, regulatory
action, major exploit). Default to NOT vetoing.

Reply ONLY in the supplied JSON schema. Keep "reason" under 200 chars.`

// vetoResponse mirrors the JSON Schema.
type vetoResponse struct {
	Veto   bool   `json:"veto"`
	Reason string `json:"reason"`
}

// askVeto consults the inference provider with a structured-output schema.
// On any error or unsupported feature, returns (false, "", nil) — the
// strategy errs on the side of NOT vetoing when the LLM is unreachable.
func (s *Strategy) askVeto(ctx context.Context, c candidate) (bool, string, error) {
	if s.inference == nil {
		return false, "", nil
	}
	prompt := fmt.Sprintf(`Candidate: %s
Annualised funding rate: %s
Funding interval: %s
Spot venue: %s

Should we veto opening a basis here?`, c.Symbol, c.Annualised, c.Funding.Interval, c.Asset.Spot.Chain)

	resp, err := s.inference.Complete(ctx, inference.Request{
		Model:  s.cfg.VetoModel,
		System: vetoSystemPrompt,
		Messages: []inference.Message{
			{Role: inference.RoleUser, Content: prompt},
		},
		JSONSchema: &inference.Schema{
			Name:   "veto_decision",
			JSON:   []byte(vetoSchemaJSON),
			Strict: true,
		},
		Temperature: 0,
		MaxTokens:   200,
	})
	if err != nil {
		if errors.Is(err, inference.ErrUnsupportedFeature) {
			return false, "", nil
		}
		return false, "", err
	}

	var v vetoResponse
	if err := json.Unmarshal([]byte(resp.Content), &v); err != nil {
		return false, "", fmt.Errorf("veto: parse model response: %w", err)
	}
	return v.Veto, v.Reason, nil
}
