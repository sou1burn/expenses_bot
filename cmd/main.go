package main

import (
	"context"
	"errors"
	"expences_bot/agent"
	"expences_bot/internal/bot"
	"expences_bot/internal/config"
	"expences_bot/internal/handler"
	"expences_bot/internal/repository"
	"expences_bot/web"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.LoadConfig(".env")

	if len(os.Args) > 1 && os.Args[1] == "init-db" {
		if err := repository.EnsureAppRole(ctx, *cfg); err != nil {
			slog.Error("init-db", "err", err)
			os.Exit(1)
		}
		return
	}

	db, err := repository.NewPostgresDB(*cfg)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	repo := repository.NewPostgresRepo(db)

	mux := http.NewServeMux()
	handler.NewHandler(repo).Register(mux)
	mux.Handle("GET /", web.Handler())
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.HttpHost, cfg.HttpPort),
		Handler:           http.NewCrossOriginProtection().Handler(mux), // CSRF: чужие сайты не могут менять траты
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("admin panel", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "err", err)
			stop()
		}
	}()

	if cfg.BotToken == "" {
		slog.Warn("BOT_TOKEN is empty, telegram bot disabled")
	} else {
		ag := agent.New(agent.Config{BaseURL: cfg.LlmBaseUrl, APIKey: cfg.LlmApiKey, Model: cfg.LlmModel})
		if err := ag.ResolveModels(ctx); err != nil {
			slog.Error("llm models, telegram bot disabled", "err", err)
		} else {
			go bot.New(cfg.BotToken, repo, ag).Run(ctx)
			slog.Info("telegram bot started")
		}
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
