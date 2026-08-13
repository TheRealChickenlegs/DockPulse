package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

func withServerID(serverID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/agent/v1/updates/report", nil)
	return r.WithContext(AttachServerID(context.Background(), serverID))
}

func TestUpdatesReportRoundTrip(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")
	agentID, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())
	_ = agentID

	body, _ := json.Marshal(UpdatesReportRequest{Updates: []UpdateReport{
		{ImageRef: "library/nginx:latest", Repo: "library/nginx", Tag: "latest", FromDigest: "sha256:local", ToDigest: "sha256:remote"},
	}})
	r := withServerID(serverID)
	r.Body = io.NopCloser(bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleUpdatesReport(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}

	// Re-reporting the same delta must not duplicate the update row.
	body2, _ := json.Marshal(UpdatesReportRequest{Updates: []UpdateReport{
		{ImageRef: "library/nginx:latest", Repo: "library/nginx", Tag: "latest", FromDigest: "sha256:local", ToDigest: "sha256:remote"},
	}})
	r2 := withServerID(serverID)
	r2.Body = io.NopCloser(bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	s.HandleUpdatesReport(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("re-report: %d %s", w2.Code, w2.Body.String())
	}

	var count int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM updates`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("updates rows = %d, want 1", count)
	}

	var toDigest string
	if err := d.QueryRowContext(context.Background(), `SELECT to_digest FROM updates`).Scan(&toDigest); err != nil {
		t.Fatal(err)
	}
	if toDigest != "sha256:remote" {
		t.Errorf("to_digest = %q, want sha256:remote", toDigest)
	}

	var images int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM images`).Scan(&images); err != nil {
		t.Fatal(err)
	}
	if images != 1 {
		t.Errorf("images rows = %d, want 1", images)
	}
}

func TestChangelogUploadRoundTrip(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")
	_, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())

	body, _ := json.Marshal(ChangelogUploadRequest{
		ImageRef: "library/nginx:latest",
		Entries: []ChangelogEntry{
			{Version: "1.27.0", Source: "github", Title: "1.27.0", URL: "https://github.com/nginx/nginx/releases/tag/1.27.0", Body: "notes", PublishedAt: "2026-01-01T00:00:00Z"},
			{Version: "1.27.0", Source: "github", Title: "1.27.0", URL: "https://github.com/nginx/nginx/releases/tag/1.27.0", Body: "notes"},
		},
	})
	r := withServerID(serverID)
	r.Body = io.NopCloser(bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.HandleChangelogUpload(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Inserted int `json:"inserted"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Inserted != 1 {
		t.Errorf("inserted = %d, want 1 (duplicate version+url deduped)", resp.Inserted)
	}
}

func TestListUpdatesIncludesChangelog(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")
	_, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())

	reportBody, _ := json.Marshal(UpdatesReportRequest{Updates: []UpdateReport{
		{ImageRef: "library/nginx:latest", Repo: "library/nginx", Tag: "latest", FromDigest: "sha256:local", ToDigest: "sha256:remote"},
	}})
	rr := withServerID(serverID)
	rr.Body = io.NopCloser(bytes.NewReader(reportBody))
	s.HandleUpdatesReport(httptest.NewRecorder(), rr)

	// Attach a container so container_count and the per-container
	// changelog endpoint resolve.
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO containers(id, server_id, docker_id, name, image_ref, image_digest_local, state, labels_json, ports_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'running', '{}', '[]', ?)
	`, auth.RandomToken(16), serverID, "abc", "web", "library/nginx:latest", "sha256:local", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	uploadBody, _ := json.Marshal(ChangelogUploadRequest{
		ImageRef: "library/nginx:latest",
		Entries:  []ChangelogEntry{{Version: "1.27.0", Source: "github", Title: "1.27.0", URL: "https://github.com/nginx/nginx/releases/tag/1.27.0"}},
	})
	ur := withServerID(serverID)
	ur.Body = io.NopCloser(bytes.NewReader(uploadBody))
	s.HandleChangelogUpload(httptest.NewRecorder(), ur)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil)
	lw := httptest.NewRecorder()
	HandleListUpdates(context.Background(), d)(lw, listReq)
	if lw.Code != http.StatusOK {
		t.Fatalf("list updates: %d %s", lw.Code, lw.Body.String())
	}
	var resp struct {
		Updates []UpdateListItem `json:"updates"`
	}
	if err := json.NewDecoder(lw.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(resp.Updates))
	}
	u := resp.Updates[0]
	if u.ServerName != "server-a" || u.ContainerCount != 1 {
		t.Errorf("got %+v, want server-a with 1 container", u)
	}
	if len(u.Changelog) != 1 || u.Changelog[0].Version != "1.27.0" {
		t.Errorf("changelog = %+v, want 1.27.0", u.Changelog)
	}
}

func TestContainerChangelog(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")
	_, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())

	containerID := auth.RandomToken(16)
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO containers(id, server_id, docker_id, name, image_ref, state, labels_json, ports_json, updated_at)
		VALUES (?, ?, 'abc', 'web', 'library/nginx:latest', 'running', '{}', '[]', '2026-01-01T00:00:00Z')
	`, containerID, serverID); err != nil {
		t.Fatal(err)
	}

	uploadBody, _ := json.Marshal(ChangelogUploadRequest{
		ImageRef: "library/nginx:latest",
		Entries:  []ChangelogEntry{{Version: "1.27.0", Source: "github", Title: "1.27.0", URL: "https://github.com/nginx/nginx/releases/tag/1.27.0"}},
	})
	ur := withServerID(serverID)
	ur.Body = io.NopCloser(bytes.NewReader(uploadBody))
	s.HandleChangelogUpload(httptest.NewRecorder(), ur)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+containerID+"/changelog", nil)
	req.SetPathValue("id", containerID)
	w := httptest.NewRecorder()
	HandleContainerChangelog(context.Background(), d)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("container changelog: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		ImageRef string            `json:"image_ref"`
		Entries  []ChangelogEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ImageRef != "library/nginx:latest" {
		t.Errorf("image_ref = %q", resp.ImageRef)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("entries = %d, want 1", len(resp.Entries))
	}
}

func TestSplitRepoTag(t *testing.T) {
	for ref, want := range map[string][2]string{
		"library/nginx:latest": {"library/nginx", "latest"},
		"user/app:v1":          {"user/app", "v1"},
		"ghcr.io/user/app:v1":  {"ghcr.io/user/app", "v1"},
	} {
		repo, tag, ok := splitRepoTag(ref)
		if !ok || repo != want[0] || tag != want[1] {
			t.Errorf("splitRepoTag(%q) = (%q, %q, %v), want (%q, %q)", ref, repo, tag, ok, want[0], want[1])
		}
	}
	for _, ref := range []string{"", "nginx", "nginx:", ":latest"} {
		if _, _, ok := splitRepoTag(ref); ok {
			t.Errorf("splitRepoTag(%q) unexpectedly ok", ref)
		}
	}
}
