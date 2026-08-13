// Package docker is a minimal client for the Docker Engine HTTP
// API used by the agent. We use the REST API directly instead of
// github.com/docker/docker because the latter requires CGO and a
// huge transitive surface for what we need (list containers and
// ping version).
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a minimal Docker Engine client. It speaks to the
// engine over plain HTTP (the agent is expected to point at a
// socket-proxy with CONTAINERS=1, IMAGES=1, POST=0).
type Client struct {
	base   string
	client *http.Client
}

// New constructs a Client for the given Docker base URL (e.g.
// "tcp://127.0.0.1:2375", "http://127.0.0.1:2375", or
// "unix:///var/run/docker.sock"). The Docker Engine HTTP API is
// served over plain TCP, and the CLI treats tcp:// as an alias
// for http://, so we normalise it here. Note: the unix scheme
// requires a net.UnixListener transport and is not yet wired; the
// agent is expected to use a socket proxy reachable over HTTP for
// the Phase 1 release.
func New(base string) (*Client, error) {
	if base == "" {
		return nil, fmt.Errorf("docker: empty base URL")
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("docker: parse base: %w", err)
	}
	switch u.Scheme {
	case "tcp":
		u.Scheme = "http"
		base = u.String()
	case "http", "https", "unix":
		// ok
	default:
		return nil, fmt.Errorf("docker: unsupported scheme %q (expected http, https, tcp, or unix)", u.Scheme)
	}
	tr := &http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Client{
		base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}, nil
}

// Version is the result of GET /version.
type Version struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	OS            string `json:"Os"`
	Arch          string `json:"Arch"`
	KernelVersion string `json:"KernelVersion"`
	GoVersion     string `json:"GoVersion"`
}

// Ping issues GET /version. It is used both as a liveness check
// and to discover the Docker version string for the heartbeat.
func (c *Client) Ping(ctx context.Context) (*Version, error) {
	resp, err := c.get(ctx, "/version")
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docker: /version status %d: %s", resp.StatusCode, body)
	}
	var v Version
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("docker: decode /version: %w", err)
	}
	return &v, nil
}

// Container is the per-container result of GET /containers/json.
type Container struct {
	ID      string            `json:"Id"`
	Name    string            `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

// ListContainers queries GET /containers/json?all=1 and returns
// the parsed list. The result includes stopped containers; the
// caller decides what to keep.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	resp, err := c.get(ctx, "/containers/json?all=1&limit=0")
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docker: /containers/json status %d: %s", resp.StatusCode, body)
	}
	var out []Container
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("docker: decode /containers/json: %w", err)
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return c.client.Do(req)
}

// StripNamePrefix trims the leading '/' that Docker applies to
// container names in /containers/json.
func StripNamePrefix(s string) string {
	return strings.TrimPrefix(s, "/")
}
