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
)

// hub is the Docker Hub v2 registry provider. Docker Hub requires a
// bearer token scoped to the repository before the manifest API
// answers; the token exchange is anonymous for public images, which
// covers the Phase 2 use case.
type hub struct {
	client *http.Client
	// tokenURL, registryURL, and service are injectable for tests.
	tokenURL    string
	registryURL string
	service     string
}

// NewHub constructs the Docker Hub provider.
func NewHub() Provider {
	return &hub{
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
	}
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

func (h *hub) token(ctx context.Context, repo string) (string, error) {
	u := h.tokenURL + "?service=" + url.QueryEscape(h.service) +
		"&scope=repository:" + pathEscape(repo) + ":pull"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
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
