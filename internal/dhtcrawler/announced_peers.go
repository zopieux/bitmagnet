package dhtcrawler

import (
	"context"
	"net/netip"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"go.uber.org/fx"
)

type AnnouncedPeersParams struct {
	fx.In
	Config Config
}

type AnnouncedPeersResult struct {
	fx.Out
	AnnouncedPeers chan dht.AnnouncedPeer `name:"dht_announced_peers"`
}

// NewAnnouncedPeers provides the bounded channel for incoming DHT announce_peer
// notifications as a separate service to avoid a circular fx dependency with the DHT server.
func NewAnnouncedPeers(params AnnouncedPeersParams) AnnouncedPeersResult {
	return AnnouncedPeersResult{
		AnnouncedPeers: make(chan dht.AnnouncedPeer, max(100, int(100*params.Config.ScalingFactor))),
	}
}

func (c *crawler) runAnnouncedPeers(ctx context.Context) {
	if c.announcedPeers == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ap := <-c.announcedPeers:
			c.statsAnnounced.Add(1)
			if c.ignoreHashes.testAndAdd(ap.InfoHash) {
				continue
			}

			// Pick a k-table node close to the infohash for BEP 33 scraping:
			// the announcing peer sits near *our* node ID, not the torrent's.
			var scrapeNode netip.AddrPort
			if c.kTable != nil {
				if nodes := c.kTable.GetClosestNodes(ap.InfoHash); len(nodes) > 0 {
					scrapeNode = nodes[0].Addr()
				}
			}

			select {
			case <-ctx.Done():
				return
			case c.infoHashTriage.In() <- nodeHasPeersForHash{
				infoHash: ap.InfoHash,
				node:     scrapeNode,
				peer:     ap.Peer,
			}:
			}
		}
	}
}
