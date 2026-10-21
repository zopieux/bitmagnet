-- +goose Up
-- The torrent listing sorts by the live per-source seed/leech maxima via a
-- correlated MAX() subquery over torrents_torrent_sources. The table's
-- primary key is (source, info_hash), so without an index leading with
-- info_hash every evaluation is a full scan: O(torrents x sources), minutes
-- at 50k torrents. These indexes turn each MAX into a range probe on the
-- hash's own rows (20ms at 53k torrents, 75ms at 214k, measured).
CREATE INDEX idx_torrents_torrent_sources_info_hash_seeders ON torrents_torrent_sources (info_hash, seeders);
CREATE INDEX idx_torrents_torrent_sources_info_hash_leechers ON torrents_torrent_sources (info_hash, leechers);

-- +goose Down
DROP INDEX IF EXISTS idx_torrents_torrent_sources_info_hash_seeders;
DROP INDEX IF EXISTS idx_torrents_torrent_sources_info_hash_leechers;
