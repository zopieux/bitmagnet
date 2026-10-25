package dhtcrawler

import (
	"context"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

const backfillBatchSize = 2000

func (c *crawler) backfillMultiFileSummaries(ctx context.Context) {
	c.logger.Infof("checking for multi-file torrents missing file summaries...")

	var cursor protocol.ID
	totalUpdated := 0
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var hashes []protocol.ID
		query := c.dao.Torrent.WithContext(ctx).UnderlyingDB().
			Table("torrents").
			Select("info_hash").
			Where("files_status = ? AND files_summary IS NULL", model.FilesStatusMulti).
			Order("info_hash ASC").
			Limit(backfillBatchSize)

		if cursor != (protocol.ID{}) {
			query = query.Where("info_hash > ?", cursor[:])
		}

		if err := query.Pluck("info_hash", &hashes).Error; err != nil {
			c.logger.Errorf("backfill query failed: %s", err)
			return
		}

		if len(hashes) == 0 {
			if totalUpdated > 0 {
				c.logger.Infof("backfill completed: %d multi-file torrents summarized in %s", totalUpdated, time.Since(startTime).Round(time.Second))
			}
			return
		}

		cursor = hashes[len(hashes)-1]

		var files []model.TorrentFile
		valuers := make([]any, len(hashes))
		for i, h := range hashes {
			valuers[i] = h[:]
		}

		if err := c.dao.TorrentFile.WithContext(ctx).UnderlyingDB().
			Table("torrent_files").
			Select("info_hash, path, size").
			Where("info_hash IN (?)", valuers).
			Order(`"index" ASC`).
			Find(&files).Error; err != nil {
			c.logger.Errorf("backfill failed querying torrent_files: %s", err)
			return
		}

		filesByHash := make(map[protocol.ID][]model.FileEntry, len(hashes))
		for _, f := range files {
			filesByHash[f.InfoHash] = append(filesByHash[f.InfoHash], model.FileEntry{
				Path: f.Path,
				Size: uint64(f.Size),
			})
		}

		txErr := c.dao.Transaction(func(tx *dao.Query) error {
			for _, h := range hashes {
				summary := model.SummarizeFileEntries(filesByHash[h])
				encoded, err := summary.Value()
				if err != nil {
					continue
				}
				if err := tx.Torrent.WithContext(ctx).UnderlyingDB().
					Table("torrents").
					Where("info_hash = ?", h[:]).
					Update("files_summary", encoded).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if txErr != nil {
			c.logger.Errorf("backfill failed updating torrents: %s", txErr)
			return
		}

		totalUpdated += len(hashes)
		c.logger.Infof("backfilled %d multi-file torrent summaries...", totalUpdated)

		if len(hashes) < backfillBatchSize {
			c.logger.Infof("backfill completed: %d multi-file torrents summarized in %s", totalUpdated, time.Since(startTime).Round(time.Second))
			return
		}
	}
}
