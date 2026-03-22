package provider

import (
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicStopReason(t *testing.T) {
	cases := []struct {
		in   anthropicsdk.StopReason
		want string
	}{
		{anthropicsdk.StopReasonEndTurn, "stop"},
		{anthropicsdk.StopReasonMaxTokens, "length"},
		{anthropicsdk.StopReasonStopSequence, "stop"},
		{"tool_use", "tool_use"},
	}
	for _, c := range cases {
		if got := anthropicStopReason(c.in); got != c.want {
			t.Errorf("anthropicStopReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildAnthropicParams_SystemExtracted(t *testing.T) {
	req := &ChatCompletionRequest{
		Model: "claude-sonnet",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}
	params := buildAnthropicParams(req, "claude-sonnet-4-20250514")

	if len(params.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(params.System))
	}
	if params.System[0].Text != "You are helpful." {
		t.Errorf("wrong system text: %q", params.System[0].Text)
	}
	if len(params.Messages) != 1 {
		t.Fatalf("expected 1 message (user only), got %d", len(params.Messages))
	}
}

func TestBuildAnthropicParams_DefaultMaxTokens(t *testing.T) {
	req := &ChatCompletionRequest{
		Model:    "claude-sonnet",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		// MaxTokens deliberately zero
	}
	params := buildAnthropicParams(req, "claude-sonnet-4-20250514")
	if params.MaxTokens != defaultMaxTokens {
		t.Errorf("expected default %d, got %d", defaultMaxTokens, params.MaxTokens)
	}
}

func TestBuildAnthropicParams_ExplicitMaxTokens(t *testing.T) {
	req := &ChatCompletionRequest{
		Model:     "claude-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 512,
	}
	params := buildAnthropicParams(req, "claude-sonnet-4-20250514")
	if params.MaxTokens != 512 {
		t.Errorf("expected 512, got %d", params.MaxTokens)
	}
}

func TestBuildAnthropicParams_StopSequences(t *testing.T) {
	req := &ChatCompletionRequest{
		Model:    "claude-sonnet",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Stop:     []string{"STOP", "END"},
	}
	params := buildAnthropicParams(req, "claude-sonnet-4-20250514")
	if len(params.StopSequences) != 2 {
		t.Errorf("expected 2 stop sequences, got %d", len(params.StopSequences))
	}
}

func TestBuildAnthropicParams_UpstreamModel(t *testing.T) {
	req := &ChatCompletionRequest{
		Model:    "claude-sonnet",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
	params := buildAnthropicParams(req, "claude-sonnet-4-20250514")
	if string(params.Model) != "claude-sonnet-4-20250514" {
		t.Errorf("expected upstream model, got %q", params.Model)
	}
}

func TestAnthropicToOpenAIUsage(t *testing.T) {
	u := anthropicsdk.Usage{InputTokens: 10, OutputTokens: 20}
	got := anthropicToOpenAIUsage(u)
	if got.PromptTokens != 10 || got.CompletionTokens != 20 || got.TotalTokens != 30 {
		t.Errorf("unexpected usage: %+v", got)
	}
}
