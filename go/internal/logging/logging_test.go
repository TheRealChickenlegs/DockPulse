package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupTextDefault(t *testing.T) {
	t.Setenv("DOCKPULSE_LOG_FORMAT", "")
	t.Setenv("DOCKPULSE_LOG_LEVEL", "")
	Setup()
	if slog.Default() == nil {
		t.Fatal("default logger should be set")
	}
}

func TestSetupJSONLevel(t *testing.T) {
	t.Setenv("DOCKPULSE_LOG_FORMAT", "json")
	t.Setenv("DOCKPULSE_LOG_LEVEL", "debug")
	Setup()
	if slog.Default() == nil {
		t.Fatal("default logger should be set")
	}
}

func TestParseLevelInvalid(t *testing.T) {
	if got := parseLevel("nonsense"); got != slog.LevelInfo {
		t.Fatalf("invalid level should fall back to info, got %v", got)
	}
	if got := parseLevel("warn"); got != slog.LevelWarn {
		t.Fatalf("warn should map to LevelWarn, got %v", got)
	}
	if got := parseLevel("ERROR"); got != slog.LevelError {
		t.Fatalf("ERROR (uppercase) should map to LevelError, got %v", got)
	}
}

// Ensure Setup does not panic when stderr is closed (defensive test).
func TestSetupIdempotent(t *testing.T) {
	t.Setenv("DOCKPULSE_LOG_LEVEL", "info")
	Setup()
	Setup() // second call must not panic
}

func BenchmarkLogger(b *testing.B) {
	b.Setenv("DOCKPULSE_LOG_FORMAT", "json")
	b.Setenv("DOCKPULSE_LOG_LEVEL", "info")
	Setup()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("bench", "i", i)
	}
	if !strings.Contains(buf.String(), "bench") {
		b.Fatalf("expected 'bench' in output, got %q", buf.String())
	}
}