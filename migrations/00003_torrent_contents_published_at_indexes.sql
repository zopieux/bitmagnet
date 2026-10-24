-- +goose Up
-- +archive
CREATE INDEX idx_torrent_contents_published_at ON torrent_contents (published_at DESC);
-- +archive
CREATE INDEX idx_torrent_contents_type_published_at ON torrent_contents (content_type, published_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_torrent_contents_published_at;
DROP INDEX IF EXISTS idx_torrent_contents_type_published_at;
