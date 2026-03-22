package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultMaxTokens = 4096

type AnthropicProvider struct {
	client anthropicsdk.Client
}

func NewAnthropic(apiKey, baseURL string) *AnthropicProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: 120 * time.Second}),
	}
	if baseURL != "" {
		// The Anthropic SDK appends /v1/... paths itself; strip a trailing /v1
		// from the config base URL so we don't end up with /v1/v1/messages.
		opts = append(opts, option.WithBaseURL(strings.TrimSuffix(baseURL, "/v1")))
	}
	return &AnthropicProvider{client: anthropicsdk.NewClient(opts...)}
}

func buildAnthropicParams(req *ChatCompletionRequest, upstreamModel string) anthropicsdk.MessageNewParams {
	var system []anthropicsdk.TextBlockParam
	var messages []anthropicsdk.MessageParam

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			system = append(system, anthropicsdk.TextBlockParam{Text: m.Content})
		case "assistant":
			messages = append(messages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(m.Content)))
		default:
			messages = append(messages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(m.Content)))
		}
	}

	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropicsdk.MessageNewParams{
		Model:     upstreamModel,
		Messages:  messages,
		MaxTokens: maxTokens,
		System:    system,
	}
	if req.Temperature != nil {
		params.Temperature = anthropicsdk.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = anthropicsdk.Float(*req.TopP)
	}
	if len(req.Stop) > 0 {
		params.StopSequences = req.Stop
	}
	return params
}

func anthropicToOpenAIUsage(u anthropicsdk.Usage) Usage {
	return Usage{
		PromptTokens:     int(u.InputTokens),
		CompletionTokens: int(u.OutputTokens),
		TotalTokens:      int(u.InputTokens + u.OutputTokens),
	}
}

func anthropicStopReason(r anthropicsdk.StopReason) string {
	switch r {
	case anthropicsdk.StopReasonEndTurn:
		return "stop"
	case anthropicsdk.StopReasonMaxTokens:
		return "length"
	case anthropicsdk.StopReasonStopSequence:
		return "stop"
	default:
		return string(r)
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter) (*Usage, error) {
	msg, err := p.client.Messages.New(ctx, buildAnthropicParams(req, upstreamModel))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, `{"error":{"message":%q,"type":"upstream_error"}}`, err.Error())
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	var content string
	for _, block := range msg.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	usage := anthropicToOpenAIUsage(msg.Usage)
	resp := ChatCompletionResponse{
		ID:      msg.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{{
			Index:        0,
			Message:      ChatMessage{Role: "assistant", Content: content},
			FinishReason: anthropicStopReason(msg.StopReason),
		}},
		Usage: usage,
	}

	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	return &usage, nil
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter, onFirstByte func()) error {
	stream := p.client.Messages.NewStreaming(ctx, buildAnthropicParams(req, upstreamModel))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	firstByte := false
	streamID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				if !firstByte {
					firstByte = true
					onFirstByte()
				}
				chunk := map[string]any{
					"id":      streamID,
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   req.Model,
					"choices": []map[string]any{{
						"index":         0,
						"delta":         map[string]string{"content": event.Delta.Text},
						"finish_reason": nil,
					}},
				}
				data, _ := json.Marshal(chunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				if flusher != nil {
					flusher.Flush()
				}
			}
		case "message_delta":
			chunk := map[string]any{
				"id":      streamID,
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   req.Model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": anthropicStopReason(event.Delta.StopReason),
				}},
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("anthropic stream: %w", err)
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
