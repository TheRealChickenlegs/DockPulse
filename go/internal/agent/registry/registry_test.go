package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in       string
		registry string
		repo     string
		tag      string
		digest   string
	}{
		{"nginx", "", "library/nginx", "latest", ""},
		{"nginx:1.27", "", "library/nginx", "1.27", ""},
		{"user/app", "", "user/app", "latest", ""},
		{"user/app:tag", "", "user/app", "tag", ""},
		{"docker.io/user/app", "docker.io", "user/app", "latest", ""},
		{"index.docker.io/library/nginx:1", "index.docker.io", "library/nginx", "1", ""},
		{"ghcr.io/user/app:v1", "ghcr.io", "user/app", "v1", ""},
		{"localhost:5000/app:tag", "localhost:5000", "app", "tag", ""},
		{"nginx@sha256:deadbeef", "", "library/nginx", "latest", "sha256:deadbeef"},
	}
	for _, c := range cases {
		r, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", c.in, err)
			continue
		}
		if r.Registry != c.registry || r.Repo != c.repo || r.Tag != c.tag || r.Digest != c.digest {
			t.Errorf("Parse(%q) = %+v, want registry=%q repo=%q tag=%q digest=%q",
				c.in, r, c.registry, c.repo, c.tag, c.digest)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "repo@md5:abc"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error", in)
		}
	}
}

func TestRefString(t *testing.T) {
	r, _ := Parse("nginx")
	if got := r.String(); got != "library/nginx:latest" {
		t.Errorf("String() = %q, want %q", got, "library/nginx:latest")
	}
}

func TestNew(t *testing.T) {
	if _, err := New("nginx"); err != nil {
		t.Errorf("New(nginx): unexpected error %v", err)
	}
	if _, err := New("ghcr.io/user/app"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New(ghcr.io/user/app) = %v, want ErrUnsupported", err)
	}
	if _, err := New("nginx@sha256:abc"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New(digest-pinned) = %v, want ErrUnsupported", err)
	}
}

func TestHubResolveDigest(t *testing.T) {
	var tokenSeen bool
	var repoForToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenSeen = true
			repoForToken = r.URL.Query().Get("scope")
			_ = json.NewEncoder(w).Encode(hubTokenResponse{Token: "tok"})
			return
		}
		if r.URL.Path == "/v2/library/nginx/manifests/latest" {
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("manifest Authorization = %q, want Bearer tok", r.Header.Get("Authorization"))
			}
			w.Header().Set("Docker-Content-Digest", "sha256:remotedigest")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	h := &hub{
		client:      srv.Client(),
		tokenURL:    srv.URL + "/token",
		registryURL: srv.URL,
		service:     "registry.docker.io",
	}

	got, err := h.ResolveDigest(context.Background(), "library/nginx", "latest")
	if err != nil {
		t.Fatalf("ResolveDigest: %v", err)
	}
	if got != "sha256:remotedigest" {
		t.Errorf("digest = %q, want sha256:remotedigest", got)
	}
	if !tokenSeen {
		t.Fatal("token endpoint was not called")
	}
	if !strings.Contains(repoForToken, "library/nginx") {
		t.Errorf("token scope = %q, want it to contain library/nginx", repoForToken)
	}
}

func TestHubResolveDigestErrorContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(hubTokenResponse{Token: "tok"})
			return
		}
		http.Error(w, "oops", http.StatusNotFound)
	}))
	defer srv.Close()

	h := &hub{client: srv.Client(), tokenURL: srv.URL + "/token", registryURL: srv.URL}

	if _, err := h.ResolveDigest(context.Background(), "library/nginx", "latest"); err == nil {
		t.Fatal("expected error for missing manifest")
	} else if !strings.Contains(err.Error(), "library/nginx") {
		t.Errorf("error %q does not name the repo", err)
	}
}
