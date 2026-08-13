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
	if c.Listen != ":9787" {
		t.Fatalf("expected default listen :9787, got %s", c.Listen)
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

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":                  nil,
		"   ":               nil,
		"a":                 {"a"},
		"a,b":               {"a", "b"},
		" a , b ,":          {"a", "b"},
		"10.0.0.0/8, ,172.16.0.0/12": {"10.0.0.0/8", "172.16.0.0/12"},
	}
	for in, want := range cases {
		got := splitCSV(in)
		if len(got) != len(want) {
			t.Fatalf("splitCSV(%q) = %v, want %v", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("splitCSV(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestControllerStringRedactsTrustedProxyList(t *testing.T) {
	c := Controller{
		Common:         Common{Mode: ModeController},
		Listen:         ":8080",
		DBPath:         "/tmp/x.db",
		TrustedProxies: []string{"10.0.0.0/8", "192.0.2.1"},
	}
	s := c.String()
	if !stringsContains(s, "trusted_proxies=2") {
		t.Fatalf("expected trusted_proxies count in %q", s)
	}
	if stringsContains(s, "10.0.0.0/8") || stringsContains(s, "192.0.2.1") {
		t.Fatalf("String() should not leak trusted proxy addresses: %q", s)
	}
}

func TestLoadControllerReadsTrustedProxiesFromEnv(t *testing.T) {
	t.Setenv("DOCKPULSE_TRUSTED_PROXIES", "10.0.0.0/8, 192.0.2.1")
	cfg, err := Load([]string{"--mode=controller"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := cfg.(Controller)
	if !ok {
		t.Fatalf("expected Controller, got %T", cfg)
	}
	if len(c.TrustedProxies) != 2 {
		t.Fatalf("expected 2 trusted proxies, got %d: %v", len(c.TrustedProxies), c.TrustedProxies)
	}
	if c.TrustedProxies[0] != "10.0.0.0/8" || c.TrustedProxies[1] != "192.0.2.1" {
		t.Fatalf("unexpected trusted proxies: %v", c.TrustedProxies)
	}
}

func TestLoadControllerFlagOverridesEnv(t *testing.T) {
	t.Setenv("DOCKPULSE_TRUSTED_PROXIES", "10.0.0.0/8")
	cfg, err := Load([]string{"--mode=controller", "--trusted-proxies=172.16.0.0/12"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := cfg.(Controller)
	if len(c.TrustedProxies) != 1 || c.TrustedProxies[0] != "172.16.0.0/12" {
		t.Fatalf("expected flag to override env, got %v", c.TrustedProxies)
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