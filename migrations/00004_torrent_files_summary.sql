-- +goose Up
-- +archive
ALTER TABLE torrents ADD COLUMN files_summary BLOB;

-- +goose Down
ALTER TABLE torrents DROP COLUMN files_summary;
