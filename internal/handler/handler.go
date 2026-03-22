package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/laurynas/actually-lite-llm/internal/auth"
	"github.com/laurynas/actually-lite-llm/internal/metrics"
	"github.com/laurynas/actually-lite-llm/internal/provider"
	"github.com/laurynas/actually-lite-llm/internal/router"
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

	app := keyInfo.App
	pname := route.Provider
	model := req.Model

	statusCode := http.StatusOK

	if req.Stream {
		firstByteTime := time.Time{}
		onFirstByte := func() {
			firstByteTime = time.Now()
			metrics.StreamFirstByte.WithLabelValues(app, pname, model).Observe(time.Since(start).Seconds())
		}

		err = prov.ChatStream(r.Context(), &req, route.UpstreamModel, w, onFirstByte)
		if err != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "stream_error").Inc()
			h.logger.Error("stream error", "error", err, "request_id", reqID)
		}
		_ = firstByteTime

		streamStr := "true"
		metrics.RequestDuration.WithLabelValues(app, pname, model, streamStr).Observe(time.Since(start).Seconds())
		if err != nil {
			statusCode = http.StatusBadGateway
		}
		metrics.RequestsTotal.WithLabelValues(app, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, app, pname, model, true)
	} else {
		usage, err := prov.Chat(r.Context(), &req, route.UpstreamModel, w)
		if err != nil {
			metrics.ProviderErrors.WithLabelValues(pname, "request_error").Inc()
			h.logger.Error("chat error", "error", err, "request_id", reqID)
			statusCode = http.StatusBadGateway
		}
		if usage != nil {
			metrics.TokensTotal.WithLabelValues(app, pname, model, "prompt").Add(float64(usage.PromptTokens))
			metrics.TokensTotal.WithLabelValues(app, pname, model, "completion").Add(float64(usage.CompletionTokens))
		}
		metrics.RequestDuration.WithLabelValues(app, pname, model, "false").Observe(time.Since(start).Seconds())
		metrics.RequestsTotal.WithLabelValues(app, pname, model, strconv.Itoa(statusCode)).Inc()
		h.logAccess(r, statusCode, start, reqID, app, pname, model, false)
	}
}

func (h *Handler) logAccess(r *http.Request, status int, start time.Time, reqID, app, prov, model string, stream bool) {
	h.logger.Info("access",
		"request_id", reqID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", time.Since(start).Milliseconds(),
		"app", app,
		"provider", prov,
		"model", model,
		"stream", stream,
	)
}
