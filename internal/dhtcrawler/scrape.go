package dhtcrawler

import (
	"context"
	"fmt"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
)

func (c *crawler) runScrapeNew(ctx context.Context) {
	_ = c.scrapeNew.Run(ctx, func(item pendingTorrent) {
		if item.node.IsValid() && len(item.torrent.Sources) > 0 {
			if res, err := c.requestScrape(ctx, item.nodeHasPeersForHash); err == nil {
				item.torrent.Sources[0].Seeders, item.torrent.Sources[0].Leechers = res.seedersLeechers()
			}
		}
		c.storageFlusher.EnqueueTorrent(item)
	})
}

func (c *crawler) runRescrape(ctx context.Context) {
	_ = c.rescrape.Run(ctx, func(req nodeHasPeersForHash) {
		res, err := c.requestScrape(ctx, req)
		if err != nil {
			return
		}
		seeders, leechers := res.seedersLeechers()
		c.storageFlusher.EnqueueRescrape(rescrapeUpdate{
			infoHash: req.infoHash,
			seeders:  seeders,
			leechers: leechers,
			at:       time.Now(),
		})
	})
}

// requestScrape requests a scrape from a node to find seeders/leechers for a given info hash;
// see https://www.bittorrent.org/beps/bep_0033.html
func (c *crawler) requestScrape(
	ctx context.Context,
	req nodeHasPeersForHash,
) (infoHashWithScrape, error) {
	res, err := c.client.GetPeersScrape(ctx, req.node, req.infoHash)
	if err != nil {
		c.kTable.BatchCommand(ktable.DropAddr{
			Addr:   req.node.Addr(),
			Reason: fmt.Errorf("failed to get peers from p: %w", err),
		})

		return infoHashWithScrape{}, err
	}

	c.kTable.BatchCommand(ktable.PutNode{
		ID:      res.ID,
		Addr:    req.node,
		Options: []ktable.NodeOption{ktable.NodeResponded()},
	})

	if len(res.Nodes) > 0 {
		cancelCtx, cancel := context.WithTimeout(ctx, time.Second)

		for _, n := range res.Nodes {
			select {
			case <-cancelCtx.Done():
				break
			case c.discoveredNodes.In() <- ktable.NewNode(n.ID, n.Addr):
				continue
			}
		}

		cancel()
	}

	return infoHashWithScrape{
		nodeHasPeersForHash: req,
		bfsd:                res.BfSeeders,
		bfpe:                res.BfPeers,
	}, nil
}
