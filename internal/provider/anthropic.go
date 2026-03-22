package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicVersion = "2023-06-01"
const defaultMaxTokens = 4096

type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewAnthropic(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// anthropicMessage is the format Anthropic expects.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest is the Anthropic messages API request body.
type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        anthropicUsage     `json:"usage"`
}

// translateRequest converts an OpenAI request to Anthropic format.
func translateRequest(req *ChatCompletionRequest, upstreamModel string) anthropicRequest {
	var system string
	var messages []anthropicMessage

	for _, m := range req.Messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n" + m.Content
			} else {
				system = m.Content
			}
		} else {
			messages = append(messages, anthropicMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	ar := anthropicRequest{
		Model:       upstreamModel,
		Messages:    messages,
		System:      system,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}

	if len(req.Stop) > 0 {
		ar.StopSequences = req.Stop
	}

	return ar
}

// mapStopReason converts Anthropic stop reason to OpenAI finish reason.
func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return reason
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter) (*Usage, error) {
	ar := translateRequest(req, upstreamModel)
	ar.Stream = false

	data, err := json.Marshal(ar)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	var ar2 anthropicResponse
	if err := json.Unmarshal(respBody, &ar2); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Concatenate text blocks
	var sb strings.Builder
	for _, block := range ar2.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}

	openAIResp := ChatCompletionResponse{
		ID:      ar2.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: sb.String(),
				},
				FinishReason: mapStopReason(ar2.StopReason),
			},
		},
		Usage: Usage{
			PromptTokens:     ar2.Usage.InputTokens,
			CompletionTokens: ar2.Usage.OutputTokens,
			TotalTokens:      ar2.Usage.InputTokens + ar2.Usage.OutputTokens,
		},
	}

	out, err := json.Marshal(openAIResp)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)

	return &openAIResp.Usage, nil
}

// Anthropic streaming event types
type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index"`
	Delta *anthropicDelta `json:"delta,omitempty"`
	Usage *anthropicUsage `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	StopReason string `json:"stop_reason,omitempty"`
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter, onFirstByte func()) error {
	ar := translateRequest(req, upstreamModel)
	ar.Stream = true

	data, err := json.Marshal(ar)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	firstByte := false

	// Generate a stable ID for this stream
	streamID := "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano())

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			break
		}

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		// Translate content_block_delta to OpenAI SSE chunk
		if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "text_delta" {
			chunk := map[string]interface{}{
				"id":      streamID,
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{
							"content": event.Delta.Text,
						},
						"finish_reason": nil,
					},
				},
			}
			chunkData, err := json.Marshal(chunk)
			if err != nil {
				continue
			}

			if !firstByte {
				firstByte = true
				onFirstByte()
			}

			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkData)
			if flusher != nil {
				flusher.Flush()
			}
		} else if event.Type == "message_delta" && event.Delta != nil {
			// Final message chunk with finish_reason
			chunk := map[string]interface{}{
				"id":      streamID,
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"delta":         map[string]string{},
						"finish_reason": mapStopReason(event.Delta.StopReason),
					},
				},
			}
			chunkData, err := json.Marshal(chunk)
			if err != nil {
				continue
			}

			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkData)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}
