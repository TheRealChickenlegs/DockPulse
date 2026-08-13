package changelog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourcesFromLabels(t *testing.T) {
	labels := map[string]string{
		"org.opencontainers.image.source": "https://github.com/nginx/nginx",
		"org.opencontainers.image.url":    "https://nginx.org",
	}
	got := SourcesFromLabels(labels)
	if len(got) != 1 {
		t.Fatalf("SourcesFromLabels = %+v, want exactly 1 source", got)
	}
	if got[0].Repo != "nginx/nginx" || got[0].Kind != "github" {
		t.Errorf("got %+v, want github nginx/nginx", got[0])
	}
}

func TestSourcesFromLabelsIgnoresForeignHosts(t *testing.T) {
	labels := map[string]string{
		"org.opencontainers.image.source": "https://gitlab.com/user/app",
		"org.opencontainers.image.url":    "https://example.com/foo",
	}
	if got := SourcesFromLabels(labels); len(got) != 0 {
		t.Errorf("expected no sources, got %+v", got)
	}
}

func TestSourcesFromLabelsDedupes(t *testing.T) {
	labels := map[string]string{
		"org.opencontainers.image.source": "https://github.com/user/app.git",
		"org.opencontainers.image.url":    "https://github.com/user/app",
	}
	got := SourcesFromLabels(labels)
	if len(got) != 1 {
		t.Errorf("expected deduped single source, got %+v", got)
	}
}

func TestHash(t *testing.T) {
	a := Hash("v1", "https://example.com/x")
	b := Hash("v1", "https://example.com/x")
	c := Hash("v2", "https://example.com/x")
	if a != b {
		t.Errorf("Hash not deterministic: %s vs %s", a, b)
	}
	if a == c {
		t.Errorf("Hash collision for different versions")
	}
}

func TestFetchGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/nginx/nginx/releases" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]githubRelease{
			{TagName: "1.27.0", Name: "1.27.0", HTMLURL: "https://github.com/nginx/nginx/releases/tag/1.27.0", PublishedAt: "2026-01-01T00:00:00Z"},
			{TagName: "", HTMLURL: "https://github.com/nginx/nginx/releases/tag/empty"},
		})
	}))
	defer srv.Close()

	f := NewFetcherWithBase(srv.URL)
	got, err := f.FetchSources(context.Background(), []Source{{Kind: "github", Repo: "nginx/nginx"}})
	if err != nil {
		t.Fatalf("FetchSources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (empty tag skipped)", len(got))
	}
	if got[0].Version != "1.27.0" || got[0].Hash == "" {
		t.Errorf("got %+v", got[0])
	}
}

func TestFetchSourcesSkipsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusForbidden)
	}))
	defer srv.Close()

	f := NewFetcherWithBase(srv.URL)
	if _, err := f.FetchSources(context.Background(), []Source{{Kind: "github", Repo: "a/b"}}); err == nil {
		t.Fatal("expected error when the only source fails")
	}
}
