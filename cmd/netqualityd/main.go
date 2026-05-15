package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ebastos/netquality/internal/api"
	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/eval"
	"github.com/ebastos/netquality/internal/probe"
	"github.com/ebastos/netquality/internal/scheduler"
	"github.com/ebastos/netquality/internal/store"
)

func main() {
	configPath := flag.String("config", envOr("NETQUALITY_CONFIG", "/etc/netquality/config.yaml"), "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.DataDir, 0750); err != nil {
		slog.Error("mkdir data", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.DBPath())
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	runner, err := probe.NewRunner(cfg)
	if err != nil {
		slog.Error("probe runner", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sched := scheduler.New(cfg, db, runner)
	go func() {
		if err := sched.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("scheduler", "err", err)
			cancel()
		}
	}()

	engine := eval.NewEngine(cfg, db)
	srv := api.New(cfg, db, engine)
	httpSrv := &http.Server{
		Addr:    cfg.Listen,
		Handler: srv.Handler(),
	}

	go func() {
		slog.Info("listening", "addr", cfg.Listen, "device", cfg.DeviceID)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.DNS.Timeout.Std())
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
