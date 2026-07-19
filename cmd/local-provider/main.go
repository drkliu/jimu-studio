package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/drkliu/jimu-studio/internal/localprovider"
)

func main() {
	server := &http.Server{
		Addr:              "127.0.0.1:8081",
		Handler:           localprovider.NewHandler(),
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
