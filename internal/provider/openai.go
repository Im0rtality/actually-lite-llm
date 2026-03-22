package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type OpenAIProvider struct {
	client *openaisdk.Client
}

func NewOpenAI(apiKey, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: 120 * time.Second}),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	c := openaisdk.NewClient(opts...)
	return &OpenAIProvider{client: &c}
}

func toOpenAIMessages(msgs []ChatMessage) []openaisdk.ChatCompletionMessageParamUnion {
	result := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			result = append(result, openaisdk.SystemMessage(m.Content))
		case "assistant":
			result = append(result, openaisdk.AssistantMessage(m.Content))
		default:
			result = append(result, openaisdk.UserMessage(m.Content))
		}
	}
	return result
}

func buildOpenAIParams(req *ChatCompletionRequest, upstreamModel string) openaisdk.ChatCompletionNewParams {
	params := openaisdk.ChatCompletionNewParams{
		Model:    openaisdk.ChatModel(upstreamModel),
		Messages: toOpenAIMessages(req.Messages),
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = openaisdk.Int(int64(req.MaxTokens))
	}
	if req.Temperature != nil {
		params.Temperature = openaisdk.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = openaisdk.Float(*req.TopP)
	}
	if len(req.Stop) > 0 {
		params.Stop = openaisdk.ChatCompletionNewParamsStopUnion{
			OfStringArray: req.Stop,
		}
	}
	return params
}

func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter) (*Usage, error) {
	completion, err := p.client.Chat.Completions.New(ctx, buildOpenAIParams(req, upstreamModel))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, `{"error":{"message":%q,"type":"upstream_error"}}`, err.Error())
		return nil, fmt.Errorf("openai: %w", err)
	}

	resp := ChatCompletionResponse{
		ID:      completion.ID,
		Object:  string(completion.Object),
		Created: completion.Created,
		Model:   req.Model,
		Usage: Usage{
			PromptTokens:     int(completion.Usage.PromptTokens),
			CompletionTokens: int(completion.Usage.CompletionTokens),
			TotalTokens:      int(completion.Usage.TotalTokens),
		},
	}
	for _, c := range completion.Choices {
		resp.Choices = append(resp.Choices, ChatCompletionChoice{
			Index:        int(c.Index),
			Message:      ChatMessage{Role: string(c.Message.Role), Content: c.Message.Content},
			FinishReason: string(c.FinishReason),
		})
	}

	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
	return &resp.Usage, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req *ChatCompletionRequest, upstreamModel string, w http.ResponseWriter, onFirstByte func()) error {
	stream := p.client.Chat.Completions.NewStreaming(ctx, buildOpenAIParams(req, upstreamModel))
	defer func() { _ = stream.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	firstByte := false

	for stream.Next() {
		chunk := stream.Current()
		if !firstByte {
			firstByte = true
			onFirstByte()
		}
		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("openai stream: %w", err)
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
