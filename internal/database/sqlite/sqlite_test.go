package sqlite

import (
	"database/sql"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/require"
)

func TestPragmas(t *testing.T) {
	tempFile := t.TempDir() + "/test.db"
	db, err := sql.Open("sqlite", BuildDSN(tempFile))
	require.NoError(t, err)
	defer db.Close()

	var cacheSize int
	err = db.QueryRow("PRAGMA cache_size").Scan(&cacheSize)
	require.NoError(t, err)
	require.Equal(t, -64000, cacheSize)

	var tempStore int
	err = db.QueryRow("PRAGMA temp_store").Scan(&tempStore)
	require.NoError(t, err)
	require.Equal(t, 2, tempStore) // 2 = MEMORY

	var walAutocheckpoint int
	err = db.QueryRow("PRAGMA wal_autocheckpoint").Scan(&walAutocheckpoint)
	require.NoError(t, err)
	require.Equal(t, 25000, walAutocheckpoint)

	var mmapSize int64
	err = db.QueryRow("PRAGMA mmap_size").Scan(&mmapSize)
	require.NoError(t, err)
	t.Logf("mmap_size: %d", mmapSize)
	require.Equal(t, int64(536870912), mmapSize)
}

func TestPragmas_UserOverride(t *testing.T) {
	tempFile := t.TempDir() + "/test.db?_pragma=mmap_size(1073741824)&_pragma=wal_autocheckpoint=5000"
	db, err := sql.Open("sqlite", BuildDSN(tempFile))
	require.NoError(t, err)
	defer db.Close()

	var mmapSize int64
	err = db.QueryRow("PRAGMA mmap_size").Scan(&mmapSize)
	require.NoError(t, err)
	require.Equal(t, int64(1073741824), mmapSize)

	var walAutocheckpoint int
	err = db.QueryRow("PRAGMA wal_autocheckpoint").Scan(&walAutocheckpoint)
	require.NoError(t, err)
	require.Equal(t, 5000, walAutocheckpoint)

	// Defaults not overridden should still be present
	var cacheSize int
	err = db.QueryRow("PRAGMA cache_size").Scan(&cacheSize)
	require.NoError(t, err)
	require.Equal(t, -64000, cacheSize)
}
