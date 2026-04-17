package inference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/internal/inference/mock"
)

func TestMock_Static(t *testing.T) {
	want := inference.Response{Content: "hello", Provider: "mock", Model: "m"}
	p := mock.New(mock.WithResponse(want))

	got, err := p.Complete(context.Background(), inference.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Content != want.Content {
		t.Errorf("Content: got %q want %q", got.Content, want.Content)
	}
	calls := p.Calls()
	if len(calls) != 1 || calls[0].Model != "m" {
		t.Errorf("Calls: got %+v", calls)
	}
}

func TestMock_Queue(t *testing.T) {
	p := mock.New(mock.WithQueue(
		inference.Response{Content: "first"},
		inference.Response{Content: "second"},
	))

	for i, want := range []string{"first", "second"} {
		got, err := p.Complete(context.Background(), inference.Request{})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got.Content != want {
			t.Errorf("call %d: got %q want %q", i, got.Content, want)
		}
	}

	if _, err := p.Complete(context.Background(), inference.Request{}); !errors.Is(err, mock.ErrQueueExhausted) {
		t.Fatalf("expected ErrQueueExhausted, got %v", err)
	}
}

func TestSentinelErrors(t *testing.T) {
	if inference.ErrUnsupportedFeature == nil {
		t.Fatal("ErrUnsupportedFeature must be non-nil")
	}
	if inference.ErrRateLimited == nil {
		t.Fatal("ErrRateLimited must be non-nil")
	}
}
