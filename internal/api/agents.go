package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/teslashibe/permafrost/internal/agent"
)

// AgentLite is the JSON shape returned by GET /v1/agents. Mirrors the
// TypeScript `Agent` interface in apps/desk/src/api/client.ts; if you
// change one, change both. Field names are snake_case to match the
// rest of the API and what the UI already expects.
type AgentLite struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Strategy   string    `json:"strategy"`
	PerpVenue  string    `json:"perp_venue"`
	SpotVenue  string    `json:"spot_venue"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	AllocUSD   string    `json:"alloc_usd"`
	Network    string    `json:"network"`
	TickSecs   int       `json:"tick_secs"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DecisionLite is the JSON shape returned by GET /v1/agents/:id/decisions.
// Mirrors the TypeScript `DecisionLite` interface in
// apps/desk/src/api/client.ts.
//
// num_orders/num_swaps are scaffolded for the UI's penguin-walk
// animations; we don't yet persist them per-decision so they're
// derived from the rationale string heuristically (look for the
// "swaps=N orders=N cancels=N" tail emitted by Runtime.tick) or
// reported as 0 when absent.
type DecisionLite struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	TS        time.Time `json:"ts"`
	Confidence float64  `json:"confidence"`
	Notes     string    `json:"notes"`
	NumOrders int       `json:"num_orders"`
	NumSwaps  int       `json:"num_swaps"`
	LLMUsed   bool      `json:"llm_used"`
}

// listAgentsHandler returns every agent in the database in newest-first
// order. The UI polls this every ~3s; the response is intentionally
// small (one row per agent, no decisions inlined).
func (s *Server) listAgentsHandler(c *fiber.Ctx) error {
	if s.agents == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "agent store unavailable (database not configured?)")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	agents, err := s.agents.List(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "list agents: "+err.Error())
	}
	out := make([]AgentLite, 0, len(agents))
	for _, a := range agents {
		out = append(out, AgentLite{
			ID:        a.ID,
			Name:      a.Name,
			Strategy:  a.Strategy,
			PerpVenue: a.PerpVenue,
			SpotVenue: a.SpotVenue,
			Mode:      string(a.Mode),
			Status:    string(a.Status),
			AllocUSD:  a.AllocationUSDC.StringFixed(2),
			Network:   string(a.Network),
			TickSecs:  a.TickSecs,
			UpdatedAt: a.UpdatedAt.UTC(),
		})
	}
	return c.JSON(out)
}

// listDecisionsHandler returns the recent decisions for one agent.
// limit query param clamps to [1..200] (default 20). The "since" window
// is fixed at 24h server-side to keep the response bounded.
func (s *Server) listDecisionsHandler(c *fiber.Ctx) error {
	if s.agents == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "agent store unavailable (database not configured?)")
	}
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent id required")
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()

	// Verify the agent exists so we return a clean 404 for unknown ids
	// rather than an empty list (which would look like "agent has no
	// decisions yet" — a different and confusing UX).
	if _, err := s.agents.Get(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) ||
			strings.Contains(err.Error(), "no rows in result set") {
			return fiber.NewError(fiber.StatusNotFound, "agent not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	since := time.Now().UTC().Add(-24 * time.Hour)
	rows, err := s.agents.RecentDecisions(ctx, id, since, limit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "decisions: "+err.Error())
	}
	out := make([]DecisionLite, 0, len(rows))
	for _, r := range rows {
		out = append(out, DecisionLite{
			ID:         r.DecisionID,
			AgentID:    id,
			TS:         r.Time.UTC(),
			Confidence: 0, // not persisted in agent_decisions today
			Notes:      r.Rationale,
			NumOrders:  r.NumOrders,
			NumSwaps:   r.NumSwaps,
			LLMUsed:    r.Provider != "",
		})
	}
	return c.JSON(out)
}

// _ ensures the agent package import is not optimised out when the
// handlers are stripped at compile time (e.g. test-only build tags).
var _ = agent.Mode("")
