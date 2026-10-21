package search

import (
	"fmt"

	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

func TorrentContentEpisodesCriteria(episodes model.Episodes) query.Criteria {
	return query.GenCriteria(func(query.DBContext) (query.Criteria, error) {
		and := make([]query.Criteria, 0, len(episodes))

		for _, s := range episodes.SeasonEntries() {
			if len(s.Episodes) == 0 {
				// whole season: the season key maps to an empty object
				and = append(and, query.DBCriteria{
					SQL: fmt.Sprintf(`json_extract(torrent_contents.episodes, '$."%d"') = '{}'`, s.Season),
				})
			} else {
				// specific episodes: each episode key must exist under the season
				for _, e := range s.Episodes {
					and = append(and, query.DBCriteria{
						SQL: fmt.Sprintf(`json_extract(torrent_contents.episodes, '$."%d"."%d"') IS NOT NULL`, s.Season, e),
					})
				}
			}
		}

		return query.AndCriteria{
			Criteria: and,
		}, nil
	})
}
