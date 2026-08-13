// Package registry resolves the remote digest of a (repo, tag) pair
// so the agent can detect when a running image has been updated.
//
// Providers are pluggable and chosen by the registry host in the
// image reference. Phase 2 ships the Docker Hub provider (bearer-token
// auth, anonymous or via an optional personal access token); other
// registries (ghcr, quay, gcr, ecr, gitlab) land in later phases.
package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupported is returned by New when no provider handles the
// registry host in the reference. Callers treat it as "skip this
// image", not as a fatal error.
var ErrUnsupported = errors.New("registry: unsupported registry host")

// Provider resolves the digest of a tag on a registry.
type Provider interface {
	// ResolveDigest returns the canonical content digest (the
	// Docker-Content-Digest header) for repo:tag, or an error if
	// the tag cannot be resolved.
	ResolveDigest(ctx context.Context, repo, tag string) (string, error)
}

// New returns the provider for the given fully-qualified image
// reference, or ErrUnsupported when no provider is implemented for
// its registry host. A digest-pinned reference (no tag) is not
// pollable and returns ErrUnsupported. store supplies credentials
// for the resolved registry host; nil means anonymous pulls.
func New(ref string, store *CredentialStore) (Provider, error) {
	r, err := Parse(ref)
	if err != nil {
		return nil, err
	}
	if r.Digest != "" {
		return nil, ErrUnsupported
	}
	switch r.Registry {
	case "", "docker.io", "index.docker.io", "registry-1.docker.io":
		var cred *Credential
		if c, ok := store.Lookup(canonicalHost(r.Registry)); ok {
			cred = &c
		}
		return NewHub(cred), nil
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupported, r.Registry)
	}
}

// Ref is a parsed image reference.
type Ref struct {
	// Registry is the registry host ("" for Docker Hub).
	Registry string
	// Repo is the fully-qualified repository, e.g. "library/nginx"
	// or "ghcr.io/user/app".
	Repo string
	// Tag defaults to "latest" when the reference has none.
	Tag string
	// Digest is the pinned digest when the reference is
	// repo@sha256:…; otherwise "".
	Digest string
}

// Parse splits an image reference into its registry, repo, tag, and
// optional digest. It mirrors the resolution rules in the Docker CLI:
// a first path component that contains '.' or ':' (or is
// "localhost") is a registry host; otherwise the reference is
// Docker Hub and the first component is the namespace or image name.
func Parse(ref string) (Ref, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Ref{}, errors.New("registry: empty image reference")
	}

	var r Ref
	// Strip the pinned digest first: repo@sha256:….
	at := strings.LastIndex(ref, "@")
	if at >= 0 {
		r.Digest = ref[at+1:]
		ref = ref[:at]
		if !strings.HasPrefix(r.Digest, "sha256:") && !strings.HasPrefix(r.Digest, "sha512:") {
			return Ref{}, fmt.Errorf("registry: unsupported digest format %q", r.Digest)
		}
	}

	first, rest, hasSlash := strings.Cut(ref, "/")
	if !hasSlash {
		// Single component: Docker Hub, official image. It may
		// still carry a tag, e.g. "nginx:1.27".
		r.Registry = ""
		name, tag, hasTag := strings.Cut(first, ":")
		r.Repo = "library/" + name
		r.Tag = "latest"
		if hasTag {
			r.Tag = tag
		}
		return r, nil
	}
	if isRegistryHost(first) {
		r.Registry = first
		repoTag := rest
		repo, tag, hasTag := strings.Cut(repoTag, ":")
		if !hasTag {
			tag = "latest"
		}
		if repo == "" {
			return Ref{}, fmt.Errorf("registry: invalid reference %q", ref)
		}
		r.Repo = repo
		r.Tag = tag
		return r, nil
	}
	// Docker Hub with namespace: user/app or user/app:tag.
	repoTag := ref
	repo, tag, hasTag := strings.Cut(repoTag, ":")
	if !hasTag {
		tag = "latest"
	}
	r.Registry = ""
	r.Repo = repo
	r.Tag = tag
	return r, nil
}

// isRegistryHost reports whether the first path component is a
// registry host rather than a Docker Hub namespace.
func isRegistryHost(s string) bool {
	if s == "localhost" {
		return true
	}
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}

// String returns the canonical "repo:tag" form, used as the stable
// key for the controller's images table.
func (r Ref) String() string {
	return r.Repo + ":" + r.Tag
}
