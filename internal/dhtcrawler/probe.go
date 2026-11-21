package dhtcrawler

import (
	"context"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

const probeInterval = time.Hour
const probeLimit = 100

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

	query := c.dao.Torrent.WithContext(ctx).UnderlyingDB().
		Table("torrents").
		Where("files_status = 'over_threshold' AND files_summary IS NULL").
		Order("info_hash ASC").
		Limit(probeLimit)

	if c.probeCursor != (protocol.ID{}) {
		query = query.Where("info_hash > ?", c.probeCursor[:])
	}

	if err := query.Pluck("info_hash", &candidates).Error; err != nil {
		c.probeCursor = protocol.ID{}
		return
	}

	if len(candidates) == 0 && c.probeCursor != (protocol.ID{}) {
		c.probeCursor = protocol.ID{}
		if err := c.dao.Torrent.WithContext(ctx).UnderlyingDB().
			Table("torrents").
			Where("files_status = 'over_threshold' AND files_summary IS NULL").
			Order("info_hash ASC").
			Limit(probeLimit).
			Pluck("info_hash", &candidates).Error; err != nil {
			return
		}
	}

	if len(candidates) < probeLimit {
		c.probeCursor = protocol.ID{}
	} else {
		c.probeCursor = candidates[len(candidates)-1]
	}

	if len(candidates) == 0 {
		return
	}

	filtered, err := c.blockingManager.Filter(ctx, candidates)
	if err != nil {
		c.logger.Errorf("failed to filter probe candidates: %s", err.Error())
		return
	}

	if len(filtered) == 0 {
		return
	}

	c.logger.Debugf("probing %d over-threshold torrents for missing summaries via DHT peers", len(filtered))

	for _, h := range filtered {
		nodes := c.kTable.GetClosestNodes(h)
		if len(nodes) == 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case c.getPeers.In() <- nodeHasPeersForHash{
			infoHash:  h,
			node:      nodes[0].Addr(),
			fromProbe: true,
		}:
		}
	}
}
