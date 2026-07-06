package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goauthentik.io/cherry-pick-svc/internal/cherrypick"
	"goauthentik.io/cherry-pick-svc/internal/config"
	"goauthentik.io/cherry-pick-svc/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg.LogLevel)

	ctx := context.Background()
	if err := cherrypick.ResolveAppGitIdentity(ctx, cfg); err != nil {
		logger.Error("failed to resolve app git identity", "err", err)
		os.Exit(1)
	}
	logger.Info("git identity resolved", "name", cfg.GitUserName, "email", cfg.GitUserEmail)

	srv := webhook.NewServer(cfg, logger)

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", srv)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
		// Webhook handler returns 200 immediately; cherry-picks run in background goroutines.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down, draining in-flight cherry-picks")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "err", err)
	}

	srv.WaitAll()
	logger.Info("done")
}

func buildLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
