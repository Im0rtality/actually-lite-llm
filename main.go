package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/laurynas/actually-lite-llm/internal/auth"
	"github.com/laurynas/actually-lite-llm/internal/config"
	"github.com/laurynas/actually-lite-llm/internal/handler"
	"github.com/laurynas/actually-lite-llm/internal/provider"
	"github.com/laurynas/actually-lite-llm/internal/router"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	authenticator := auth.New(cfg.APIKeys)
	rtr := router.New(cfg.Models, cfg.Routing)

	providers := make(handler.Providers)
	if p, ok := cfg.Providers["openai"]; ok {
		providers["openai"] = provider.NewOpenAI(p.APIKey, p.BaseURL)
	}
	if p, ok := cfg.Providers["anthropic"]; ok {
		providers["anthropic"] = provider.NewAnthropic(p.APIKey, p.BaseURL)
	}

	h := handler.New(authenticator, rtr, providers, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", h.ChatCompletions)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		logger.Info("starting server", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("stopped")
}
