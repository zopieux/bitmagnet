package responder

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/anacrolix/dht/v2/krpc"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
	ktable_mocks "github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable/mocks"
	"github.com/stretchr/testify/assert"
)

type testResponderMocks struct {
	nodeID    protocol.ID
	table     *ktable_mocks.Table
	responder responder
	sender    dht.NodeInfo
}

func newTestResponderMocks(t *testing.T) testResponderMocks {
	t.Helper()

	nodeID := protocol.RandomNodeID()
	tableMock := ktable_mocks.NewTable(t)

	return testResponderMocks{
		nodeID: nodeID,
		table:  tableMock,
		responder: responder{
			nodeID:                   nodeID,
			kTable:                   tableMock,
			sampleInfoHashesInterval: 20,
		},
		sender: dht.RandomNodeInfo(4),
	}
}

var _ ktable.Node = mockedPeer{}

type mockedPeer struct {
	dht.NodeInfo
}

func (m mockedPeer) ID() protocol.ID {
	return m.NodeInfo.ID
}

func (m mockedPeer) Addr() netip.AddrPort {
	return m.NodeInfo.Addr.ToAddrPort()
}

func (mockedPeer) Time() time.Time {
	return time.Time{}
}

func (mockedPeer) Dropped() bool {
	return false
}

func (mockedPeer) IsSampleInfoHashesCandidate() bool {
	return true
}

type mockedHash struct {
	id        protocol.ID
	nodeInfos []dht.NodeInfo
}

func (m mockedHash) ID() protocol.ID {
	return m.id
}

func (m mockedHash) Peers() []ktable.HashPeer {
	peers := make([]ktable.HashPeer, 0, len(m.nodeInfos))
	for _, nodeInfo := range m.nodeInfos {
		peers = append(peers, ktable.HashPeer{
			// ID:   nodeInfo.ID,
			Addr: nodeInfo.Addr.ToAddrPort(),
		})
	}

	return peers
}

func (mockedHash) Dropped() bool {
	return false
}

func TestResponder_ping(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "ping",
			A: &dht.MsgArgs{
				ID: mocks.sender.ID,
			},
		},
	}
	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{ID: mocks.nodeID}, ret)
	assert.NoError(t, err)
}

func TestResponder_ping__missing_args(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "ping",
		},
	}
	_, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, ErrMissingArguments, err)
}

func TestResponder_find_node(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	target := protocol.RandomNodeID()
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "find_node",
			A: &dht.MsgArgs{
				ID:     mocks.sender.ID,
				Target: target,
			},
		},
	}
	nodes := dht.CompactIPv4NodeInfo{
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
	}
	peers := []ktable.Node{
		mockedPeer{nodes[0]},
		mockedPeer{nodes[1]},
		mockedPeer{nodes[2]},
	}
	mocks.table.On("GetClosestNodes", target).Return(peers)
	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID:    mocks.nodeID,
		Nodes: nodes,
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_find_node__missing_target(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "find_node",
			A: &dht.MsgArgs{
				ID: mocks.sender.ID,
			},
		},
	}
	_, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, ErrMissingArguments, err)
}

func TestResponder_get_peers__values(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	infoHash := protocol.RandomNodeID()
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "get_peers",
			A: &dht.MsgArgs{
				ID:       mocks.sender.ID,
				InfoHash: infoHash,
			},
		},
	}
	nodeInfos := []dht.NodeInfo{
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
	}
	expectedToken := mocks.responder.announceToken(infoHash, mocks.sender.ID, mocks.sender.Addr.ToAddrPort().Addr())
	mocks.table.On("GetHashOrClosestNodes", infoHash).Return(ktable.GetHashOrClosestNodesResult{
		Hash:  mockedHash{nodeInfos: nodeInfos},
		Found: true,
	})

	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID: mocks.nodeID,
		Values: []dht.NodeAddr{
			nodeInfos[0].Addr,
			nodeInfos[1].Addr,
			nodeInfos[2].Addr,
		},
		Nodes: nil,
		Token: &expectedToken,
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_get_peers__nodes(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	infoHash := protocol.RandomNodeID()
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "get_peers",
			A: &dht.MsgArgs{
				ID:       mocks.sender.ID,
				InfoHash: infoHash,
			},
		},
	}
	nodes := dht.CompactIPv4NodeInfo{
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
	}
	peers := []ktable.Node{
		mockedPeer{nodes[0]},
		mockedPeer{nodes[1]},
		mockedPeer{nodes[2]},
	}
	expectedToken := mocks.responder.announceToken(infoHash, mocks.sender.ID, mocks.sender.Addr.ToAddrPort().Addr())
	mocks.table.On("GetHashOrClosestNodes", infoHash).Return(ktable.GetHashOrClosestNodesResult{
		ClosestNodes: peers,
	})

	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID:     mocks.nodeID,
		Values: nil,
		Nodes:  nodes,
		Token:  &expectedToken,
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_get_peers__missing_info_hash(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "get_peers",
			A: &dht.MsgArgs{
				ID: mocks.sender.ID,
			},
		},
	}
	_, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, ErrMissingArguments, err)
}

func TestResponder_announce_peer__implied_port(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	infoHash := protocol.RandomNodeID()
	expectedToken := mocks.responder.announceToken(infoHash, mocks.sender.ID, mocks.sender.Addr.ToAddrPort().Addr())
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "announce_peer",
			A: &dht.MsgArgs{
				ID:          mocks.sender.ID,
				InfoHash:    infoHash,
				Token:       expectedToken,
				ImpliedPort: true,
			},
		},
	}

	mocks.table.On("BatchCommand", ktable.PutHash{
		ID: infoHash,
		Peers: []ktable.HashPeer{{
			Addr: mocks.sender.Addr.ToAddrPort(),
		}},
	}).Return(nil)

	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID: mocks.nodeID,
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_announce_peer__specified_port(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	infoHash := protocol.RandomNodeID()
	expectedToken := mocks.responder.announceToken(infoHash, mocks.sender.ID, mocks.sender.Addr.ToAddrPort().Addr())
	port := krpc.RandomNodeInfo(4).Addr.Port
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "announce_peer",
			A: &dht.MsgArgs{
				ID:       mocks.sender.ID,
				InfoHash: infoHash,
				Token:    expectedToken,
				Port:     &port,
			},
		},
	}

	mocks.table.On("BatchCommand", ktable.PutHash{
		ID: infoHash,
		Peers: []ktable.HashPeer{{
			Addr: netip.AddrPortFrom(mocks.sender.Addr.ToAddrPort().Addr(), uint16(port)),
		}},
	}).Return(nil)

	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID: mocks.nodeID,
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_sample_infohashes(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "sample_infohashes",
			A: &dht.MsgArgs{
				ID: mocks.sender.ID,
			},
		},
	}
	infoHashes := []ktable.Hash{
		mockedHash{id: protocol.RandomNodeID()},
		mockedHash{id: protocol.RandomNodeID()},
		mockedHash{id: protocol.RandomNodeID()},
	}
	nodes := dht.CompactIPv4NodeInfo{
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
		dht.RandomNodeInfo(4),
	}
	peers := []ktable.Node{
		mockedPeer{nodes[0]},
		mockedPeer{nodes[1]},
		mockedPeer{nodes[2]},
	}
	num := int64(123)
	mocks.table.On("SampleHashesAndNodes").Return(ktable.SampleHashesAndNodesResult{
		Hashes:      infoHashes,
		Nodes:       peers,
		TotalHashes: int(num),
	})

	ret, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, dht.Return{
		ID:    mocks.nodeID,
		Nodes: nodes,
		Bep51Return: dht.Bep51Return{
			Samples: &dht.CompactInfohashes{
				infoHashes[0].ID(),
				infoHashes[1].ID(),
				infoHashes[2].ID(),
			},
			Num:      &num,
			Interval: &mocks.responder.sampleInfoHashesInterval,
		},
	}, ret)
	assert.NoError(t, err)
}

func TestResponder_unknown_method(t *testing.T) {
	t.Parallel()

	mocks := newTestResponderMocks(t)
	msg := dht.RecvMsg{
		From: mocks.sender.Addr.ToAddrPort(),
		Msg: dht.Msg{
			Q: "foo",
			A: &dht.MsgArgs{
				ID: mocks.sender.ID,
			},
		},
	}
	_, err := mocks.responder.Respond(context.Background(), msg)
	assert.Equal(t, err, ErrMethodUnknown)
}

func TestResponderNodeDiscovery_AnnouncePeer(t *testing.T) {
	t.Parallel()

	t.Run("implied port and explicit ports", func(t *testing.T) {
		t.Parallel()

		mocks := newTestResponderMocks(t)
		discovered := make(chan ktable.Node, 10)
		announced := make(chan dht.AnnouncedPeer, 10)
		nd := responderNodeDiscovery{
			responder:       mocks.responder,
			discoveredNodes: discovered,
			announcedPeers:  announced,
		}

		from := mocks.sender.Addr.ToAddrPort()
		h1 := protocol.RandomNodeID()
		tok1 := mocks.responder.announceToken(h1, mocks.sender.ID, from.Addr())
		mocks.table.On("BatchCommand", ktable.PutHash{
			ID:    h1,
			Peers: []ktable.HashPeer{{Addr: from}},
		}).Return(nil).Once()

		_, err := nd.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:          mocks.sender.ID,
					InfoHash:    h1,
					Token:       tok1,
					ImpliedPort: true,
				},
			},
		})
		assert.NoError(t, err)
		got1 := <-announced
		assert.Equal(t, dht.AnnouncedPeer{InfoHash: h1, Node: from, Peer: from}, got1)

		h2 := protocol.RandomNodeID()
		tok2 := mocks.responder.announceToken(h2, mocks.sender.ID, from.Addr())
		explicitPort := 51413
		expectedPeer2 := netip.AddrPortFrom(from.Addr(), uint16(explicitPort))
		mocks.table.On("BatchCommand", ktable.PutHash{
			ID:    h2,
			Peers: []ktable.HashPeer{{Addr: expectedPeer2}},
		}).Return(nil).Once()

		_, err = nd.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:       mocks.sender.ID,
					InfoHash: h2,
					Token:    tok2,
					Port:     &explicitPort,
				},
			},
		})
		assert.NoError(t, err)
		got2 := <-announced
		assert.Equal(t, dht.AnnouncedPeer{InfoHash: h2, Node: from, Peer: expectedPeer2}, got2)

		h3 := protocol.RandomNodeID()
		tok3 := mocks.responder.announceToken(h3, mocks.sender.ID, from.Addr())
		zeroPort := 0
		mocks.table.On("BatchCommand", ktable.PutHash{
			ID:    h3,
			Peers: []ktable.HashPeer{{Addr: netip.AddrPortFrom(from.Addr(), 0)}},
		}).Return(nil).Once()

		_, err = nd.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:       mocks.sender.ID,
					InfoHash: h3,
					Token:    tok3,
					Port:     &zeroPort,
				},
			},
		})
		assert.NoError(t, err)
		got3 := <-announced
		assert.Equal(t, h3, got3.InfoHash)
		assert.Equal(t, from, got3.Node)
		assert.False(t, got3.Peer.IsValid(), "port 0 should leave Peer invalid")
	})

	t.Run("ignores non-announce and error responses", func(t *testing.T) {
		t.Parallel()

		mocks := newTestResponderMocks(t)
		discovered := make(chan ktable.Node, 10)
		announced := make(chan dht.AnnouncedPeer, 10)
		nd := responderNodeDiscovery{
			responder:       mocks.responder,
			discoveredNodes: discovered,
			announcedPeers:  announced,
		}

		from := mocks.sender.Addr.ToAddrPort()
		_, err := nd.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QPing,
				A: &dht.MsgArgs{ID: mocks.sender.ID},
			},
		})
		assert.NoError(t, err)

		_, err = nd.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:          mocks.sender.ID,
					InfoHash:    protocol.RandomNodeID(),
					Token:       "wrong-token",
					ImpliedPort: true,
				},
			},
		})
		assert.ErrorIs(t, err, ErrInvalidToken)
		assert.Empty(t, announced)
	})

	t.Run("non-blocking when full or nil", func(t *testing.T) {
		t.Parallel()

		mocks := newTestResponderMocks(t)
		discovered := make(chan ktable.Node, 10)
		announced := make(chan dht.AnnouncedPeer, 1)
		announced <- dht.AnnouncedPeer{InfoHash: protocol.RandomNodeID()}

		from := mocks.sender.Addr.ToAddrPort()
		h := protocol.RandomNodeID()
		tok := mocks.responder.announceToken(h, mocks.sender.ID, from.Addr())
		mocks.table.On("BatchCommand", ktable.PutHash{
			ID:    h,
			Peers: []ktable.HashPeer{{Addr: from}},
		}).Return(nil).Twice()

		ndFull := responderNodeDiscovery{
			responder:       mocks.responder,
			discoveredNodes: discovered,
			announcedPeers:  announced,
		}
		_, err := ndFull.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:          mocks.sender.ID,
					InfoHash:    h,
					Token:       tok,
					ImpliedPort: true,
				},
			},
		})
		assert.NoError(t, err)
		assert.Len(t, announced, 1)

		ndNil := responderNodeDiscovery{
			responder:       mocks.responder,
			discoveredNodes: discovered,
		}
		_, err = ndNil.Respond(context.Background(), dht.RecvMsg{
			From: from,
			Msg: dht.Msg{
				Q: dht.QAnnouncePeer,
				A: &dht.MsgArgs{
					ID:          mocks.sender.ID,
					InfoHash:    h,
					Token:       tok,
					ImpliedPort: true,
				},
			},
		})
		assert.NoError(t, err)
	})
}
