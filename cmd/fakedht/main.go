// fakedht is a test-only fake DHT node + BitTorrent peer for exercising
// bitmagnette's DHT crawler without touching the real internet DHT.
//
// It generates N deterministic fake torrents (seeded by FAKEDHT_SEED),
// serves their infohashes via BEP 51 sample_infohashes, answers get_peers
// with its own peer address, and serves the torrent metadata over
// ut_metadata (BEP 9).
//
// Point bitmagnette at it with:
//   DHT_CRAWLER_BOOTSTRAP_NODES='["127.0.0.1:<dht-port>"]' bitmagnet worker run --all
//
// Env vars:
//   FAKEDHT_SEED       string seed for deterministic torrent generation (default "test")
//   FAKEDHT_DHT_PORT   UDP port for the fake DHT node (default 6889)
//   FAKEDHT_PEER_PORT  TCP port for the fake peer (default 6890)
//   FAKEDHT_COUNT      number of fake torrents to generate (default 20)
package main

import (
	"fmt"
	"os"
	"strconv"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	seed := getenv("FAKEDHT_SEED", "test")
	dhtPort := getenvInt("FAKEDHT_DHT_PORT", 6889)
	peerPort := getenvInt("FAKEDHT_PEER_PORT", 6890)
	count := getenvInt("FAKEDHT_COUNT", 20)

	torrents := generateTorrents(seed, count)
	fmt.Printf("fakedht: generated %d torrents (seed %q)\n", len(torrents), seed)
	for i, t := range torrents {
		if i < 3 {
			fmt.Printf("  %x %q\n", t.infoHash, t.title)
		}
	}
	if len(torrents) > 3 {
		fmt.Printf("  ... and %d more\n", len(torrents)-3)
	}

	peer, err := newPeerServer(fmt.Sprintf("127.0.0.1:%d", peerPort), torrents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "peer server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("fakedht: peer listening on %s\n", peer.addr())

	dht, err := newDHTServer(fmt.Sprintf("127.0.0.1:%d", dhtPort), torrents, peer.addr())
	if err != nil {
		fmt.Fprintf(os.Stderr, "dht server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("fakedht: dht listening on %s\n", dht.addr())
	fmt.Printf("fakedht: point bitmagnette at it with DHT_CRAWLER_BOOTSTRAP_NODES='[\"127.0.0.1:%d\"]'\n", dhtPort)

	go peer.serve()
	dht.serve() // blocks
}
