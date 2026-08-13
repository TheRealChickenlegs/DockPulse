package docker

import (
	"testing"
)

func TestNewNormalisesTCP(t *testing.T) {
	c, err := New("tcp://socket-proxy:2375")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http://socket-proxy:2375"
	if c.base != want {
		t.Fatalf("base = %q, want %q", c.base, want)
	}
}

func TestNewRejectsUnknownScheme(t *testing.T) {
	if _, err := New("ftp://example.com"); err == nil {
		t.Fatal("expected error for ftp://")
	}
}

func TestNewKeepsHTTPSAndUnix(t *testing.T) {
	for _, in := range []string{"https://engine:2376", "unix:///var/run/docker.sock"} {
		c, err := New(in)
		if err != nil {
			t.Fatalf("New(%q): %v", in, err)
		}
		if c.base != in {
			t.Fatalf("base = %q, want %q", c.base, in)
		}
	}
}
