-- +goose Up
-- +archive
UPDATE torrent_contents SET content_type = 'porn' WHERE content_type = 'xxx';
-- +archive
UPDATE torrent_hints SET content_type = 'porn' WHERE content_type = 'xxx';
-- +archive
UPDATE torrent_contents SET content_type = 'book' WHERE content_type IN ('ebook', 'comic');
-- +archive
UPDATE torrent_hints SET content_type = 'book' WHERE content_type IN ('ebook', 'comic');

-- +goose Down
-- +archive
UPDATE torrent_contents SET content_type = 'xxx' WHERE content_type = 'porn';
-- +archive
UPDATE torrent_hints SET content_type = 'xxx' WHERE content_type = 'porn';
-- +archive
UPDATE torrent_contents SET content_type = 'ebook' WHERE content_type = 'book';
-- +archive
UPDATE torrent_hints SET content_type = 'ebook' WHERE content_type = 'book';
