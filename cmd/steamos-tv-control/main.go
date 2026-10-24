package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tipfurion/steamos-tv-control/internal/app"
	"github.com/Tipfurion/steamos-tv-control/internal/logging"
)

func main() {
	logging.Configure()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Run(ctx, os.Args[1:]); err != nil {
		slog.Error("Command failed", "error", err)
		os.Exit(1)
	}
}
