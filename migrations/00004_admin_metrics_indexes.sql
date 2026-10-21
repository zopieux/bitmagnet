-- +goose Up
CREATE INDEX idx_torrents_torrent_sources_datetime_updated_at ON torrents_torrent_sources (datetime(updated_at), source);
CREATE INDEX idx_queue_jobs_status_created_at ON queue_jobs (status, datetime(created_at));
CREATE INDEX idx_queue_jobs_ran_at ON queue_jobs (datetime(ran_at)) WHERE status != 'pending';

-- +goose Down
DROP INDEX IF EXISTS idx_torrents_torrent_sources_datetime_updated_at;
DROP INDEX IF EXISTS idx_queue_jobs_status_created_at;
DROP INDEX IF EXISTS idx_queue_jobs_ran_at;
