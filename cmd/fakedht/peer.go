// Minimal fake BitTorrent peer serving ut_metadata (BEP 9 / BEP 10).
package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

const (
	extIDHandshake  = 0x00
	extIDUtMetadata = 0x01
	// LTEP bit (BEP 10) in the 8 reserved handshake bytes.
	ltepBit = 20
)

type peerServer struct {
	listener *net.TCPListener
	byHash   map[[20]byte]fakeTorrent
	peerID   [20]byte
}

func newPeerServer(peerAddr string, torrents []fakeTorrent) (*peerServer, error) {
	taddr, err := net.ResolveTCPAddr("tcp", peerAddr)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenTCP("tcp", taddr)
	if err != nil {
		return nil, err
	}
	byHash := make(map[[20]byte]fakeTorrent, len(torrents))
	for _, t := range torrents {
		byHash[t.infoHash] = t
	}
	var peerID [20]byte
	if _, err := rand.Read(peerID[:]); err != nil {
		return nil, err
	}
	return &peerServer{listener: ln, byHash: byHash, peerID: peerID}, nil
}

func (s *peerServer) serve() {
	for {
		conn, err := s.listener.AcceptTCP()
		if err != nil {
			continue
		}
		go s.handle(conn)
	}
}

func (s *peerServer) addr() *net.TCPAddr {
	return s.listener.Addr().(*net.TCPAddr)
}

func (s *peerServer) handle(conn *net.TCPConn) {
	defer conn.Close()

	// 1. BitTorrent handshake (68 bytes).
	hs := make([]byte, 68)
	if _, err := io.ReadFull(conn, hs); err != nil {
		return
	}
	if hs[0] != 19 || string(hs[1:20]) != "BitTorrent protocol" {
		return
	}
	var infoHash [20]byte
	copy(infoHash[:], hs[28:48])
	torrent, ok := s.byHash[infoHash]
	if !ok {
		fmt.Printf("fakedht: peer connection from %s for unknown hash %x\n", conn.RemoteAddr(), infoHash)
		return
	}

	fmt.Printf("fakedht: peer connection from %s for %q\n", conn.RemoteAddr(), torrent.title)

	// Respond with LTEP bit set.
	resp := make([]byte, 0, 68)
	resp = append(resp, 19)
	resp = append(resp, []byte("BitTorrent protocol")...)
	reserved := make([]byte, 8)
	reserved[7-ltepBit/8] |= 1 << (ltepBit % 8)
	resp = append(resp, reserved...)
	resp = append(resp, infoHash[:]...)
	resp = append(resp, s.peerID[:]...)
	if _, err := conn.Write(resp); err != nil {
		return
	}

	// 2. Extended handshake: read client's, send ours.
	// Client sends: 4-byte len + 0x14 + 0x00 + bencoded dict.
	if _, err := readExtMessage(conn); err != nil {
		return
	}
	extHS := bencode(map[string]interface{}{
		"m":             map[string]interface{}{"ut_metadata": 1},
		"metadata_size": len(torrent.infoDict),
	})
	if err := writeExtMessage(conn, extIDHandshake, extHS); err != nil {
		return
	}

	// 3. Serve ut_metadata piece requests.
	// Our metadata is small (< 16 KiB), so it's always a single piece.
	for {
		msg, err := readExtMessage(conn)
		if err != nil {
			return
		}
		if len(msg) < 1 || msg[0] != extIDUtMetadata {
			continue
		}
		dict, err := bdecode(msg[1:])
		if err != nil {
			return
		}
		d, ok := dict.(map[string]interface{})
		if !ok {
			return
		}
		msgType, _ := d["msg_type"].(int64)
		piece, _ := d["piece"].(int64)
		if msgType != 0 || piece != 0 {
			// We only serve single-piece metadata; reject anything else.
			reject := bencode(map[string]interface{}{"msg_type": 2, "piece": piece})
			_ = writeExtMessage(conn, extIDUtMetadata, reject)
			return
		}
		data := bencode(map[string]interface{}{
			"msg_type":   1,
			"piece":      0,
			"total_size": len(torrent.infoDict),
		})
		data = append(data, torrent.infoDict...)
		if err := writeExtMessage(conn, extIDUtMetadata, data); err != nil {
			return
		}
		// Client closes after receiving; keep serving in case it asks again.
	}
}

// readExtMessage reads a BitTorrent extended message: 4-byte big-endian
// length prefix, then 0x14, then the extension ID byte, then payload.
func readExtMessage(conn *net.TCPConn) ([]byte, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(lenBuf)
	if n < 2 || n > 1<<20 {
		return nil, fmt.Errorf("bad extended message length %d", n)
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(conn, msg); err != nil {
		return nil, err
	}
	if msg[0] != 0x14 {
		return nil, fmt.Errorf("not an extended message")
	}
	return msg[1:], nil
}

func writeExtMessage(conn *net.TCPConn, extID byte, payload []byte) error {
	total := 2 + len(payload)
	buf := make([]byte, 0, 4+total)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(total))
	buf = append(buf, lenBuf...)
	buf = append(buf, 0x14, extID)
	buf = append(buf, payload...)
	_, err := conn.Write(buf)
	return err
}
