// Minimal fake DHT node (BEP 5 + BEP 51 sample_infohashes).
package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
)

type dhtServer struct {
	conn     *net.UDPConn
	nodeID   [20]byte
	torrents []fakeTorrent
	byHash   map[[20]byte]fakeTorrent
	peerAddr *net.TCPAddr
	// sampleOffset rotates the sample window so repeated sample_infohashes
	// responses cover all torrents instead of always returning the first page.
	sampleOffset atomic.Int64
}

func newDHTServer(dhtAddr string, torrents []fakeTorrent, peerAddr *net.TCPAddr) (*dhtServer, error) {
	// Listen dual-stack: the crawler may send via IPv4-mapped IPv6.
	uaddr, err := net.ResolveUDPAddr("udp", dhtAddr)
	if err != nil {
		return nil, err
	}
	// Try the resolved address first; fall back to IPv6 any (dual-stack).
	var conn *net.UDPConn
	if c, err := net.ListenUDP("udp", uaddr); err == nil {
		conn = c
	} else {
		v6addr := &net.UDPAddr{IP: net.IPv6zero, Port: uaddr.Port}
		c, err := net.ListenUDP("udp", v6addr)
		if err != nil {
			return nil, err
		}
		conn = c
	}
	var nodeID [20]byte
	if _, err := rand.Read(nodeID[:]); err != nil {
		return nil, err
	}
	byHash := make(map[[20]byte]fakeTorrent, len(torrents))
	for _, t := range torrents {
		byHash[t.infoHash] = t
	}

	return &dhtServer{conn: conn, nodeID: nodeID, torrents: torrents, byHash: byHash, peerAddr: peerAddr}, nil
}

func (s *dhtServer) serve() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		// Copy the packet before handing it to a goroutine: buf is reused by
		// the next ReadFromUDP, which would otherwise overwrite the packet
		// while the handler is still decoding it.
		pkt := append([]byte(nil), buf[:n]...)
		go s.handle(pkt, addr)
	}
}

func (s *dhtServer) handle(data []byte, addr *net.UDPAddr) {
	fmt.Printf("fakedht: raw %d bytes from %s: %q\n", len(data), addr, data)

	msg, err := bdecode(data)
	if err != nil {
		return
	}
	m, ok := msg.(map[string]interface{})
	if !ok {
		return
	}
	y, _ := m["y"].([]byte)
	if string(y) != "q" {
		return
	}
	qb, _ := m["q"].([]byte)
	q := string(qb)
	t, _ := m["t"].([]byte)
	if t == nil {
		return
	}

	r := map[string]interface{}{"id": s.nodeID[:]}

	// Cap the sample page so the response always fits a single UDP datagram
	// (returning every hash at once exceeds 64KB beyond ~3000 torrents and the
	// write fails silently). Real BEP 51 nodes also return a subset.
	const samplePageSize = 500

	sampleCount := 0
	scrapeReq := false

	switch q {
	case "ping":
		// r already has id.
	case "find_node":
		// Return no additional nodes; the crawler already knows us via bootstrap.
		r["nodes"] = []byte{}
	case "sample_infohashes":
		n := len(s.torrents)
		start := 0
		if n > samplePageSize {
			start = int(s.sampleOffset.Add(samplePageSize) % int64(n))
		}

		page := make([]fakeTorrent, 0, samplePageSize)
		for i := 0; i < n && len(page) < samplePageSize; i++ {
			page = append(page, s.torrents[(start+i)%n])
		}

		samples := make([]byte, 0, 20*len(page))
		for _, t := range page {
			samples = append(samples, t.infoHash[:]...)
		}

		sampleCount = len(page)
		r["samples"] = samples
		r["num"] = int64(n)
		r["interval"] = int64(0)
	case "get_peers":
		// Return our fake peer for any infohash.
		r["values"] = []interface{}{compactPeer(s.peerAddr)}
		r["token"] = []byte("faketoken")
		// BEP 33: a scrape request gets bloom filters for the torrent's swarm.
		if args, ok := m["a"].(map[string]interface{}); ok {
			if scrape, _ := args["scrape"].(int64); scrape == 1 {
				scrapeReq = true
				if ih, _ := args["info_hash"].([]byte); len(ih) == 20 {
					var hash [20]byte
					copy(hash[:], ih)
					if t, found := s.byHash[hash]; found {
						bfpe, bfsd := t.scrapeFilters()
						r["BFpe"] = bfpe[:]
						r["BFsd"] = bfsd[:]
					}
				}
			}
		}
	default:
		return
	}

	resp := map[string]interface{}{"t": t, "y": "r", "r": r}
	if _, err := s.conn.WriteToUDP(bencode(resp), addr); err != nil {
		fmt.Printf("fakedht: reply to %s query from %s failed: %v\n", q, addr, err)
		return
	}

	if q == "sample_infohashes" {
		fmt.Printf("fakedht: query sample_infohashes from %s -> %d hashes (of %d)\n", addr, sampleCount, len(s.torrents))
	} else if scrapeReq {
		fmt.Printf("fakedht: query get_peers (scrape) from %s\n", addr)
	} else {
		fmt.Printf("fakedht: query %s from %s\n", q, addr)
	}
}

// compactNode encodes a DHT node as 26 bytes: 20-byte ID + 4-byte IP + 2-byte port.
func compactNode(id [20]byte, addr *net.UDPAddr) []byte {
	out := make([]byte, 0, 26)
	out = append(out, id[:]...)
	ip4 := addr.IP.To4()
	if ip4 == nil {
		ip4 = net.ParseIP("127.0.0.1").To4()
	}
	out = append(out, ip4...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(addr.Port))
	out = append(out, port...)
	return out
}

// compactPeer encodes a peer address as 6 bytes: 4-byte IP + 2-byte port.
func compactPeer(addr *net.TCPAddr) []byte {
	out := make([]byte, 0, 6)
	ip4 := addr.IP.To4()
	if ip4 == nil {
		ip4 = net.ParseIP("127.0.0.1").To4()
	}
	out = append(out, ip4...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(addr.Port))
	out = append(out, port...)
	return out
}

func (s *dhtServer) addr() *net.UDPAddr {
	return s.conn.LocalAddr().(*net.UDPAddr)
}

var _ = fmt.Sprintf // keep fmt if unused in future edits
