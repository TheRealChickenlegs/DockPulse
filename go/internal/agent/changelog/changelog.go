// Package changelog discovers changelog sources for a running image
// (from OCI labels) and fetches release notes from those sources.
//
// Phase 2 supports the GitHub Releases API for repos referenced by
// the org.opencontainers.image.source / .url labels. The set of
// reachable hosts is hardcoded (api.github.com) so a malicious image
// cannot turn the agent into an SSRF proxy (see docs/THREAT_MODEL.md
// T15).
package changelog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// LabelSource and LabelURL are the OCI annotation keys that
	// point at an image's upstream project.
	LabelSource = "org.opencontainers.image.source"
	LabelURL    = "org.opencontainers.image.url"

	githubAPIBase = "https://api.github.com"
)

// Source describes one changelog source for an image.
type Source struct {
	Kind string `json:"kind"` // "github"
	Repo string `json:"repo"` // owner/name, e.g. "nginx/nginx"
	URL  string `json:"url"`  // the label-provided URL
}

// Entry is a single release note.
type Entry struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	Body        string `json:"body,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Hash        string `json:"hash"`
}

// SourcesFromLabels extracts changelog sources from an image's OCI
// labels. GitHub URLs are the only kind supported in Phase 2; other
// hosts are ignored.
func SourcesFromLabels(labels map[string]string) []Source {
	var out []Source
	seen := map[string]bool{}
	for _, key := range []string{LabelSource, LabelURL} {
		raw := strings.TrimSpace(labels[key])
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Host != "github.com" && u.Host != "www.github.com") {
			continue
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		repo := strings.TrimSuffix(parts[0]+"/"+parts[1], ".git")
		if seen[repo] {
			continue
		}
		seen[repo] = true
		out = append(out, Source{Kind: "github", Repo: repo, URL: raw})
	}
	return out
}

// Hash computes the dedup key for an entry: SHA-256 of the version
// and URL. The controller stores this so re-fetching the same
// release never duplicates rows.
func Hash(version, url string) string {
	sum := sha256.Sum256([]byte(version + "|" + url))
	return hex.EncodeToString(sum[:])
}

// Fetcher fetches release notes for the sources attached to an image.
type Fetcher struct {
	client *http.Client
	base   string
}

// NewFetcher constructs a Fetcher that only talks to api.github.com.
func NewFetcher() *Fetcher {
	return NewFetcherWithBase(githubAPIBase)
}

// NewFetcherWithBase is NewFetcher with an injectable API base URL
// (used by tests).
func NewFetcherWithBase(base string) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 15 * time.Second,
				IdleConnTimeout:       30 * time.Second,
			},
		},
		base: base,
	}
}

// FetchSources returns entries for every source. Sources that fail
// individually are skipped rather than aborting the batch, and the
// failures are reported so the caller can log them.
func (f *Fetcher) FetchSources(ctx context.Context, sources []Source) ([]Entry, error) {
	var entries []Entry
	var errs []string
	for _, s := range sources {
		switch s.Kind {
		case "github":
			got, err := f.fetchGitHub(ctx, s.Repo)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", s.Repo, err))
				continue
			}
			entries = append(entries, got...)
		}
	}
	if len(errs) > 0 && len(entries) == 0 {
		return nil, fmt.Errorf("changelog: all sources failed: %s", strings.Join(errs, "; "))
	}
	return entries, nil
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

func (f *Fetcher) fetchGitHub(ctx context.Context, repo string) ([]Entry, error) {
	u := f.base + "/repos/" + url.PathEscape(repo) + "/releases?per_page=5"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dockpulse-agent")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github: releases for %s status %d: %s", repo, resp.StatusCode, truncate(string(body), 256))
	}
	var rels []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, fmt.Errorf("github: decode releases for %s: %w", repo, err)
	}
	out := make([]Entry, 0, len(rels))
	for _, r := range rels {
		if r.TagName == "" {
			continue
		}
		out = append(out, Entry{
			Version:     r.TagName,
			Source:      "github",
			Title:       firstNonEmpty(r.Name, r.TagName),
			URL:         r.HTMLURL,
			Body:        strings.TrimSpace(r.Body),
			PublishedAt: r.PublishedAt,
			Hash:        Hash(r.TagName, r.HTMLURL),
		})
	}
	return out, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
