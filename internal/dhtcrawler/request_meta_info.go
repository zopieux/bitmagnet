package dhtcrawler

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/classify"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/metainforequester"
)

func (c *crawler) runRequestMetaInfo(ctx context.Context) {
	_ = c.requestMetaInfo.Run(ctx, func(req infoHashWithPeers) {
		mi, reqErr := c.doRequestMetaInfo(ctx, req.infoHash, req.peers, req.fromProbe)
		if reqErr != nil {
			c.statsMetaErr.Add(1)
			return
		}

		c.statsMetaOk.Add(1)

		t, err := createTorrentModel(req.infoHash, mi.Info, c.savePieces, c.saveFilesThreshold)
		if err != nil {
			c.logger.Errorf("error creating torrent model: %s", err.Error())
			return
		}

		switch c.classifyConfig.ActionFor(t) {
		case classify.ActionDelete:
			if req.fromProbe {
				_ = c.blockingManager.Block(ctx, []protocol.ID{req.infoHash}, false)
			} else {
				c.blockingManager.BlockUnpersisted([]protocol.ID{req.infoHash})
			}
			c.statsBlocked.Add(1)
		case classify.ActionArchive:
			c.knownHashes.MarkKnown(req.infoHash, encodeDay(time.Now()))
			c.storageFlusher.EnqueueTorrent(pendingTorrent{
				nodeHasPeersForHash: req.nodeHasPeersForHash,
				torrent:             t,
				archiveAfter:        true,
			})
		default:
			c.knownHashes.MarkKnown(req.infoHash, encodeDay(time.Now()))
			select {
			case <-ctx.Done():
			case c.scrapeNew.In() <- pendingTorrent{
				nodeHasPeersForHash: req.nodeHasPeersForHash,
				torrent:             t,
			}:
			}
		}
	})
}

func (c *crawler) doRequestMetaInfo(
	ctx context.Context,
	hash protocol.ID,
	peers []netip.AddrPort,
	fromProbe bool,
) (metainforequester.Response, error) {
	var errs []error

	errsMutex := sync.Mutex{}
	addErr := func(err error) {
		errsMutex.Lock()
		errs = append(errs, err)
		errsMutex.Unlock()
	}

	for _, p := range peers {
		res, err := c.metainfoRequester.Request(ctx, hash, p)
		if err != nil {
			addErr(err)
			continue
		}

		if banErr := c.banningChecker.Check(res.Info); banErr != nil {
			if fromProbe {
				_ = c.blockingManager.Block(ctx, []protocol.ID{hash}, false)
			} else {
				c.blockingManager.BlockUnpersisted([]protocol.ID{hash})
			}
			return metainforequester.Response{}, banErr
		}

		return res, nil
	}

	return metainforequester.Response{}, errors.Join(errs...)
}
