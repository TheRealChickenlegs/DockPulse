package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	ua := UserAgent()
	if !strings.HasPrefix(ua, "DockPulse/") {
		t.Fatalf("UserAgent should start with DockPulse/, got %q", ua)
	}
	if !strings.Contains(ua, runtime.GOOS) {
		t.Fatalf("UserAgent should mention GOOS %s, got %q", runtime.GOOS, ua)
	}
}

func TestString(t *testing.T) {
	s := String()
	if !strings.HasPrefix(s, "DockPulse ") {
		t.Fatalf("String should start with 'DockPulse ', got %q", s)
	}
}