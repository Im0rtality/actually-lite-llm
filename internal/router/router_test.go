package router

import (
	"testing"

	"github.com/laurynas/actually-lite-llm/internal/config"
)

func TestResolveAlias(t *testing.T) {
	r := New(
		map[string]config.ModelAlias{
			"claude-sonnet": {Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
		},
		nil,
	)

	route, err := r.Resolve("claude-sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "anthropic" {
		t.Errorf("expected anthropic, got %s", route.Provider)
	}
	if route.UpstreamModel != "claude-sonnet-4-20250514" {
		t.Errorf("unexpected upstream model %s", route.UpstreamModel)
	}
}

func TestResolvePrefix(t *testing.T) {
	r := New(nil, []config.RoutingRule{
		{Prefix: "gpt-", Provider: "openai"},
		{Prefix: "claude-", Provider: "anthropic"},
	})

	route, err := r.Resolve("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "openai" {
		t.Errorf("expected openai, got %s", route.Provider)
	}
	if route.UpstreamModel != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", route.UpstreamModel)
	}
}

func TestResolveUnknown(t *testing.T) {
	r := New(nil, nil)
	_, err := r.Resolve("unknown-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestAliasBeatsPrefix(t *testing.T) {
	r := New(
		map[string]config.ModelAlias{
			"claude-sonnet": {Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
		},
		[]config.RoutingRule{
			{Prefix: "claude-", Provider: "openai"}, // alias should win
		},
	)

	route, err := r.Resolve("claude-sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "anthropic" {
		t.Errorf("alias should win: expected anthropic, got %s", route.Provider)
	}
}
