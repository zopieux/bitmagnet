package dhtcrawler

import (
	"context"
	"net/netip"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

// runInfoHashTriage receives discovered hashes on the infoHashTriage channel,
// filters blocked hashes and already-known hashes purely in memory, and forwards
// unknown hashes to requestMetaInfo/getPeers or stale-scrape hashes to rescrape.
func (c *crawler) runInfoHashTriage(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case reqs := <-c.infoHashTriage.Out():
			allHashes := make([]protocol.ID, 0, len(reqs))

			reqMap := make(map[protocol.ID]nodeHasPeersForHash, len(reqs))
			for _, r := range reqs {
				if existing, ok := reqMap[r.infoHash]; ok {
					// Prefer an entry with a direct peer endpoint (from announce_peer) over sample_infohashes.
					if !existing.peer.IsValid() && r.peer.IsValid() {
						reqMap[r.infoHash] = r
					}
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
					if r.peer.IsValid() {
						select {
						case <-ctx.Done():
							return
						case c.requestMetaInfo.In() <- infoHashWithPeers{
							nodeHasPeersForHash: r,
							peers:               []netip.AddrPort{r.peer},
						}:
							c.statsNew.Add(1)
						}
					} else if r.node.IsValid() {
						select {
						case <-ctx.Done():
							return
						case c.getPeers.In() <- r:
							c.statsNew.Add(1)
						}
					}
				} else if r.node.IsValid() && c.rescrapeThreshold > 0 && (lastDay == 0 || lastDay < cutoffDay) {
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
