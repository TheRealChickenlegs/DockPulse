// Command dockpulse is the single DockPulse binary.
//
// It runs in one of two modes:
//
//	dockpulse --mode=controller            # default; web UI + agent API
//	dockpulse --mode=agent --controller=…  # runs on each Docker host
//
// All configuration is via CLI flags. Sensitive values (enrollment
// tokens, registry credentials) are loaded from files, never from
// flags or environment.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/agent"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/logging"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/version"
)

func main() {
	logging.Setup()
	if err := run(os.Args[1:]); err != nil {
		slog.Error("dockpulse exited with error", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	slog.Info("dockpulse starting", "version", version.String())

	cfg, err := config.Load(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		fmt.Fprintln(os.Stderr, "use --help to see available flags")
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch c := cfg.(type) {
	case config.Controller:
		slog.Info("loaded controller config", "cfg", c.String())
		sqlDB, err := db.Open(ctx, c.DBPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = sqlDB.Close() }()
		srv, err := controller.New(c, sqlDB)
		if err != nil {
			return err
		}
		if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case config.Agent:
		slog.Info("loaded agent config", "cfg", c.String())
		if err := agent.Run(ctx, c); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown config type %T", cfg)
	}
}
