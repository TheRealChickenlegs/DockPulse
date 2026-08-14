package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultHubTokenURL    = "https://auth.docker.io/token"
	defaultHubRegistryURL = "https://registry-1.docker.io"
	defaultHubService     = "registry.docker.io"
	defaultHubTagsURL     = "https://hub.docker.com"
)

// hub is the Docker Hub v2 registry provider. Docker Hub requires a
// bearer token scoped to the repository before the manifest API
// answers. The token exchange is anonymous for public images by
// default; when credentials are set (a personal access token via
// --registry-credentials-dir) the exchange is made as that account,
// which lifts the unauthenticated pull rate limit.
type hub struct {
	client *http.Client
	// tokenURL, registryURL, and service are injectable for tests.
	tokenURL    string
	registryURL string
	service     string
	// tagsURL is the Docker Hub web API base used to enumerate an
	// image's published tags (release-history fallback).
	tagsURL string
	// username and password authenticate the token exchange. When both
	// are empty the exchange is anonymous.
	username string
	password string
}

// NewHub constructs the Docker Hub provider. cred authenticates the
// token exchange when non-nil; nil keeps it anonymous.
func NewHub(cred *Credential) Provider {
	h := &hub{
		client: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 15 * time.Second,
				IdleConnTimeout:       30 * time.Second,
			},
		},
		tokenURL:    defaultHubTokenURL,
		registryURL: defaultHubRegistryURL,
		service:     defaultHubService,
		tagsURL:     defaultHubTagsURL,
	}
	if cred != nil {
		h.username = cred.Username
		h.password = cred.Token
	}
	return h
}

type hubTokenResponse struct {
	Token string `json:"token"`
}

// ResolveDigest returns the Docker-Content-Digest header for the
// manifest list at repo:tag. It follows the standard Docker
// registry v2 flow: fetch a bearer token, then request the manifest
// accepting all supported manifest media types.
func (h *hub) ResolveDigest(ctx context.Context, repo, tag string) (string, error) {
	token, err := h.token(ctx, repo)
	if err != nil {
		return "", err
	}

	u := h.registryURL + "/v2/" + pathEscape(repo) + "/manifests/" + pathEscape(tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", manifestAcceptHeader())

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry: get manifest %s:%s: %w", repo, tag, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry: manifest %s:%s status %d: %s", repo, tag, resp.StatusCode, truncate(string(body), 256))
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry: manifest %s:%s response missing Docker-Content-Digest", repo, tag)
	}
	return digest, nil
}

// hubTagsResponse is the subset of Docker Hub's tag-list response the
// agent needs (name + last_updated). Docker Hub already returns tags
// newest-first when ordering=last_updated.
type hubTagsResponse struct {
	Results []hubTag `json:"results"`
}

type hubTag struct {
	Name        string `json:"name"`
	LastUpdated string `json:"last_updated"`
}

// ListTags returns up to limit published tags for repo from the
// Docker Hub web API, most recently updated first. The host is the
// hardcoded hub.docker.com (SSRF-bounded, T15); the repo path is
// escaped per segment.
func (h *hub) ListTags(ctx context.Context, repo string, limit int) ([]Tag, error) {
	u := h.tagsURL + "/v2/repositories/" + pathEscape(repo) + "/tags/?page_size=100&ordering=last_updated"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: list tags for %s: %w", repo, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry: list tags for %s status %d: %s", repo, resp.StatusCode, truncate(string(body), 256))
	}
	var tr hubTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("registry: decode tags for %s: %w", repo, err)
	}
	out := make([]Tag, 0, len(tr.Results))
	for _, t := range tr.Results {
		if t.Name == "" {
			continue
		}
		out = append(out, Tag{Name: t.Name, LastUpdated: t.LastUpdated})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (h *hub) token(ctx context.Context, repo string) (string, error) {
	u := h.tokenURL + "?service=" + url.QueryEscape(h.service) +
		"&scope=repository:" + pathEscape(repo) + ":pull"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	if h.username != "" {
		req.SetBasicAuth(h.username, h.password)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry: token for %s: %w", repo, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry: token for %s status %d: %s", repo, resp.StatusCode, truncate(string(body), 256))
	}
	var tr hubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("registry: decode token for %s: %w", repo, err)
	}
	if tr.Token == "" {
		return "", errors.New("registry: empty token from auth.docker.io")
	}
	return tr.Token, nil
}

// manifestAcceptHeader lists every media type the registry may use
// for a manifest or manifest list. Requesting the whole set lets the
// server return whatever it has; we only care about the
// Docker-Content-Digest header, which identifies the resolved
// artifact.
func manifestAcceptHeader() string {
	return strings.Join([]string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
	}, ", ")
}

// pathEscape escapes a repo path segment for use in a URL path while
// preserving the '/' separators.
func pathEscape(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
