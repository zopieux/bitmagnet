package search

import (
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// TorrentFilePathCriteria matches torrent files whose path contains the
// given substring (SQLite LIKE is case-insensitive for ASCII). LIKE
// wildcards in the input are escaped: the match is a plain substring.
func TorrentFilePathCriteria(substr string) query.Criteria {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(substr)
	return query.DBCriteria{
		SQL:  model.TableNameTorrentFile + `.path LIKE ? ESCAPE '\'`,
		Args: []interface{}{"%" + esc + "%"},
	}
}
