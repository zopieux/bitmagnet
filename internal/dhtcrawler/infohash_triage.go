package dhtcrawler

import (
	"context"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

// runInfoHashTriage receives discovered hashes on the infoHashTriage channel,
// filters blocked hashes and already-known hashes purely in memory, and forwards
// unknown hashes to getPeers or stale-scrape hashes to rescrape.
func (c *crawler) runInfoHashTriage(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case reqs := <-c.infoHashTriage.Out():
			allHashes := make([]protocol.ID, 0, len(reqs))

			reqMap := make(map[protocol.ID]nodeHasPeersForHash, len(reqs))
			for _, r := range reqs {
				if _, ok := reqMap[r.infoHash]; ok {
					continue
				}

				allHashes = append(allHashes, r.infoHash)
				reqMap[r.infoHash] = r
			}

			filteredHashes, filterErr := c.blockingManager.Filter(ctx, allHashes)
			if filterErr != nil {
				c.logger.Errorf("failed to filter infohashes: %s", filterErr.Error())
				continue
			}

			if blocked := len(allHashes) - len(filteredHashes); blocked > 0 {
				c.statsBlocked.Add(int64(blocked))
			}

			if len(filteredHashes) == 0 {
				continue
			}

			now := time.Now()
			today := encodeDay(now)
			var cutoffDay uint16
			if c.rescrapeThreshold > 0 {
				cutoffDay = encodeDay(now.Add(-c.rescrapeThreshold))
			}

			for _, h := range filteredHashes {
				r := reqMap[h]
				known, lastDay := c.knownHashes.Lookup(h)
				if !known {
					select {
					case <-ctx.Done():
						return
					case c.getPeers.In() <- r:
						c.statsNew.Add(1)
					}
				} else if c.rescrapeThreshold > 0 && (lastDay == 0 || lastDay < cutoffDay) {
					c.knownHashes.UpdateScrapeDay(h, today)
					select {
					case <-ctx.Done():
						return
					case c.rescrape.In() <- r:
					}
				}
			}
		}
	}
}
