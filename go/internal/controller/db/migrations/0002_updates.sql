-- 0002_updates.sql -- Phase 2: update detection and changelog aggregation
--
-- Extends the Phase 1 schema with the tables the agent's registry
-- polling writes to. The remote digest for every unique (repo, tag)
-- is cached in `images` so updates are detected by comparing the
-- local digest to the last-known remote digest, not by re-querying
-- the registry for every snapshot.

-- A unique (repo, tag) pair, e.g. ("library/nginx", "latest") or
-- ("ghcr.io/user/app", "1.0.0"). repo is the fully-qualified
-- repository including the registry host when it is not Docker Hub.
CREATE TABLE images (
    id TEXT PRIMARY KEY,
    repo TEXT NOT NULL,
    tag TEXT NOT NULL,
    remote_digest TEXT,
    last_checked_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (repo, tag)
);
CREATE INDEX idx_images_repo ON images(repo, tag);

-- One row per (image, server) so each host reports independently.
-- from_digest is the local digest observed when the update was first
-- detected; to_digest is the latest remote digest. Re-detecting the
-- same delta refreshes seen_at instead of creating a new row.
CREATE TABLE updates (
    id TEXT PRIMARY KEY,
    image_id TEXT NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    from_digest TEXT NOT NULL,
    to_digest TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ignored', 'applied')),
    created_at TEXT NOT NULL,
    seen_at TEXT NOT NULL,
    UNIQUE (image_id, server_id)
);
CREATE INDEX idx_updates_status ON updates(status);
CREATE INDEX idx_updates_server ON updates(server_id);

-- Aggregated changelog entries per image. Deduplicated by
-- (image_id, version, hash) so the same release note is never stored
-- twice even if multiple sources report it or multiple servers see it.
CREATE TABLE changelog_entries (
    id TEXT PRIMARY KEY,
    image_id TEXT NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('oci_label', 'github', 'gitlab', 'scrape', 'manual')),
    version TEXT NOT NULL,
    title TEXT,
    url TEXT,
    body TEXT,
    published_at TEXT,
    hash TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    UNIQUE (image_id, version, hash)
);
CREATE INDEX idx_changelog_image ON changelog_entries(image_id);
CREATE INDEX idx_changelog_published ON changelog_entries(published_at);

-- Operator-pinned changelog URL per image per source. The admin UI
-- manages these in Phase 2; the agent never writes this table.
CREATE TABLE changelog_links (
    id TEXT PRIMARY KEY,
    image_id TEXT NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('github', 'gitlab', 'scrape', 'manual')),
    url TEXT NOT NULL,
    UNIQUE (image_id, source)
);
