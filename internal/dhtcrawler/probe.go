package dhtcrawler

import (
	"context"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

const probeInterval = 15 * time.Minute
const probeLimit = 100
const probeScanBatchSize = 2000

type probeTorrentRow struct {
	InfoHash     protocol.ID           `gorm:"column:info_hash"`
	FilesStatus  model.FilesStatus     `gorm:"column:files_status"`
	FilesSummary *model.ContentSummary `gorm:"column:files_summary"`
}

func (c *crawler) runMissingSummaryProbe(ctx context.Context) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()

	// Wait 2 minutes after startup before first probe to allow DHT routing table to populate.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
		c.probeMissingSummaries(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.probeMissingSummaries(ctx)
		}
	}
}

func (c *crawler) probeMissingSummaries(ctx context.Context) {
	var candidates []protocol.ID

	for len(candidates) < probeLimit {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var rows []probeTorrentRow
		query := c.dao.Torrent.WithContext(ctx).UnderlyingDB().
			Table("torrents").
			Select("info_hash, files_status, files_summary").
			Order("info_hash ASC").
			Limit(probeScanBatchSize)

		if c.probeCursor != (protocol.ID{}) {
			query = query.Where("info_hash > ?", c.probeCursor[:])
		}

		if err := query.Find(&rows).Error; err != nil || len(rows) == 0 {
			c.probeCursor = protocol.ID{}
			break
		}

		for _, r := range rows {
			if r.FilesStatus == model.FilesStatusOverThreshold && r.FilesSummary == nil {
				candidates = append(candidates, r.InfoHash)
				if len(candidates) >= probeLimit {
					break
				}
			}
		}

		if len(rows) < probeScanBatchSize {
			c.probeCursor = protocol.ID{}
			break
		}
		c.probeCursor = rows[len(rows)-1].InfoHash
	}

	if len(candidates) == 0 {
		return
	}

	c.logger.Debugf("probing %d over-threshold torrents for missing summaries via DHT peers", len(candidates))

	for _, h := range candidates {
		nodes := c.kTable.GetClosestNodes(h)
		if len(nodes) == 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case c.infoHashTriage.In() <- nodeHasPeersForHash{
			infoHash: h,
			node:     nodes[0].Addr(),
		}:
		}
	}
}
