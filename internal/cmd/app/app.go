// Package app is helper for simple cli apps.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

func Run(run func(ctx context.Context, lg *slog.Logger) error) {
	lg := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	if err := run(context.Background(), lg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(2)
	}
}
