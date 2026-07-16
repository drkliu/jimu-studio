package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/drkliu/jimu-studio/internal/config"
	"github.com/drkliu/jimu-studio/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Studio stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	address := os.Getenv("STUDIO_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	settings, err := config.Load(os.Getenv("STUDIO_CONFIG"))
	if err != nil {
		return err
	}
	if err = settings.ValidateListenAddress(address); err != nil {
		return err
	}
	broker, err := settings.Build(ctx, os.Getenv)
	if err != nil {
		return err
	}
	handler, err := server.NewAuthenticated(broker, !settings.Development)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
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
		return fmt.Errorf("serve Studio: %w", err)
	}
	return nil
}
