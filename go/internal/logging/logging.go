// Package logging configures the process-wide structured logger.
//
// DockPulse uses log/slog exclusively. Subsystems should construct a child
// logger with slog.Default().With("subsystem", "controller") so the
// subsystem label travels with every record.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup installs a slog handler on the default logger.
//
// The level is read from the DOCKPULSE_LOG_LEVEL environment variable
// (debug, info, warn, error). Invalid values fall back to info.
// Setting DOCKPULSE_LOG_FORMAT=json switches from the default text
// handler to a JSON handler suitable for container log aggregation.
func Setup() {
	level := parseLevel(os.Getenv("DOCKPULSE_LOG_LEVEL"))
	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level <= parseLevelLevel("debug"),
	}

	var h slog.Handler
	if strings.EqualFold(os.Getenv("DOCKPULSE_LOG_FORMAT"), "json") {
		h = slog.NewJSONHandler(os.Stderr, handlerOpts)
	} else {
		h = slog.NewTextHandler(os.Stderr, handlerOpts)
	}

	logger := slog.New(h)
	slog.SetDefault(logger)
}

func parseLevel(s string) slog.Level {
	return parseLevelLevel(s)
}

func parseLevelLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}