// Package mock provides a deterministic Provider for tests and backtests.
//
// It supports two modes:
//   - Static: returns a fixed Response (set via WithResponse) for every call.
//   - Scripted: returns successive Responses from a queue (set via WithQueue),
//     panicking if the queue is exhausted.
package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/teslashibe/permafrost/pkg/inference"
)

// Name is the registered provider identifier.
const Name = "mock"

// Provider is a mock inference implementation.
//
// Behaviour:
//   - If WithResponse is supplied, every Complete returns that Response.
//   - If WithQueue is supplied, successive Completes return queue entries
//     in order; once exhausted, Complete returns ErrQueueExhausted.
//   - If both are supplied, the queue takes precedence and falls back to
//     the static Response after exhaustion.
//   - If neither is supplied, Complete returns a stable empty Response.
type Provider struct {
	mu          sync.Mutex
	queue       []inference.Response
	static      *inference.Response
	staticSet   bool
	queueSet    bool
	calls       []inference.Request
	embedFn     func(inference.EmbedRequest) (inference.EmbedResponse, error)
}

// Option mutates Provider at construction time.
type Option func(*Provider)

// WithResponse sets a static Response returned for every Complete call.
func WithResponse(r inference.Response) Option {
	return func(p *Provider) {
		p.static = &r
		p.staticSet = true
	}
}

// WithQueue enqueues Responses returned in order. Once exhausted, subsequent
// calls return ErrQueueExhausted (unless WithResponse was also supplied, in
// which case the static Response is used as a fallback).
func WithQueue(rs ...inference.Response) Option {
	return func(p *Provider) {
		p.queue = append(p.queue, rs...)
		p.queueSet = true
	}
}

// WithEmbedFunc installs a custom embedder.
func WithEmbedFunc(fn func(inference.EmbedRequest) (inference.EmbedResponse, error)) Option {
	return func(p *Provider) { p.embedFn = fn }
}

// New constructs a mock Provider.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Compile-time check.
var _ inference.Provider = (*Provider)(nil)

func (p *Provider) Name() string { return Name }

// ErrQueueExhausted is returned when WithQueue is set and the queue is empty.
var ErrQueueExhausted = errors.New("mock: response queue exhausted")

func (p *Provider) Complete(_ context.Context, req inference.Request) (inference.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, req)

	if len(p.queue) > 0 {
		next := p.queue[0]
		p.queue = p.queue[1:]
		return next, nil
	}
	if p.queueSet && !p.staticSet {
		return inference.Response{}, ErrQueueExhausted
	}
	if p.staticSet {
		return *p.static, nil
	}
	return inference.Response{Provider: Name, Model: "mock", FinishReason: "stop"}, nil
}

func (p *Provider) Embed(_ context.Context, req inference.EmbedRequest) (inference.EmbedResponse, error) {
	if p.embedFn != nil {
		return p.embedFn(req)
	}
	return inference.EmbedResponse{Provider: Name, Model: req.Model}, nil
}

// Calls returns a snapshot of the requests received by Complete.
func (p *Provider) Calls() []inference.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]inference.Request, len(p.calls))
	copy(out, p.calls)
	return out
}
