package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestListContainersParsesDockerPayload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id":      "abc123",
				"Names":   []string{"/web", "/web-alias"},
				"Image":   "nginx:latest",
				"ImageID": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"State":   "running",
				"Status":  "Up 2 hours",
				"Labels":  map[string]string{"com.example.foo": "bar"},
			},
		})
	}))
	defer ts.Close()

	c, err := New(ts.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	con := got[0]
	if con.ID != "abc123" {
		t.Errorf("ID = %q", con.ID)
	}
	if got := con.Name(); got != "web" {
		t.Errorf("Name() = %q, want %q", got, "web")
	}
	if con.Image != "nginx:latest" {
		t.Errorf("Image = %q", con.Image)
	}
	if con.Labels["com.example.foo"] != "bar" {
		t.Errorf("Labels = %v", con.Labels)
	}
}

