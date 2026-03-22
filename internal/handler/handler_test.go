package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"
	"os"

	"github.com/im0rtality/actually-lite-llm/internal/auth"
	"github.com/im0rtality/actually-lite-llm/internal/config"
	"github.com/im0rtality/actually-lite-llm/internal/provider"
	"github.com/im0rtality/actually-lite-llm/internal/router"
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

const (
	goodKey       = "sk-test-good-key-1234"
	restrictedKey = "sk-test-restricted-01"
)

func makeHandler() *Handler {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	a := auth.New([]config.APIKey{
		{Key: goodKey, App: "testapp", AllowedModels: []string{"*"}},
		{Key: restrictedKey, App: "restricted-app", AllowedModels: []string{"gpt-4o"}},
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

func post(h *Handler, body interface{}, authKey string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)
	return w
}

func TestChatCompletions_Unauthorized(t *testing.T) {
	h := makeHandler()
	w := post(h, map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, "bad-key")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestChatCompletions_Forbidden(t *testing.T) {
	h := makeHandler()
	w := post(h, map[string]interface{}{
		"model":    "claude-sonnet",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, restrictedKey)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestChatCompletions_UnknownModel(t *testing.T) {
	h := makeHandler()
	w := post(h, map[string]interface{}{
		"model":    "unknown-model",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, goodKey)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_BadBody(t *testing.T) {
	h := makeHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer "+goodKey)
	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_ProviderError(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	a := auth.New([]config.APIKey{
		{Key: goodKey, App: "testapp", AllowedModels: []string{"*"}},
	})
	r := router.New(map[string]config.ModelAlias{
		"gpt-4o": {Provider: "openai", Model: "gpt-4o"},
	}, nil)
	p := Providers{
		"openai": &stubProvider{err: errors.New("upstream down"), usage: &provider.Usage{}},
	}
	h := New(a, r, p, logger)
	w := post(h, map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, goodKey)
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_OK(t *testing.T) {
	h := makeHandler()
	w := post(h, map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func getModels(h *Handler, authKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	w := httptest.NewRecorder()
	h.Models(w, req)
	return w
}

func TestModels_Unauthorized(t *testing.T) {
	h := makeHandler()
	w := getModels(h, "bad-key")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestModels_OK(t *testing.T) {
	h := makeHandler()
	w := getModels(h, goodKey)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Object string        `json:"object"`
		Data   []modelObject `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("expected object=list, got %q", resp.Object)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o" {
		t.Errorf("unexpected models: %+v", resp.Data)
	}
}

func TestModels_Restricted(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	a := auth.New([]config.APIKey{
		{Key: restrictedKey, App: "restricted-app", AllowedModels: []string{"gpt-4o"}},
	})
	r := router.New(map[string]config.ModelAlias{
		"gpt-4o":       {Provider: "openai", Model: "gpt-4o"},
		"claude-sonnet": {Provider: "anthropic", Model: "claude-sonnet-4-5"},
	}, nil)
	h := New(a, r, Providers{}, logger)

	w := getModels(h, restrictedKey)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data []modelObject `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4o" {
		t.Errorf("expected only gpt-4o, got %+v", resp.Data)
	}
}

func TestNotFound(t *testing.T) {
	h := makeHandler()
	req := httptest.NewRequest(http.MethodGet, "/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	h.NotFound(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestModels_LogsUnauthorized(t *testing.T) {
	h := makeHandler()
	w := getModels(h, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestChatCompletions_Stream(t *testing.T) {
	h := makeHandler()
	w := post(h, map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
		"stream":   true,
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected SSE content type, got %s", w.Header().Get("Content-Type"))
	}
}
