package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/drkliu/jimu-studio/internal/localprovider"
)

func main() {
	dsn := os.Getenv("JIMU_STUDIO_POSTGRES_DSN")
	if dsn == "" {
		slog.Error("JIMU_STUDIO_POSTGRES_DSN is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	handler, err := localprovider.NewHandler(ctx, localprovider.Config{DSN: dsn})
	if err != nil {
		slog.Error("initialize PostgreSQL-backed local provider", "error", err)
		os.Exit(1)
	}
	defer func() { _ = handler.Close() }()
	server := &http.Server{
		Addr:              "127.0.0.1:8081",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("starting local Jimu Provider", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("local Jimu Provider stopped", "error", err)
		os.Exit(1)
	}
}
