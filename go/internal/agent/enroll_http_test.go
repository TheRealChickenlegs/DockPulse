package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentapi"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentca"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/db"
)

func TestEnrollOverPlainHTTP(t *testing.T) {
	d, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open controller db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ca, err := agentca.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	srv := agentapi.New(d, ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	srv.Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	tok := auth.RandomToken(24)
	sum := sha256.Sum256([]byte(tok))
	now := time.Now().UTC()
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO enrollment_tokens(id, token_hash, server_name, created_by, created_at, expires_at)
		VALUES (?, ?, ?, NULL, ?, ?)
	`, auth.RandomToken(16), hex.EncodeToString(sum[:]), "test", now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	dataDir := t.TempDir()
	tokPath := filepath.Join(dataDir, "token")
	if err := os.WriteFile(tokPath, []byte(tok), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := config.Agent{
		Common:          config.Common{Mode: config.ModeAgent},
		Name:            "test",
		ControllerURL:   ts.URL,
		DockerHost:      "tcp://127.0.0.1:1",
		DataDir:         dataDir,
		EnrollTokenFile: tokPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = Run(ctx, cfg) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dataDir, "agent.crt")); err == nil {
			if _, err := os.Stat(filepath.Join(dataDir, "identity.json")); err == nil {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "agent.crt")); err != nil {
		t.Fatalf("agent.crt was not created; enrollment over plain http failed")
	}
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM servers WHERE name = 'test'`).Scan(&n); err != nil {
		t.Fatalf("query servers: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 server row after enrollment, got %d", n)
	}
}
