// Package ai hides every AI implementation behind one small interface, so the
// rest of the platform never knows (or cares) whether the analysis runs in Go,
// in a Python microservice, or against a different model.
package ai

import (
	"context"
	"strings"
)

type Spec struct {
	Summary  string   `json:"summary"`
	Category string   `json:"category"`
	Features []string `json:"features"`
	Risks    []string `json:"risks"`
}

type Analyzer interface {
	Analyze(ctx context.Context, description string) (*Spec, error)
}

// Stub is for local development without any model running.
type Stub struct{}

func (Stub) Analyze(_ context.Context, description string) (*Spec, error) {
	s := strings.TrimSpace(description)
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "..."
	}
	return &Spec{Summary: s, Category: "other", Features: []string{}, Risks: []string{"stub analyzer: no real analysis"}}, nil
}

// sanitize bounds whatever the model returned before it is stored.
func sanitize(s *Spec) *Spec {
	s.Summary = clip(s.Summary, 600)
	s.Category = clip(s.Category, 40)
	s.Features = clipList(s.Features, 15, 200)
	s.Risks = clipList(s.Risks, 10, 200)
	return s
}

func clip(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

func clipList(in []string, maxItems, maxLen int) []string {
	if len(in) > maxItems {
		in = in[:maxItems]
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, clip(v, maxLen))
	}
	return out
}
