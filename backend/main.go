package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := LoadConfig()
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	srv, err := NewServer(cfg, log)
	if err != nil {
		log.Error("could not create server", "error", err)
		os.Exit(1)
	}

	defer func() { _ = srv.Close() }()

	if err := srv.SeedDemoUser(); err != nil {
		log.Error("could not seed the demo account", "error", err)
		os.Exit(1)
	}

	log.Info("starting",
		"addr", cfg.Addr,
		"db", cfg.DBPath,
		"rpId", cfg.RPID,
		"origins", cfg.RPOrigins,
		"androidPackage", cfg.AndroidPackageName,
		"restoreRequiresUserPresence", cfg.RestoreRequireUserPresence,
	)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				srv.store.GC()
			}
		}
	}()

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
