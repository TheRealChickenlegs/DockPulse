// Package agent is the DockPulse agent mode. The agent runs on each
// Docker host, talks to the controller over mTLS, and is responsible
// for local Docker inventory and registry polling.
//
// In Phase 0 the agent only validates configuration, logs a startup
// banner, and runs a placeholder loop until cancelled. Phase 1 wires
// the enrollment handshake and Docker SDK calls.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/version"
)

// Run starts the agent and blocks until ctx is cancelled or a fatal
// error occurs. It is the single entry point invoked by main.go.
func Run(ctx context.Context, cfg config.Agent) error {
	logger := slog.Default().With("subsystem", "agent", "name", cfg.Name)
	logger.Info("agent starting",
		"controller", cfg.ControllerURL,
		"docker", cfg.DockerHost,
		"data_dir", cfg.DataDir,
		"version", version.Version,
	)

	if err := validate(cfg); err != nil {
		return fmt.Errorf("invalid agent config: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Phase 0 placeholder: the agent confirms its configuration and
	// then idles until cancelled. Real work starts in Phase 1.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("agent stopping", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			logger.Debug("agent heartbeat (phase 0 placeholder)")
		}
	}
}

// validate performs early sanity checks on the agent configuration.
// These checks happen before any I/O so misconfiguration fails fast.
func validate(cfg config.Agent) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("--name is required in agent mode")
	}
	if _, err := url.Parse(cfg.ControllerURL); err != nil {
		return fmt.Errorf("invalid --controller URL: %w", err)
	}
	if !strings.HasPrefix(cfg.ControllerURL, "https://") {
		return errors.New("--controller must use https:// in production")
	}
	if cfg.DockerHost == "" {
		return errors.New("--docker must not be empty")
	}
	if cfg.EnrollTokenFile != "" {
		if _, err := os.Stat(cfg.EnrollTokenFile); err != nil {
			return fmt.Errorf("enroll token file: %w", err)
		}
	}
	if cfg.DataDir == "" {
		return errors.New("--data must not be empty")
	}
	// Defensive: never accept the agent's data directory under /proc
	// or /sys; these are read-only kernel surfaces.
	abs, err := filepath.Abs(cfg.DataDir)
	if err == nil && (strings.HasPrefix(abs, "/proc") || strings.HasPrefix(abs, "/sys")) {
		return fmt.Errorf("refusing to use %s as data dir", abs)
	}
	return nil
}