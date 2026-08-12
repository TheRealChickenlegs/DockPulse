package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
)

func TestValidateRequiresName(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when name is empty")
	}
}

func TestValidateRequiresHTTPS(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "http://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when controller URL is http://")
	}
}

func TestValidateRefusesProcDataDir(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       "/proc/self",
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when data dir is under /proc")
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-data")
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "https://example.invalid",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       dir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected data dir to exist: %v", err)
	}
}