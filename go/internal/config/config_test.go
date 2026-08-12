package config

import (
	"testing"
)

func TestLoadControllerDefaults(t *testing.T) {
	cfg, err := Load([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := cfg.(Controller)
	if !ok {
		t.Fatalf("expected Controller config, got %T", cfg)
	}
	if c.Mode != ModeController {
		t.Fatalf("expected ModeController, got %s", c.Mode)
	}
	if c.Listen != ":8080" {
		t.Fatalf("expected default listen :8080, got %s", c.Listen)
	}
}

func TestLoadAgentRequiresController(t *testing.T) {
	if _, err := Load([]string{"--mode=agent"}); err == nil {
		t.Fatal("expected error when --controller is missing in agent mode")
	}
}

func TestLoadAgentRejectsHTTP(t *testing.T) {
	_, err := Load([]string{"--mode=agent", "--controller=http://example.com", "--name=test", "--data=/tmp"})
	if err == nil {
		t.Fatal("expected error rejecting http:// in agent mode")
	}
}

func TestLoadAgentRejectsProcDataDir(t *testing.T) {
	_, err := Load([]string{"--mode=agent", "--controller=https://example.com", "--name=test", "--data=/proc/self"})
	if err == nil {
		t.Fatal("expected error rejecting /proc as data dir")
	}
}

func TestLoadAgentAcceptsHTTPS(t *testing.T) {
	_, err := Load([]string{"--mode=agent", "--controller=https://example.com", "--name=foo", "--data=/tmp/dockpulse"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadUnknownMode(t *testing.T) {
	_, err := Load([]string{"--mode=bogus"})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com":                 "https://example.com",
		"https://user:pass@example.com/path":  "https://<redacted>@example.com/path",
		"http://user:pass@example.com:8080/x": "http://<redacted>@example.com:8080/x",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Fatalf("redactURL(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestControllerString(t *testing.T) {
	c := Controller{Common: Common{Mode: ModeController}, Listen: ":8080", DBPath: "/tmp/x.db"}
	s := c.String()
	if !stringsContains(s, "mode=controller") {
		t.Fatalf("expected mode=controller in %q", s)
	}
	if stringsContains(s, "/tmp/x.db") {
		t.Fatalf("String() should not leak DB path: %q", s)
	}
}

func TestAgentStringRedactsCredentials(t *testing.T) {
	a := Agent{Common: Common{Mode: ModeAgent}, Name: "h", ControllerURL: "https://u:p@h.example/x", DockerHost: "x", DataDir: "y"}
	s := a.String()
	if !stringsContains(s, "controller=https://<redacted>@h.example/x") {
		t.Fatalf("expected redacted controller URL, got %q", s)
	}
}

// local helper to avoid importing strings just for one call
func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}