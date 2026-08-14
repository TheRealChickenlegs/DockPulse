package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/agent/changelog"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/agent/docker"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/agent/registry"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
)

func TestValidateRequiresName(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when name is empty")
	}
}

func TestValidateRequiresHTTPS(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "http://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when controller URL is http://")
	}
}

func TestValidateRefusesProcDataDir(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       "/proc/self",
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error when data dir is under /proc")
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "https://example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsLocalHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.53", true},
		{"::1", true},
		{"localhost", true},
		{"10.0.0.5", true},
		{"172.16.3.4", true},
		{"192.168.10.10", true},
		{"169.254.1.1", true}, // link-local
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.5", false},
		{"myhost.local", true},
		{"myhost.lan", true},
		{"myhost.example.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLocalHost(c.host); got != c.want {
			t.Errorf("isLocalHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestValidateAllowsHTTPForLocalHosts(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9787",
		"http://localhost:9787",
		"http://10.0.0.5:9787",
		"http://192.168.10.10:9787",
		"http://myhost.local:9787",
	}
	for _, u := range cases {
		cfg := config.Agent{
			Common:        config.Common{Mode: config.ModeAgent},
			Name:          "x",
			ControllerURL: u,
			DockerHost:    "unix:///var/run/docker.sock",
			DataDir:       t.TempDir(),
		}
		if err := validate(cfg); err != nil {
			t.Errorf("validate(%q): unexpected error: %v", u, err)
		}
	}
}

func TestValidateRejectsHTTPForPublicHosts(t *testing.T) {
	cfg := config.Agent{
		Common:        config.Common{Mode: config.ModeAgent},
		Name:          "x",
		ControllerURL: "http://dockpulse.example.com",
		DockerHost:    "unix:///var/run/docker.sock",
		DataDir:       t.TempDir(),
	}
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for http public host without --allow-insecure-controller")
	}
}

func TestValidateAllowsInsecureControllerOverride(t *testing.T) {
	cfg := config.Agent{
		Common:                  config.Common{Mode: config.ModeAgent},
		Name:                    "x",
		ControllerURL:           "http://dockpulse.example.com",
		AllowInsecureController: true,
		DockerHost:              "unix:///var/run/docker.sock",
		DataDir:                 t.TempDir(),
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("with --allow-insecure-controller, expected no error: %v", err)
	}
}

func TestFetchChangelogCooldown(t *testing.T) {
	unreachable, err := docker.New("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		Docker:             unreachable,
		lastChangelogFetch: map[string]time.Time{},
	}
	const ref = "library/nginx:latest"

	// Within the cooldown window the fetch is skipped entirely, so no
	// Docker call is made.
	d.lastChangelogFetch[ref] = time.Now()
	if got := d.fetchChangelog(context.Background(), "imgid", ref); got != nil {
		t.Fatalf("expected nil inside cooldown, got %d entries", len(got))
	}

	// Fresh ref: the attempt is recorded, then the Docker call fails
	// fast against the unreachable engine and returns nil.
	d.lastChangelogFetch = map[string]time.Time{}
	if got := d.fetchChangelog(context.Background(), "imgid", ref); got != nil {
		t.Fatalf("expected nil on inspect failure, got %d entries", len(got))
	}

	// The failed attempt still armed the cooldown, so an immediate
	// retry is skipped without touching Docker again.
	before := len(d.lastChangelogFetch)
	if got := d.fetchChangelog(context.Background(), "imgid", ref); got != nil {
		t.Fatalf("expected nil on immediate retry, got %d entries", len(got))
	}
	if len(d.lastChangelogFetch) != before {
		t.Fatal("cooldown map changed on a skipped fetch")
	}
}

func TestPollRegistryUploadsChangelogWithoutUpdate(t *testing.T) {
	// Fake Docker: one running container whose image has an OCI
	// changelog label but lives on a registry with no provider, so
	// the registry resolve is skipped entirely and only the changelog
	// path runs.
	fakeDocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]docker.Container{{
				ID:      "c1",
				Names:   []string{"/web"},
				Image:   "ghcr.io/user/app:latest",
				ImageID: "sha256:local",
				State:   "running",
				Labels:  map[string]string{"com.docker.compose.project": "apps"},
			}})
		case r.URL.Path == "/images/sha256:local/json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id": "sha256:local",
				"Config": map[string]any{
					"Labels": map[string]string{"org.opencontainers.image.source": "https://github.com/nginx/nginx"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeDocker.Close()
	dockerClient, err := docker.New(fakeDocker.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Fake GitHub: two releases for nginx/nginx.
	var githubHits int
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubHits++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "1.28.0", "name": "1.28.0", "html_url": "https://github.com/nginx/nginx/releases/tag/1.28.0", "published_at": "2026-07-01T00:00:00Z"},
			{"tag_name": "1.27.3", "name": "1.27.3", "html_url": "https://github.com/nginx/nginx/releases/tag/1.27.3", "published_at": "2026-06-01T00:00:00Z"},
		})
	}))
	defer fakeGitHub.Close()

	// Fake controller: record which agent endpoints get hit.
	var uploads, reports int
	var uploadedEntries int
	fakeController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/v1/changelog/upload":
			uploads++
			var body struct {
				ImageRef string `json:"image_ref"`
				Entries  []struct {
					Version string `json:"version"`
				} `json:"entries"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			uploadedEntries = len(body.Entries)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "inserted": len(body.Entries)})
		case "/agent/v1/updates/report":
			reports++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeController.Close()

	d := &Daemon{
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		Docker:             dockerClient,
		baseURL:            fakeController.URL,
		httpClient:         fakeController.Client(),
		changelogFetcher:   changelog.NewFetcherWithBase(fakeGitHub.URL),
		lastReported:       map[string]string{},
		lastChangelogFetch: map[string]time.Time{},
	}

	ctx := context.Background()
	if err := d.pollRegistry(ctx); err != nil {
		t.Fatalf("pollRegistry: %v", err)
	}

	// No update is reported (unsupported registry), but the release
	// history is uploaded anyway.
	if reports != 0 {
		t.Errorf("updates/report called %d times, want 0", reports)
	}
	if uploads != 1 {
		t.Fatalf("changelog/upload called %d times, want 1", uploads)
	}
	if uploadedEntries != 2 {
		t.Errorf("uploaded %d entries, want 2", uploadedEntries)
	}
	if githubHits != 1 {
		t.Errorf("github fetched %d times, want 1", githubHits)
	}

	// The cooldown keeps a second poll quiet: no re-fetch, no upload.
	if err := d.pollRegistry(ctx); err != nil {
		t.Fatalf("pollRegistry (2nd): %v", err)
	}
	if uploads != 1 {
		t.Errorf("changelog/upload called %d times after second poll, want 1", uploads)
	}
	if githubHits != 1 {
		t.Errorf("github fetched %d times after second poll, want 1", githubHits)
	}
}

func TestPollRegistryTagHistoryFallback(t *testing.T) {
	fakeDocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]docker.Container{{
				ID:      "c1",
				Names:   []string{"/web"},
				Image:   "ghcr.io/user/app:latest",
				ImageID: "sha256:local",
				State:   "running",
				Labels:  map[string]string{},
			}})
		case r.URL.Path == "/images/sha256:local/json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":     "sha256:local",
				"Config": map[string]any{"Labels": map[string]string{}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeDocker.Close()
	dockerClient, err := docker.New(fakeDocker.URL)
	if err != nil {
		t.Fatal(err)
	}

	var uploads int
	var uploadedRef string
	var uploadedSources []string
	fakeController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/v1/changelog/upload":
			uploads++
			var body struct {
				ImageRef string `json:"image_ref"`
				Entries  []struct {
					Version string `json:"version"`
					Source  string `json:"source"`
				} `json:"entries"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			uploadedRef = body.ImageRef
			for _, e := range body.Entries {
				uploadedSources = append(uploadedSources, e.Source)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "inserted": len(body.Entries)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeController.Close()

	d := &Daemon{
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		Docker:             dockerClient,
		baseURL:            fakeController.URL,
		httpClient:         fakeController.Client(),
		changelogFetcher:   changelog.NewFetcherWithBase("http://unused"),
		tagLister:          fakeTagLister{},
		lastReported:       map[string]string{},
		lastChangelogFetch: map[string]time.Time{},
	}

	if err := d.pollRegistry(context.Background()); err != nil {
		t.Fatalf("pollRegistry: %v", err)
	}

	if uploads != 1 {
		t.Fatalf("changelog/upload called %d times, want 1", uploads)
	}
	if uploadedRef != "user/app:latest" {
		t.Errorf("uploaded image_ref = %q, want user/app:latest", uploadedRef)
	}
	for _, s := range uploadedSources {
		if s != changelog.SourceRegistry {
			t.Errorf("entry source = %q, want %q", s, changelog.SourceRegistry)
		}
	}
	if len(uploadedSources) != 3 {
		t.Errorf("uploaded %d entries, want 3", len(uploadedSources))
	}
}

type fakeTagLister struct{}

func (fakeTagLister) ListTags(_ context.Context, repo string, limit int) ([]registry.Tag, error) {
	if repo != "user/app" {
		return nil, nil
	}
	return []registry.Tag{
		{Name: "latest", LastUpdated: "2026-08-01T00:00:00Z"},
		{Name: "1.28.0", LastUpdated: "2026-07-01T00:00:00Z"},
		{Name: "1.27.3", LastUpdated: "2026-06-01T00:00:00Z"},
	}, nil
}

func TestRunCreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-data")
	// Pre-place an enroll token file so the daemon's first-start
	// check passes its IO sanity step; the network call is
	// short-circuited by the short context deadline.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tok := filepath.Join(dir, "token")
	if err := os.WriteFile(tok, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := config.Agent{
		Common:          config.Common{Mode: config.ModeAgent},
		Name:            "x",
		ControllerURL:   "https://127.0.0.1:1",
		DockerHost:      "tcp://127.0.0.1:1",
		DataDir:         dir,
		EnrollTokenFile: tok,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = Run(ctx, cfg) // expected to fail on enroll; we only check the data dir was created
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected data dir to exist: %v", err)
	}
}
