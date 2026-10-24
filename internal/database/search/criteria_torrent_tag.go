package search

import (
	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
)

func TorrentTagCriteria(tagNames ...string) query.Criteria {
	return query.GenCriteria(func(ctx query.DBContext) (query.Criteria, error) {
		q := ctx.Query()

		return query.RawCriteria{
			Query: ctx.TableName() + ".info_hash IN (?)",
			Args: []interface{}{
				q.TorrentTag.UnderlyingDB().Table("torrent_tags").Select("info_hash").Where("name IN (?)", tagNames),
			},
		}, nil
	})
}
