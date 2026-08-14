package agentapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

// UpdatesReportRequest is the JSON body of POST /agent/v1/updates/report.
type UpdatesReportRequest struct {
	Updates []UpdateReport `json:"updates"`
}

// UpdateReport is a single detected digest delta.
type UpdateReport struct {
	ImageRef   string `json:"image_ref"`
	Repo       string `json:"repo"`
	Tag        string `json:"tag"`
	FromDigest string `json:"from_digest"`
	ToDigest   string `json:"to_digest"`
}

// HandleUpdatesReport records the agent's detected digest deltas.
// Images are upserted by (repo, tag) with the latest remote digest;
// the update row is upserted per (image, server) so re-reporting an
// unchanged delta only refreshes seen_at.
func (s *Server) HandleUpdatesReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := ServerIDFromContext(r.Context())
	if serverID == "" {
		http.Error(w, "missing server context", http.StatusInternalServerError)
		return
	}
	var req UpdatesReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req.Updates) > 200 {
		http.Error(w, "too many updates", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	inserted := 0
	for _, u := range req.Updates {
		repo := strings.TrimSpace(u.Repo)
		tag := strings.TrimSpace(u.Tag)
		if repo == "" || tag == "" || u.FromDigest == "" || u.ToDigest == "" {
			continue
		}
		imageID, err := upsertImage(r.Context(), tx, repo, tag, u.ToDigest, now)
		if err != nil {
			s.Log.Error("upsert image", "err", err, "repo", repo, "tag", tag)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := upsertUpdate(r.Context(), tx, imageID, serverID, u.FromDigest, u.ToDigest, now); err != nil {
			s.Log.Error("upsert update", "err", err, "repo", repo, "tag", tag)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		s.Log.Error("updates report commit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inserted > 0 {
		if _, err := s.DB.ExecContext(r.Context(), `
			INSERT INTO audit_log(actor_kind, actor_id, action, target, details_json)
			VALUES ('agent', ?, 'updates.report', ?, ?)
		`, serverID, serverID, fmt.Sprintf(`{"count":%d}`, inserted)); err != nil {
			s.Log.Warn("audit log", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": inserted})
}

// ChangelogUploadRequest is the JSON body of POST /agent/v1/changelog/upload.
type ChangelogUploadRequest struct {
	ImageRef string            `json:"image_ref"`
	Entries  []ChangelogEntry `json:"entries"`
}

// ChangelogEntry is a single release note for an image.
type ChangelogEntry struct {
	Version     string `json:"version"`
	Source      string `json:"source"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	Body        string `json:"body,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Hash        string `json:"hash"`
}

var changelogSources = map[string]bool{
	"oci_label": true, "github": true, "gitlab": true, "scrape": true, "manual": true, "registry": true,
}

// HandleChangelogUpload attaches release notes to an image. Entries
// are deduplicated by (image_id, version, hash); re-uploading the
// same release is a no-op.
func (s *Server) HandleChangelogUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := ServerIDFromContext(r.Context())
	if serverID == "" {
		http.Error(w, "missing server context", http.StatusInternalServerError)
		return
	}
	var req ChangelogUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	repo, tag, ok := splitRepoTag(req.ImageRef)
	if !ok || len(req.Entries) > 200 {
		http.Error(w, "invalid image_ref or too many entries", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// The image row may not exist if the update report was never
	// received; create it without a remote digest so the changelog
	// is still attached.
	imageID, err := upsertImage(r.Context(), tx, repo, tag, "", "")
	if err != nil {
		s.Log.Error("upsert image", "err", err, "repo", repo, "tag", tag)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	inserted := 0
	for _, e := range req.Entries {
		version := strings.TrimSpace(e.Version)
		source := strings.TrimSpace(e.Source)
		if version == "" || !changelogSources[source] {
			continue
		}
		hash := e.Hash
		if hash == "" {
			hash = changelogHash(version, e.URL)
		}
		res, err := tx.ExecContext(r.Context(), `
			INSERT INTO changelog_entries(id, image_id, source, version, title, url, body, published_at, hash, fetched_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(image_id, version, hash) DO NOTHING
		`,
			auth.RandomToken(16), imageID, source, version,
			nullableString(e.Title), nullableString(e.URL), nullableString(e.Body),
			nullableString(e.PublishedAt), hash, now)
		if err != nil {
			s.Log.Error("insert changelog entry", "err", err, "repo", repo, "tag", tag, "version", version)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		s.Log.Error("changelog upload commit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inserted > 0 {
		if _, err := s.DB.ExecContext(r.Context(), `
			INSERT INTO audit_log(actor_kind, actor_id, action, target, details_json)
			VALUES ('agent', ?, 'changelog.upload', ?, ?)
		`, serverID, req.ImageRef, fmt.Sprintf(`{"inserted":%d}`, inserted)); err != nil {
			s.Log.Warn("audit log", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "inserted": inserted})
}

// queryer is the subset of *sql.DB / *sql.Tx used by the upsert
// helpers.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// upsertImage creates or refreshes the image row for (repo, tag) and
// returns its id. remoteDigest and checkedAt may be "" to leave the
// existing values untouched (changelog-only uploads).
func upsertImage(ctx context.Context, q queryer, repo, tag, remoteDigest, checkedAt string) (string, error) {
	if _, err := q.ExecContext(ctx, `
		INSERT INTO images(id, repo, tag, remote_digest, last_checked_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repo, tag) DO UPDATE SET
			remote_digest = COALESCE(excluded.remote_digest, images.remote_digest),
			last_checked_at = COALESCE(excluded.last_checked_at, images.last_checked_at)
	`, auth.RandomToken(16), repo, tag, nullableString(remoteDigest), nullableString(checkedAt)); err != nil {
		return "", err
	}
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM images WHERE repo = ? AND tag = ?`, repo, tag).Scan(&id)
	return id, err
}

// upsertUpdate creates or refreshes the update row for (image, server).
// The original from_digest is preserved on re-report; only to_digest
// and seen_at move forward.
func upsertUpdate(ctx context.Context, q queryer, imageID, serverID, fromDigest, toDigest, now string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO updates(id, image_id, server_id, from_digest, to_digest, created_at, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(image_id, server_id) DO UPDATE SET
			to_digest = excluded.to_digest,
			seen_at = excluded.seen_at
	`, auth.RandomToken(16), imageID, serverID, fromDigest, toDigest, now, now)
	return err
}

// splitRepoTag splits "repo:tag" into its components. The tag is the
// text after the final colon (the repo itself never contains a colon
// for the registries Phase 2 polls).
func splitRepoTag(ref string) (repo, tag string, ok bool) {
	i := strings.LastIndex(ref, ":")
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	repo, tag = ref[:i], ref[i+1:]
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(tag) == "" {
		return "", "", false
	}
	return repo, tag, true
}

// changelogHash is the controller-side fallback dedup key, matching
// the agent's changelog.Hash.
func changelogHash(version, url string) string {
	sum := sha256.Sum256([]byte(version + "|" + url))
	return hex.EncodeToString(sum[:])
}
