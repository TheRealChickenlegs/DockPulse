-- 0003_changelog_registry_source.sql -- Phase 2: registry tag-list history
--
-- The agent now uploads release-history entries synthesized from a
-- registry's tag list (source 'registry') for images that carry no
-- changelog source label. The changelog_entries.source CHECK constraint
-- did not allow that value, so the table is recreated with the wider
-- set. SQLite cannot alter a CHECK constraint in place.

CREATE TABLE changelog_entries_new (
    id TEXT PRIMARY KEY,
    image_id TEXT NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('oci_label', 'github', 'gitlab', 'scrape', 'manual', 'registry')),
    version TEXT NOT NULL,
    title TEXT,
    url TEXT,
    body TEXT,
    published_at TEXT,
    hash TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    UNIQUE (image_id, version, hash)
);

INSERT INTO changelog_entries_new (id, image_id, source, version, title, url, body, published_at, hash, fetched_at)
    SELECT id, image_id, source, version, title, url, body, published_at, hash, fetched_at
    FROM changelog_entries;

DROP TABLE changelog_entries;

ALTER TABLE changelog_entries_new RENAME TO changelog_entries;

CREATE INDEX idx_changelog_image ON changelog_entries(image_id);
CREATE INDEX idx_changelog_published ON changelog_entries(published_at);
