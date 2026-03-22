package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postResponses(h *Handler, body interface{}, authKey string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	w := httptest.NewRecorder()
	h.Responses(w, req)
	return w
}

func TestResponses_Unauthorized(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "gpt-4o",
		"input": "hello",
	}, "bad-key")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestResponses_Forbidden(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "claude-sonnet",
		"input": "hello",
	}, restrictedKey)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestResponses_UnknownModel(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "unknown-model",
		"input": "hello",
	}, goodKey)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestResponses_StringInput(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "gpt-4o",
		"input": "hello",
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp responsesAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Object != "response" {
		t.Errorf("expected object=response, got %q", resp.Object)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status=completed, got %q", resp.Status)
	}
	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 1 {
		t.Fatalf("unexpected output: %+v", resp.Output)
	}
	if resp.Output[0].Content[0].Type != "output_text" {
		t.Errorf("expected output_text, got %q", resp.Output[0].Content[0].Type)
	}
}

func TestResponses_MessagesInput(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "gpt-4o",
		"input": []map[string]string{{"role": "user", "content": "hello"}},
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp responsesAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected completed, got %q", resp.Status)
	}
}

func TestResponses_ContentPartsInput(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model": "gpt-4o",
		"input": []map[string]interface{}{
			{"role": "user", "content": []map[string]string{{"type": "text", "text": "hello"}}},
		},
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp responsesAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected completed, got %q", resp.Status)
	}
}

func TestResponses_Instructions(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model":        "gpt-4o",
		"input":        "hello",
		"instructions": "Be concise.",
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestResponses_Stream(t *testing.T) {
	h := makeHandler()
	w := postResponses(h, map[string]interface{}{
		"model":  "gpt-4o",
		"input":  "hello",
		"stream": true,
	}, goodKey)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: response.created") {
		t.Error("missing response.created event")
	}
	if !strings.Contains(body, "event: response.completed") {
		t.Error("missing response.completed event")
	}
}
