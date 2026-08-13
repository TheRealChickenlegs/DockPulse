package registry

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Credential holds registry credentials used to authenticate pulls.
// Today only the Docker Hub provider consumes them; other providers
// (ghcr, quay, …) will add their own auth in later phases.
type Credential struct {
	Username string
	Token    string
}

// LoadCredentialFile reads a registry credential file. The format is
// a single line "username:token" (the same shape as a `docker login`
// pair). A trailing newline is tolerated; the token is never logged.
func LoadCredentialFile(path string) (*Credential, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("registry credential file: %w", err)
	}
	line := strings.TrimSpace(string(raw))
	username, token, ok := strings.Cut(line, ":")
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
	if !ok || username == "" || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("registry credential file must contain one line of the form username:token")
	}
	return &Credential{Username: username, Token: token}, nil
}

// WithCredential attaches registry credentials to the provider. It
// only affects providers that support authentication (Docker Hub);
// other providers ignore it. A nil credential is a no-op.
func WithCredential(cred *Credential) Option {
	return func(p Provider) {
		if cred == nil {
			return
		}
		if h, ok := p.(*hub); ok {
			h.username = cred.Username
			h.password = cred.Token
		}
	}
}
