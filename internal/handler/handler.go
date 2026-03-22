package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/im0rtality/actually-lite-llm/internal/auth"
	"github.com/im0rtality/actually-lite-llm/internal/metrics"
	"github.com/im0rtality/actually-lite-llm/internal/provider"
	"github.com/im0rtality/actually-lite-llm/internal/router"
)

type Providers map[string]provider.Provider

type Handler struct {
	auth      *auth.Authenticator
	router    *router.Router
	providers Providers
	logger    *slog.Logger
}

func New(a *auth.Authenticator, r *router.Router, p Providers, logger *slog.Logger) *Handler {
	return &Handler{auth: a, router: r, providers: p, logger: logger}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

type modelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	token := auth.ExtractBearer(r.Header.Get("Authorization"))
	keyInfo := h.auth.Lookup(token)
	if keyInfo == nil {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return
	}

	all := h.router.Aliases()
	sort.Strings(all)
	data := make([]modelObject, 0, len(all))
	for _, name := range all {
		if keyInfo.AllowsModel(name) {
			data = append(data, modelObject{
				ID:      name,
				Object:  "model",
				Created: 0,
				OwnedBy: "actually-lite-llm",
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := uuid.New().String()

	// Auth
	token := auth.ExtractBearer(r.Header.Get("Authorization"))
	keyInfo := h.auth.Lookup(token)
	if keyInfo == nil {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		h.logAccess(r, http.StatusUnauthorized, start, reqID, "", "", "", false)
		return
	}

	// Parse request
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB
	var req provider.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		h.logAccess(r, http.StatusBadRequest, start, reqID, keyInfo.App, "", "", false)
		return
	}

	// Authorization check
	if !keyInfo.AllowsModel(req.Model) {
		writeError(w, http.StatusForbidden, "model not allowed for this key")
		h.logAccess(r, http.StatusForbidden, start, reqID, keyInfo.App, "", req.Model, false)
		return
	}

	// Routing
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

	vk := keyInfo.App
	pname := route.Provider
	model := req.Model

	statusCode := http.StatusOK

	if req.Stream {
		onFirstByte := func() {
			metrics.StreamFirstByte.WithLabelValues(vk, pname, model).Observe(time.Since(start).Seconds())
		}

		err = prov.ChatStream(r.Context(), &req, route.UpstreamModel, w, onFirstByte)
		if err != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "stream_error").Inc()
			h.logger.Error("stream error", "error", err, "request_id", reqID)
		}

		metrics.RequestDuration.WithLabelValues(vk, pname, model, "true").Observe(time.Since(start).Seconds())
		if err != nil {
			statusCode = http.StatusBadGateway
		}
		metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, vk, pname, model, true)
	} else {
		usage, err := prov.Chat(r.Context(), &req, route.UpstreamModel, w)
		if err != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "request_error").Inc()
			h.logger.Error("chat error", "error", err, "request_id", reqID)
			statusCode = http.StatusBadGateway
		}
		if usage != nil {
			metrics.TokensTotal.WithLabelValues(vk, pname, model, "prompt").Add(float64(usage.PromptTokens))
			metrics.TokensTotal.WithLabelValues(vk, pname, model, "completion").Add(float64(usage.CompletionTokens))
			if route.CostPerMillionInput > 0 || route.CostPerMillionOutput > 0 {
				cost := float64(usage.PromptTokens)*route.CostPerMillionInput/1_000_000 +
					float64(usage.CompletionTokens)*route.CostPerMillionOutput/1_000_000
				metrics.CostTotal.WithLabelValues(vk, pname, model).Add(cost)
			}
		}
		metrics.RequestDuration.WithLabelValues(vk, pname, model, "false").Observe(time.Since(start).Seconds())
		metrics.RequestsTotal.WithLabelValues(vk, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, vk, pname, model, false)
	}
}

func (h *Handler) logAccess(r *http.Request, status int, start time.Time, reqID, virtualKey, prov, model string, stream bool) {
	h.logger.Info("access",
		"request_id", reqID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", time.Since(start).Milliseconds(),
		"virtual_key", virtualKey,
		"provider", prov,
		"model", model,
		"stream", stream,
	)
}
