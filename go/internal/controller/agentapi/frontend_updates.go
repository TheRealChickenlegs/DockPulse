package agentapi

import (
	"context"
	"database/sql"
	"net/http"
)

// UpdateListItem is the JSON shape of GET /api/v1/updates.
type UpdateListItem struct {
	ID             string           `json:"id"`
	ImageRef       string           `json:"image_ref"`
	Repo           string           `json:"repo"`
	Tag            string           `json:"tag"`
	FromDigest     string           `json:"from_digest"`
	ToDigest       string           `json:"to_digest"`
	Status         string           `json:"status"`
	CreatedAt      string           `json:"created_at"`
	SeenAt         string           `json:"seen_at"`
	ServerID       string           `json:"server_id"`
	ServerName     string           `json:"server_name"`
	ContainerCount int              `json:"container_count"`
	Changelog      []ChangelogEntry `json:"changelog"`
}

// HandleListUpdates returns every detected update across all servers,
// newest first, with the image's changelog attached.
func HandleListUpdates(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		reqCtx := r.Context()
		rows, err := db.QueryContext(reqCtx, `
			SELECT u.id, i.repo, i.tag, u.from_digest, u.to_digest, u.status,
			       u.created_at, u.seen_at, u.server_id, s.name,
			       (SELECT COUNT(*) FROM containers c
			        WHERE c.server_id = u.server_id
			          AND c.image_ref = i.repo || ':' || i.tag)
			FROM updates u
			JOIN images i ON i.id = u.image_id
			JOIN servers s ON s.id = u.server_id
			ORDER BY u.seen_at DESC
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type row struct {
			item UpdateListItem
			repo string
			tag  string
		}
		var collected []row
		for rows.Next() {
			var u UpdateListItem
			if err := rows.Scan(&u.ID, &u.Repo, &u.Tag, &u.FromDigest, &u.ToDigest,
				&u.Status, &u.CreatedAt, &u.SeenAt, &u.ServerID, &u.ServerName,
				&u.ContainerCount); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			u.ImageRef = u.Repo + ":" + u.Tag
			collected = append(collected, row{item: u, repo: u.Repo, tag: u.Tag})
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// The cursor must be closed before the per-image changelog
		// queries run: the pool is capped at one connection (WAL
		// single-writer), so a second query while this cursor is
		// open would deadlock.
		rows.Close()

		out := make([]UpdateListItem, 0, len(collected))
		for _, r := range collected {
			r.item.Changelog = listChangelog(reqCtx, db, r.repo, r.tag, 5)
			out = append(out, r.item)
		}
		if out == nil {
			out = []UpdateListItem{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"updates": out})
	}
}

// HandleContainerChangelog returns the changelog entries for the
// image the container is running.
func HandleContainerChangelog(ctx context.Context, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		containerID := r.PathValue("id")
		if containerID == "" {
			http.Error(w, "container id required", http.StatusBadRequest)
			return
		}
		var imageRef string
		if err := db.QueryRowContext(r.Context(),
			`SELECT image_ref FROM containers WHERE id = ?`, containerID).Scan(&imageRef); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"image_ref": "", "entries": []ChangelogEntry{}})
			return
		}
		repo, tag, ok := splitRepoTag(imageRef)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"image_ref": imageRef, "entries": []ChangelogEntry{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"image_ref": imageRef,
			"entries":   listChangelog(r.Context(), db, repo, tag, 20),
		})
	}
}

func listChangelog(ctx context.Context, db *sql.DB, repo, tag string, limit int) []ChangelogEntry {
	rows, err := db.QueryContext(ctx, `
		SELECT e.version, e.title, e.url, e.published_at, e.source
		FROM changelog_entries e
		JOIN images i ON i.id = e.image_id
		WHERE i.repo = ? AND i.tag = ?
		ORDER BY COALESCE(e.published_at, e.fetched_at) DESC
		LIMIT ?
	`, repo, tag, limit)
	if err != nil {
		return []ChangelogEntry{}
	}
	defer rows.Close()

	var out []ChangelogEntry
	for rows.Next() {
		var e ChangelogEntry
		var title, url, published sql.NullString
		if err := rows.Scan(&e.Version, &title, &url, &published, &e.Source); err != nil {
			continue
		}
		if title.Valid {
			e.Title = title.String
		}
		if url.Valid {
			e.URL = url.String
		}
		if published.Valid {
			e.PublishedAt = published.String
		}
		out = append(out, e)
	}
	return out
}
