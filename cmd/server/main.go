package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/dispatcher"
	"webhook-event-router/internal/httpapi"
	"webhook-event-router/internal/metrics"
	"webhook-event-router/internal/store/sqlite"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir data dir: %v", err)
		}
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	st := sqlite.New(db)
	attempts := sqlite.NewAttemptStore(db)
	reg := metrics.NewRegistry()
	disp := dispatcher.New(st, attempts, cfg)

	handler := httpapi.NewServer(st, cfg, disp, reg, attempts)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				disp.Retry(ctx)
			}
		}
	}()

	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	cancel()
}
