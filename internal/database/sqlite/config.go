package sqlite

import "time"

type Config struct {
	// DSN is the SQLite database path. When empty, Name is used.
	DSN string
	// Name is the SQLite database path when DSN is empty.
	Name string
	// WriteInterval is how often background writers flush their in-RAM buffers
	// to the database. Increase this to reduce disk writes on rotating media.
	// Data buffered in RAM is lost on power loss or crash.
	WriteInterval time.Duration
	// CheckpointInterval is how often the WAL is truncated back into the main
	// database file. Setting this higher than WriteInterval coalesces hot B-tree
	// pages across multiple flushes into a single sorted writeback burst.
	CheckpointInterval time.Duration
}

func NewDefaultConfig() Config {
	return Config{
		Name:               "bitmagnet",
		WriteInterval:      5 * time.Minute,
		CheckpointInterval: 30 * time.Minute,
	}
}
