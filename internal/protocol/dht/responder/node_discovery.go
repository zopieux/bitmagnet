package responder

import (
	"context"
	"net/netip"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
)

// responderNodeDiscovery attempts to add nodes from incoming requests to the discovered nodes channel
// and forwards incoming announce_peer hashes to the crawler.
type responderNodeDiscovery struct {
	responder       Responder
	discoveredNodes chan<- ktable.Node
	announcedPeers  chan<- dht.AnnouncedPeer
}

func (r responderNodeDiscovery) Respond(ctx context.Context, msg dht.RecvMsg) (dht.Return, error) {
	ret, err := r.responder.Respond(ctx, msg)
	if err == nil && msg.Msg.A != nil {
		if msg.Msg.Q == dht.QAnnouncePeer && r.announcedPeers != nil {
			port := msg.AnnouncePort()
			var peer netip.AddrPort
			if port > 0 && msg.From.Addr().IsValid() {
				peer = netip.AddrPortFrom(msg.From.Addr(), port)
			}
			select {
			case r.announcedPeers <- dht.AnnouncedPeer{
				InfoHash: msg.Msg.A.InfoHash,
				Node:     msg.From,
				Peer:     peer,
			}:
			default:
			}
		}

		go func() {
			// wait for up to a second
			cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			select {
			case <-cancelCtx.Done():
			case r.discoveredNodes <- ktable.NewNode(msg.Msg.A.ID, msg.From):
			}
		}()
	}

	return ret, err
}
