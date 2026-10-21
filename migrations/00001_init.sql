-- +goose Up
-- SQLite initial schema for bitmagnet. Column layout mirrors the gorm models
-- in internal/model; the DAO in internal/database/dao is generated from them.

CREATE TABLE IF NOT EXISTS torrents (
    info_hash BLOB NOT NULL PRIMARY KEY,
    name TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    private INTEGER NOT NULL DEFAULT 0,
    files_status TEXT NOT NULL DEFAULT 'no_info',
    extension TEXT,
    files_count INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS torrent_files (
    info_hash BLOB NOT NULL,
    "index" INTEGER NOT NULL,
    path TEXT NOT NULL,
    extension TEXT,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (info_hash, path),
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS torrent_sources (
    key TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS torrents_torrent_sources (
    source TEXT NOT NULL,
    info_hash BLOB NOT NULL,
    import_id TEXT,
    seeders INTEGER,
    leechers INTEGER,
    published_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source, info_hash),
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS torrent_pieces (
    info_hash BLOB NOT NULL PRIMARY KEY,
    piece_length INTEGER NOT NULL,
    pieces BLOB NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS torrent_contents (
    id TEXT NOT NULL PRIMARY KEY,
    info_hash BLOB NOT NULL,
    content_type TEXT,
    content_source TEXT,
    content_id TEXT,
    languages TEXT,
    episodes TEXT,
    video_resolution TEXT,
    video_source TEXT,
    video_codec TEXT,
    video_3d TEXT,
    video_modifier TEXT,
    release_group TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tsv TEXT,
    seeders INTEGER,
    leechers INTEGER,
    published_at DATETIME,
    size INTEGER NOT NULL DEFAULT 0,
    files_count INTEGER,
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_torrent_contents_info_hash ON torrent_contents (info_hash);
CREATE INDEX IF NOT EXISTS idx_torrent_contents_type ON torrent_contents (content_type);
CREATE INDEX IF NOT EXISTS idx_torrent_contents_content ON torrent_contents (content_source, content_id);

CREATE TABLE IF NOT EXISTS torrent_tags (
    info_hash BLOB NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (info_hash, name),
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_torrent_tags_name ON torrent_tags (name);

CREATE TABLE IF NOT EXISTS torrent_hints (
    info_hash BLOB NOT NULL PRIMARY KEY,
    content_type TEXT NOT NULL,
    content_source TEXT,
    content_id TEXT,
    title TEXT,
    release_year INTEGER,
    languages TEXT,
    episodes TEXT,
    video_resolution TEXT,
    video_source TEXT,
    video_codec TEXT,
    video_3d TEXT,
    video_modifier TEXT,
    release_group TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (info_hash) REFERENCES torrents (info_hash) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS queue_jobs (
    id TEXT NOT NULL PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    queue TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    payload TEXT NOT NULL,
    retries INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 0,
    run_after DATETIME NOT NULL,
    ran_at DATETIME,
    error TEXT,
    deadline DATETIME,
    archival_duration INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    priority INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_queue_jobs_queue_status ON queue_jobs (queue, status, run_after);

CREATE TABLE IF NOT EXISTS key_values (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bloom_filters (
    key TEXT NOT NULL PRIMARY KEY,
    data BLOB,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Full text search with FTS5 over the torrent name. The triggers keep the
-- index in sync; rowid maps to torrent_contents.rowid (the id column is
-- TEXT, not usable as rowid).
CREATE VIRTUAL TABLE IF NOT EXISTS torrent_contents_fts USING fts5(
    name,
    content='torrent_contents',
    content_rowid='rowid'
);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS torrent_contents_ai AFTER INSERT ON torrent_contents BEGIN
    INSERT INTO torrent_contents_fts(rowid, name)
    SELECT new.rowid, t.name
    FROM torrents t WHERE t.info_hash = new.info_hash;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS torrent_contents_ad AFTER DELETE ON torrent_contents BEGIN
    INSERT INTO torrent_contents_fts(torrent_contents_fts, rowid, name)
    VALUES('delete', old.rowid, '');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS torrent_contents_au AFTER UPDATE ON torrent_contents BEGIN
    INSERT INTO torrent_contents_fts(torrent_contents_fts, rowid, name)
    VALUES('delete', old.rowid, '');
    INSERT INTO torrent_contents_fts(rowid, name)
    SELECT new.rowid, t.name
    FROM torrents t WHERE t.info_hash = new.info_hash;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS torrent_contents_au;
DROP TRIGGER IF EXISTS torrent_contents_ad;
DROP TRIGGER IF EXISTS torrent_contents_ai;
DROP TABLE IF EXISTS torrent_contents_fts;
DROP TABLE IF EXISTS bloom_filters;
DROP TABLE IF EXISTS key_values;
DROP TABLE IF EXISTS queue_jobs;
DROP TABLE IF EXISTS torrent_hints;
DROP TABLE IF EXISTS torrent_tags;
DROP TABLE IF EXISTS torrent_contents;
DROP TABLE IF EXISTS torrent_pieces;
DROP TABLE IF EXISTS torrents_torrent_sources;
DROP TABLE IF EXISTS torrent_sources;
DROP TABLE IF EXISTS torrent_files;
DROP TABLE IF EXISTS torrents;
