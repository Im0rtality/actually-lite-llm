package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/im0rtality/actually-lite-llm/internal/auth"
	"github.com/im0rtality/actually-lite-llm/internal/metrics"
	"github.com/im0rtality/actually-lite-llm/internal/provider"
)

type responsesRequest struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    string          `json:"instructions,omitempty"`
	Stream          bool            `json:"stream"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
}

type responsesOutputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesOutputItem struct {
	Type    string                `json:"type"`
	ID      string                `json:"id"`
	Status  string                `json:"status"`
	Role    string                `json:"role"`
	Content []responsesOutputText `json:"content"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesAPIResponse struct {
	ID        string                `json:"id"`
	Object    string                `json:"object"`
	CreatedAt int64                 `json:"created_at"`
	Model     string                `json:"model"`
	Status    string                `json:"status"`
	Output    []responsesOutputItem `json:"output"`
	Usage     responsesUsage        `json:"usage"`
}

func parseResponsesInput(raw json.RawMessage) ([]provider.ChatMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("input is required")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []provider.ChatMessage{{Role: "user", Content: text}}, nil
	}
	var msgs []provider.ChatMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func emitResponsesEvent(w http.ResponseWriter, eventType string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
}

// responsesStreamAdapter wraps an http.ResponseWriter to translate
// chat completion SSE chunks into Responses API SSE events.
type responsesStreamAdapter struct {
	w             http.ResponseWriter
	f             http.Flusher
	itemID        string
	dummy         http.Header
	buf           []byte
	collectedText string
}

func (a *responsesStreamAdapter) Header() http.Header { return a.dummy }
func (a *responsesStreamAdapter) WriteHeader(int)     {}

func (a *responsesStreamAdapter) Write(p []byte) (int, error) {
	a.buf = append(a.buf, p...)
	for {
		idx := bytes.Index(a.buf, []byte("\n\n"))
		if idx < 0 {
			break
		}
		line := bytes.TrimSpace(a.buf[:idx])
		a.buf = a.buf[idx+2:]

		line = bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(line, []byte("[DONE]")) {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		a.collectedText += delta
		emitResponsesEvent(a.w, "response.output_text.delta", map[string]interface{}{
			"type":          "response.output_text.delta",
			"item_id":       a.itemID,
			"output_index":  0,
			"content_index": 0,
			"delta":         delta,
		})
		if a.f != nil {
			a.f.Flush()
		}
	}
	return len(p), nil
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := uuid.New().String()

	token := auth.ExtractBearer(r.Header.Get("Authorization"))
	keyInfo := h.auth.Lookup(token)
	if keyInfo == nil {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		h.logAccess(r, http.StatusUnauthorized, start, reqID, "", "", "", false)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	var req responsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		h.logAccess(r, http.StatusBadRequest, start, reqID, keyInfo.App, "", "", false)
		return
	}

	if !keyInfo.AllowsModel(req.Model) {
		writeError(w, http.StatusForbidden, "model not allowed for this key")
		h.logAccess(r, http.StatusForbidden, start, reqID, keyInfo.App, "", req.Model, false)
		return
	}

	route, err := h.router.Resolve(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown model: "+req.Model)
		h.logAccess(r, http.StatusBadRequest, start, reqID, keyInfo.App, "", req.Model, false)
		return
	}

	prov, ok := h.providers[route.Provider]
	if !ok {
		writeError(w, http.StatusInternalServerError, "provider not configured: "+route.Provider)
		h.logAccess(r, http.StatusInternalServerError, start, reqID, keyInfo.App, route.Provider, req.Model, false)
		return
	}

	messages, err := parseResponsesInput(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid input: "+err.Error())
		h.logAccess(r, http.StatusBadRequest, start, reqID, keyInfo.App, "", req.Model, false)
		return
	}
	if req.Instructions != "" {
		messages = append([]provider.ChatMessage{{Role: "system", Content: req.Instructions}}, messages...)
	}

	chatReq := &provider.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxOutputTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}

	vk := keyInfo.App
	pname := route.Provider
	model := req.Model
	statusCode := http.StatusOK

	if req.Stream {
		respID := "resp_" + reqID
		itemID := "msg_" + uuid.New().String()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		f, _ := w.(http.Flusher)

		emitResponsesEvent(w, "response.created", map[string]interface{}{
			"type": "response.created",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "status": "in_progress", "model": model,
			},
		})
		emitResponsesEvent(w, "response.output_item.added", map[string]interface{}{
			"type": "response.output_item.added", "output_index": 0,
			"item": map[string]interface{}{
				"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []interface{}{},
			},
		})
		emitResponsesEvent(w, "response.content_part.added", map[string]interface{}{
			"type": "response.content_part.added", "item_id": itemID, "output_index": 0, "content_index": 0,
			"part": map[string]interface{}{"type": "output_text", "text": ""},
		})
		if f != nil {
			f.Flush()
		}

		adapter := &responsesStreamAdapter{w: w, f: f, itemID: itemID, dummy: make(http.Header)}
		onFirstByte := func() {
			metrics.StreamFirstByte.WithLabelValues(vk, pname, model).Observe(time.Since(start).Seconds())
		}
		chatReq.Stream = true
		streamErr := prov.ChatStream(r.Context(), chatReq, route.UpstreamModel, adapter, onFirstByte)

		text := adapter.collectedText
		emitResponsesEvent(w, "response.output_text.done", map[string]interface{}{
			"type": "response.output_text.done", "item_id": itemID, "output_index": 0, "content_index": 0, "text": text,
		})
		emitResponsesEvent(w, "response.output_item.done", map[string]interface{}{
			"type": "response.output_item.done", "output_index": 0,
			"item": map[string]interface{}{
				"id": itemID, "type": "message", "status": "completed", "role": "assistant",
				"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text}},
			},
		})
		emitResponsesEvent(w, "response.completed", map[string]interface{}{
			"type": "response.completed",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "status": "completed", "model": model,
				"output": []interface{}{map[string]interface{}{
					"id": itemID, "type": "message", "status": "completed", "role": "assistant",
					"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text}},
				}},
			},
		})
		if f != nil {
			f.Flush()
		}

		if streamErr != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "stream_error").Inc()
			h.logger.Error("stream error", "error", streamErr, "request_id", reqID)
			statusCode = http.StatusBadGateway
		}
		metrics.RequestDuration.WithLabelValues(vk, pname, model, "true").Observe(time.Since(start).Seconds())
		metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, vk, pname, model, true)
	} else {
		rec := httptest.NewRecorder()
		usage, err := prov.Chat(r.Context(), chatReq, route.UpstreamModel, rec)
		if err != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "request_error").Inc()
			h.logger.Error("chat error", "error", err, "request_id", reqID)
			writeError(w, http.StatusBadGateway, "upstream error")
			statusCode = http.StatusBadGateway
			metrics.RequestDuration.WithLabelValues(vk, pname, model, "false").Observe(time.Since(start).Seconds())
			metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
			h.logAccess(r, statusCode, start, reqID, vk, pname, model, false)
			return
		}

		var chatResp provider.ChatCompletionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &chatResp); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse upstream response")
			statusCode = http.StatusInternalServerError
			metrics.RequestDuration.WithLabelValues(vk, pname, model, "false").Observe(time.Since(start).Seconds())
			metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
			h.logAccess(r, statusCode, start, reqID, vk, pname, model, false)
			return
		}

		respID := "resp_" + reqID
		itemID := "msg_" + uuid.New().String()

		text := ""
		if len(chatResp.Choices) > 0 {
			text = chatResp.Choices[0].Message.Content
		}

		resp := responsesAPIResponse{
			ID:        respID,
			Object:    "response",
			CreatedAt: time.Now().Unix(),
			Model:     model,
			Status:    "completed",
			Output: []responsesOutputItem{{
				Type:    "message",
				ID:      itemID,
				Status:  "completed",
				Role:    "assistant",
				Content: []responsesOutputText{{Type: "output_text", Text: text}},
			}},
		}
		if usage != nil {
			resp.Usage = responsesUsage{
				InputTokens:  usage.PromptTokens,
				OutputTokens: usage.CompletionTokens,
				TotalTokens:  usage.TotalTokens,
			}
			metrics.TokensTotal.WithLabelValues(vk, pname, model, "prompt").Add(float64(usage.PromptTokens))
			metrics.TokensTotal.WithLabelValues(vk, pname, model, "completion").Add(float64(usage.CompletionTokens))
			if route.CostPerMillionInput > 0 || route.CostPerMillionOutput > 0 {
				cost := float64(usage.PromptTokens)*route.CostPerMillionInput/1_000_000 +
					float64(usage.CompletionTokens)*route.CostPerMillionOutput/1_000_000
				metrics.CostTotal.WithLabelValues(vk, pname, model).Add(cost)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)

		metrics.RequestDuration.WithLabelValues(vk, pname, model, "false").Observe(time.Since(start).Seconds())
		metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, vk, pname, model, false)
	}
}
