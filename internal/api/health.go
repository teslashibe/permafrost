package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// HealthResponse is the JSON shape returned from /v1/health.
type HealthResponse struct {
	Status   string                 `json:"status"`              // ok | degraded
	Service  string                 `json:"service"`             // permafrostd
	Time     time.Time              `json:"time"`                // server time, RFC3339
	Checks   map[string]CheckResult `json:"checks"`              // dependency checks
	Version  string                 `json:"version,omitempty"`   // build version (set at link time)
}

// CheckResult is the per-dependency health summary.
type CheckResult struct {
	Status string `json:"status"`         // ok | error | unconfigured
	Detail string `json:"detail,omitempty"`
}

// Build version, populated via -ldflags at link time.
var buildVersion = "dev"

func (s *Server) registerRoutes() {
	v1 := s.app.Group("/v1")
	v1.Get("/health", s.healthHandler)
	v1.Get("/agents", s.listAgentsHandler)
	v1.Get("/agents/:id/decisions", s.listDecisionsHandler)
}

func (s *Server) healthHandler(c *fiber.Ctx) error {
	checks := map[string]CheckResult{}
	overall := "ok"

	if s.db == nil {
		checks["database"] = CheckResult{Status: "unconfigured"}
		overall = "degraded"
	} else {
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			checks["database"] = CheckResult{Status: "error", Detail: err.Error()}
			overall = "degraded"
		} else {
			checks["database"] = CheckResult{Status: "ok"}
		}
	}

	resp := HealthResponse{
		Status:  overall,
		Service: "permafrostd",
		Time:    time.Now().UTC(),
		Checks:  checks,
		Version: buildVersion,
	}
	return c.JSON(resp)
}
