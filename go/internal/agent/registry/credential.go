package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Credential holds registry credentials used to authenticate pulls.
// Providers opt into consuming them (today only Docker Hub does;
// other providers will add their own auth in later phases).
type Credential struct {
	Username string
	Token    string
}

// CredentialStore is an immutable set of registry credentials keyed
// by canonical registry host (e.g. "docker.io", "ghcr.io"). A nil
// store means anonymous pulls everywhere.
type CredentialStore struct {
	creds map[string]Credential
}

// Lookup returns the credential for the canonical host key, if any.
func (s *CredentialStore) Lookup(host string) (Credential, bool) {
	if s == nil {
		return Credential{}, false
	}
	c, ok := s.creds[canonicalHost(host)]
	return c, ok
}

// Len returns the number of credentials in the store.
func (s *CredentialStore) Len() int {
	if s == nil {
		return 0
	}
	return len(s.creds)
}

// canonicalHost maps every Docker Hub hostname (including the empty
// registry in Parse) to the single credential-store key "docker.io".
// Other registries are used verbatim.
func canonicalHost(reg string) string {
	switch reg {
	case "", "docker.io", "index.docker.io", "registry-1.docker.io":
		return "docker.io"
	default:
		return reg
	}
}

// LoadCredentialDir reads a credentials directory into a store. Each
// file is keyed by its filename (the canonical registry host, e.g.
// "docker.io") and must contain one line of the form "username:token"
// — the same shape as a `docker login` pair. Dotfiles and
// subdirectories are ignored. A malformed file is an error so a typo
// fails loudly at startup rather than silently degrading to anonymous
// pulls.
func LoadCredentialDir(dir string) (*CredentialStore, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("registry credentials dir: %w", err)
	}
	store := &CredentialStore{creds: map[string]Credential{}}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("registry credentials: read %s: %w", e.Name(), err)
		}
		c, err := parseCredentialLine(string(raw))
		if err != nil {
			return nil, fmt.Errorf("registry credentials %s: %w", e.Name(), err)
		}
		store.creds[e.Name()] = *c
	}
	return store, nil
}

// parseCredentialLine parses one "username:token" line. A trailing
// newline is tolerated; the token is never logged.
func parseCredentialLine(line string) (*Credential, error) {
	username, token, ok := strings.Cut(strings.TrimSpace(line), ":")
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
	if !ok || username == "" || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("expected one line of the form username:token")
	}
	return &Credential{Username: username, Token: token}, nil
}
