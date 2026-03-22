package provider

import (
	"context"
	"net/http"
)

// ChatMessage mirrors the OpenAI chat message format.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the OpenAI-compatible request body.
type ChatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	Temperature    *float64      `json:"temperature,omitempty"`
	Stream         bool          `json:"stream"`
	Stop           []string      `json:"stop,omitempty"`
	TopP           *float64      `json:"top_p,omitempty"`
	N              int           `json:"n,omitempty"`
	User           string        `json:"user,omitempty"`
}

// ChatCompletionChoice is one completion choice.
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage holds token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse is the OpenAI-compatible response body.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

// Provider is the interface every upstream provider must implement.
type Provider interface {
	// Chat performs a non-streaming chat completion and writes the response to w.
	Chat(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter) (*Usage, error)

	// ChatStream performs a streaming chat completion, writing SSE to w.
	// It calls onFirstByte once the first chunk has been written.
	ChatStream(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter, onFirstByte func()) error
}
