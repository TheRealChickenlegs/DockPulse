package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
	if _, err := New("nginx", nil); err != nil {
		t.Errorf("New(nginx): unexpected error %v", err)
	}
	if _, err := New("ghcr.io/user/app", nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New(ghcr.io/user/app) = %v, want ErrUnsupported", err)
	}
	if _, err := New("nginx@sha256:abc", nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New(digest-pinned) = %v, want ErrUnsupported", err)
	}
}

func TestNewAppliesStoreCredential(t *testing.T) {
	store := &CredentialStore{creds: map[string]Credential{
		"docker.io": {Username: "chicken", Token: "pat-secret"},
		"quay.io":   {Username: "robot", Token: "quay-token"},
	}}
	p, err := New("nginx", store)
	if err != nil {
		t.Fatalf("New(nginx, store): %v", err)
	}
	h, ok := p.(*hub)
	if !ok {
		t.Fatalf("provider is %T, want *hub", p)
	}
	if h.username != "chicken" || h.password != "pat-secret" {
		t.Errorf("hub credential = %q:%q, want chicken:pat-secret", h.username, h.password)
	}

	for _, ref := range []string{"docker.io/nginx", "index.docker.io/nginx", "registry-1.docker.io/nginx"} {
		p, err := New(ref, store)
		if err != nil {
			t.Fatalf("New(%q, store): %v", ref, err)
		}
		h := p.(*hub)
		if h.username != "chicken" || h.password != "pat-secret" {
			t.Errorf("New(%q) credential = %q:%q, want chicken:pat-secret", ref, h.username, h.password)
		}
	}

	if _, err := New("ghcr.io/user/app", store); !errors.Is(err, ErrUnsupported) {
		t.Errorf("New(ghcr, store) = %v, want ErrUnsupported", err)
	}
}

func TestNewWithoutStoreIsAnonymous(t *testing.T) {
	p, err := New("nginx", nil)
	if err != nil {
		t.Fatalf("New(nginx): %v", err)
	}
	h := p.(*hub)
	if h.username != "" || h.password != "" {
		t.Errorf("hub credential = %q:%q, want anonymous", h.username, h.password)
	}
}

func TestCanonicalHost(t *testing.T) {
	for _, in := range []string{"", "docker.io", "index.docker.io", "registry-1.docker.io"} {
		if got := canonicalHost(in); got != "docker.io" {
			t.Errorf("canonicalHost(%q) = %q, want docker.io", in, got)
		}
	}
	if got := canonicalHost("ghcr.io"); got != "ghcr.io" {
		t.Errorf("canonicalHost(ghcr.io) = %q, want ghcr.io", got)
	}
}

func TestLoadCredentialDir(t *testing.T) {
	dir := t.TempDir()
	for file, body := range map[string]string{
		"docker.io": "chicken:pat-secret\n",
		"quay.io":   "robot:quay-token",
		".ignored":  "should:not-be-loaded",
	} {
		if err := os.WriteFile(dir+"/"+file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := LoadCredentialDir(dir)
	if err != nil {
		t.Fatalf("LoadCredentialDir: %v", err)
	}
	if store.Len() != 2 {
		t.Errorf("store has %d creds, want 2", store.Len())
	}
	c, ok := store.Lookup("docker.io")
	if !ok || c.Username != "chicken" || c.Token != "pat-secret" {
		t.Errorf("Lookup(docker.io) = %+v ok=%v, want chicken:pat-secret", c, ok)
	}
	if c, ok := store.Lookup("index.docker.io"); !ok || c.Token != "pat-secret" {
		t.Errorf("Lookup(index.docker.io) = %+v ok=%v, want docker.io credential", c, ok)
	}
	if _, ok := store.Lookup("ghcr.io"); ok {
		t.Error("Lookup(ghcr.io) unexpectedly found")
	}
}

func TestLoadCredentialDirErrors(t *testing.T) {
	if _, err := LoadCredentialDir(t.TempDir() + "/does-not-exist"); err == nil {
		t.Fatal("LoadCredentialDir(missing dir): expected error")
	}
	cases := []struct {
		name string
		body string
	}{
		{"no colon", "justatoken\n"},
		{"empty username", ":token\n"},
		{"empty token", "user:\n"},
		{"space in token", "user:two words\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(dir+"/docker.io", []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCredentialDir(dir); err == nil {
				t.Fatalf("LoadCredentialDir(%s): expected error", c.name)
			}
		})
	}
}

func TestHubListTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/repositories/library/nginx/tags/" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(hubTagsResponse{
			Results: []hubTag{
				{Name: "latest", LastUpdated: "2026-08-01T00:00:00Z"},
				{Name: "1.28.0", LastUpdated: "2026-07-01T00:00:00Z"},
				{Name: "1.27.3", LastUpdated: "2026-06-01T00:00:00Z"},
				{Name: "1.26.2", LastUpdated: "2026-05-01T00:00:00Z"},
				{Name: "1.26.1", LastUpdated: "2026-04-01T00:00:00Z"},
				{Name: "1.26.0", LastUpdated: "2026-03-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	h := &hub{client: srv.Client(), tagsURL: srv.URL}

	tags, err := h.ListTags(context.Background(), "library/nginx", 5)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 5 {
		t.Fatalf("ListTags returned %d tags, want 5", len(tags))
	}
	if tags[0].Name != "latest" {
		t.Errorf("first tag = %q, want latest", tags[0].Name)
	}
	if tags[4].Name != "1.26.1" {
		t.Errorf("last tag = %q, want 1.26.1", tags[4].Name)
	}
}

func TestHubListTagsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	h := &hub{client: srv.Client(), tagsURL: srv.URL}
	if _, err := h.ListTags(context.Background(), "library/nginx", 5); err == nil {
		t.Fatal("expected error for non-200 tag list")
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

func TestHubResolveDigestWithCredential(t *testing.T) {
	var sawBasicAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "chicken" || pass != "pat-secret" {
				http.Error(w, "expected Basic auth chicken:pat-secret", http.StatusUnauthorized)
				return
			}
			sawBasicAuth = true
			_ = json.NewEncoder(w).Encode(hubTokenResponse{Token: "authed-token"})
			return
		}
		if r.URL.Path == "/v2/library/nginx/manifests/latest" {
			if r.Header.Get("Authorization") != "Bearer authed-token" {
				t.Errorf("manifest Authorization = %q, want Bearer authed-token", r.Header.Get("Authorization"))
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
		username:    "chicken",
		password:    "pat-secret",
	}

	got, err := h.ResolveDigest(context.Background(), "library/nginx", "latest")
	if err != nil {
		t.Fatalf("ResolveDigest: %v", err)
	}
	if got != "sha256:remotedigest" {
		t.Errorf("digest = %q, want sha256:remotedigest", got)
	}
	if !sawBasicAuth {
		t.Fatal("token exchange did not authenticate")
	}
}

func TestHubAnonymousTokenHasNoBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("anonymous token exchange set Authorization %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(hubTokenResponse{Token: "tok"})
			return
		}
		if r.URL.Path == "/v2/library/nginx/manifests/latest" {
			w.Header().Set("Docker-Content-Digest", "sha256:remotedigest")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	h := &hub{client: srv.Client(), tokenURL: srv.URL + "/token", registryURL: srv.URL, service: "registry.docker.io"}
	if _, err := h.ResolveDigest(context.Background(), "library/nginx", "latest"); err != nil {
		t.Fatalf("ResolveDigest: %v", err)
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
