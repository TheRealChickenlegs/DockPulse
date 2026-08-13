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

func TestIsLocalHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.53", true},
		{"::1", true},
		{"localhost", true},
		{"10.0.0.5", true},
		{"172.16.3.4", true},
		{"192.168.10.10", true},
		{"169.254.1.1", true}, // link-local
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.5", false},
		{"myhost.local", true},
		{"myhost.lan", true},
		{"myhost.example.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLocalHost(c.host); got != c.want {
			t.Errorf("isLocalHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestValidateAllowsHTTPForLocalHosts(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9787",
		"http://localhost:9787",
		"http://10.0.0.5:9787",
		"http://192.168.10.10:9787",
		"http://myhost.local:9787",
	}
	for _, u := range cases {
		cfg := config.Agent{
			Common:        config.Common{Mode: config.ModeAgent},
			Name:          "x",
			ControllerURL: u,
			DockerHost:    "unix:///var/run/docker.sock",
			DataDir:       t.TempDir(),
		}
		if err := validate(cfg); err != nil {
			t.Errorf("validate(%q): unexpected error: %v", u, err)
		}
	}
}

func TestValidateRejectsHTTPForPublicHosts(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "http://dockpulse.example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for http public host without --allow-insecure-controller")
	}
}

func TestValidateAllowsInsecureControllerOverride(t *testing.T) {
	cfg := config.Agent{
		Common:                 config.Common{Mode: config.ModeAgent},
		Name:                   "x",
		ControllerURL:          "http://dockpulse.example.com",
		AllowInsecureController: true,
		DockerHost:             "unix:///var/run/docker.sock",
		DataDir:                t.TempDir(),
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("with --allow-insecure-controller, expected no error: %v", err)
	}
}

func TestRunCreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-data")
	// Pre-place an enroll token file so the daemon's first-start
	// check passes its IO sanity step; the network call is
	// short-circuited by the short context deadline.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tok := filepath.Join(dir, "token")
	if err := os.WriteFile(tok, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := config.Agent{
		Common:          config.Common{Mode: config.ModeAgent},
		Name:            "x",
		ControllerURL:   "https://127.0.0.1:1",
		DockerHost:      "tcp://127.0.0.1:1",
		DataDir:         dir,
		EnrollTokenFile: tok,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = Run(ctx, cfg) // expected to fail on enroll; we only check the data dir was created
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected data dir to exist: %v", err)
	}
}