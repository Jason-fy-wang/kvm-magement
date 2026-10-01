package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jason-fy-wang/kvm-manager/internal/manager"
)

func main() {
	listen := flag.String("listen", ":8080", "manager HTTP listen address")
	database := flag.String("db", "kvm-manager.db", "SQLite database path")
	networkCIDR := flag.String("network-cidr", "172.20.0.0/16", "shared VM bridge network CIDR")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, or error")
	flag.Parse()

	var level slog.LevelVar
	switch *logLevel {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: &level}))
	srv, err := manager.NewServer(manager.Config{ListenAddr: *listen, DatabasePath: *database, NetworkCIDR: *networkCIDR, Logger: logger})
	if err != nil {
		logger.Error("create manager", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil {
		logger.Error("manager stopped", "error", err)
		os.Exit(1)
	}
}
