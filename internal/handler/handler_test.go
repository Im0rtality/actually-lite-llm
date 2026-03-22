package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"
	"os"

	"github.com/laurynas/actually-lite-llm/internal/auth"
	"github.com/laurynas/actually-lite-llm/internal/config"
	"github.com/laurynas/actually-lite-llm/internal/provider"
	"github.com/laurynas/actually-lite-llm/internal/router"
)

// stubProvider implements provider.Provider for testing.
type stubProvider struct {
	usage *provider.Usage
	err   error
}

func (s *stubProvider) Chat(_ context.Context, _ *provider.ChatCompletionRequest, _ string, w http.ResponseWriter) (*provider.Usage, error) {
	if s.err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return nil, s.err
	}
	resp := provider.ChatCompletionResponse{
		ID:     "test-id",
		Object: "chat.completion",
		Model:  "gpt-4o",
		Choices: []provider.ChatCompletionChoice{
			{Index: 0, Message: provider.ChatMessage{Role: "assistant", Content: "hello"}, FinishReason: "stop"},
		},
		Usage: *s.usage,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	return s.usage, nil
}

func (s *stubProvider) ChatStream(_ context.Context, _ *provider.ChatCompletionRequest, _ string, w http.ResponseWriter, onFirstByte func()) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	onFirstByte()
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	return s.err
}

func makeHandler() *Handler {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	a := auth.New([]config.APIKey{
		{Key: "good-key", App: "testapp", AllowedModels: []string{"*"}},
		{Key: "restricted", App: "restricted-app", AllowedModels: []string{"gpt-4o"}},
	})
	r := router.New(
		map[string]config.ModelAlias{
			"gpt-4o": {Provider: "openai", Model: "gpt-4o"},
		},
		nil,
	)
	p := Providers{
		"openai": &stubProvider{usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	}
	return New(a, r, p, logger)
}

func TestChatCompletions_Unauthorized(t *testing.T) {
	h := makeHandler()
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer bad-key")
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestChatCompletions_Forbidden(t *testing.T) {
	h := makeHandler()
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	// restricted key only allows gpt-4o... but we ask for gpt-4o which is allowed
	// Let's test with an actually disallowed model
	body2, _ := json.Marshal(map[string]interface{}{
		"model":    "claude-sonnet",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer restricted")
	w2 := httptest.NewRecorder()
	h.ChatCompletions(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w2.Code)
	}
	_ = req
	_ = body
}

func TestChatCompletions_OK(t *testing.T) {
	h := makeHandler()
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer good-key")
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_Stream(t *testing.T) {
	h := makeHandler()
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer good-key")
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected SSE content type, got %s", w.Header().Get("Content-Type"))
	}
}
