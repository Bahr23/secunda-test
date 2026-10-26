package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bahr/secunda/config"
	"github.com/bahr/secunda/internal/app"
	"github.com/bahr/secunda/migrations"
	"github.com/bahr/secunda/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "migrate" {
			return fmt.Errorf("usage: secunda [migrate]")
		}
		dsn := os.Getenv("MYSQL_DSN")
		if dsn == "" {
			return fmt.Errorf("MYSQL_DSN is required")
		}
		return migrations.Up(dsn)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, err := logger.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, cfg, log)
}
