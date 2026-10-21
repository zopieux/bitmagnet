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
}

func NewDefaultConfig() Config {
	return Config{
		Name:          "bitmagnet",
		WriteInterval: time.Minute,
	}
}
