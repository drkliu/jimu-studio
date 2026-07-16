package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/drkliu/jimu-studio/internal/server"
)

func main() {
	address := os.Getenv("STUDIO_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8080"
	}

	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.New(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("Studio shutdown failed", "error", err)
		}
	}()

	slog.Info("starting Jimu Studio", "address", address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Studio server failed", "error", err)
		os.Exit(1)
	}
}
