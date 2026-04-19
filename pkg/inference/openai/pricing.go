package openai

import "strings"

// modelPrice describes per-million-token pricing in USD.
type modelPrice struct {
	InputPerM  float64
	OutputPerM float64
}

// pricing is a best-effort lookup table for common models. Unknown models
// return (0, 0) and CostUSD will be reported as 0.
//
// Provider-specific routing (e.g. OpenRouter) often passes through the
// upstream model name; we match on the most-specific suffix first. Numbers
// are illustrative defaults; operators should override via their own
// pricing source for accurate accounting.
var pricing = map[string]modelPrice{
	// OpenAI
	"gpt-5":              {InputPerM: 5.00, OutputPerM: 15.00},
	"gpt-5-mini":         {InputPerM: 0.25, OutputPerM: 1.00},
	"gpt-4o":             {InputPerM: 2.50, OutputPerM: 10.00},
	"gpt-4o-mini":        {InputPerM: 0.15, OutputPerM: 0.60},
	"o3":                 {InputPerM: 2.00, OutputPerM: 8.00},
	"o3-mini":            {InputPerM: 1.10, OutputPerM: 4.40},
	"text-embedding-3-small": {InputPerM: 0.02},
	"text-embedding-3-large": {InputPerM: 0.13},

	// Anthropic via OpenRouter / native
	"claude-sonnet-4.5":     {InputPerM: 3.00, OutputPerM: 15.00},
	"claude-opus-4.1":       {InputPerM: 15.00, OutputPerM: 75.00},
	"claude-3.7-sonnet":     {InputPerM: 3.00, OutputPerM: 15.00},
	"claude-3.5-sonnet":     {InputPerM: 3.00, OutputPerM: 15.00},
	"claude-3.5-haiku":      {InputPerM: 0.80, OutputPerM: 4.00},

	// Google
	"gemini-2.5-pro":  {InputPerM: 1.25, OutputPerM: 10.00},
	"gemini-2.5-flash": {InputPerM: 0.30, OutputPerM: 2.50},

	// Open / hosted-OSS (typical Groq/Together pricing)
	"llama-3.1-70b": {InputPerM: 0.59, OutputPerM: 0.79},
	"llama-3.1-8b":  {InputPerM: 0.05, OutputPerM: 0.08},
	"deepseek-chat": {InputPerM: 0.27, OutputPerM: 1.10},

	// Local (Ollama, vLLM, LM Studio): zero by definition
	"local": {InputPerM: 0, OutputPerM: 0},
}

// estimateCost computes a USD cost from token usage and a model name.
// Returns 0 if the model is unknown. Matching is suffix-based to handle
// provider prefixes like "anthropic/claude-sonnet-4.5".
func estimateCost(model string, in, out int) float64 {
	if p, ok := pricing[model]; ok {
		return cost(p, in, out)
	}
	for k, p := range pricing {
		if strings.HasSuffix(model, k) {
			return cost(p, in, out)
		}
	}
	return 0
}

func cost(p modelPrice, in, out int) float64 {
	return float64(in)/1_000_000*p.InputPerM + float64(out)/1_000_000*p.OutputPerM
}
