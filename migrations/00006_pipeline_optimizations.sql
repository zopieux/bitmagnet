-- +goose Up
CREATE TABLE IF NOT EXISTS blocked_hash_chunks (
    id INTEGER PRIMARY KEY,
    hashes BLOB NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torrents_missing_summary
ON torrents (info_hash)
WHERE files_status = 'over_threshold' AND files_summary IS NULL;

DELETE FROM queue_jobs WHERE status IN ('processed', 'failed');

-- Drop torrent_contents_au while clearing legacy tsv so it does not fire FTS5
-- updates on every row (which would also duplicate postings in the external-content FTS5 index).
DROP TRIGGER IF EXISTS torrent_contents_au;
UPDATE torrent_contents SET tsv = NULL WHERE tsv IS NOT NULL;
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
DROP INDEX IF EXISTS idx_torrents_missing_summary;
DROP TABLE IF EXISTS blocked_hash_chunks;
