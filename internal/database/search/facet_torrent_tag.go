package search

import (
	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

const TorrentTagFacetKey = "torrent_tag"

func TorrentTagsFacet(options ...query.FacetOption) query.Facet {
	return torrentTagFacet{
		FacetConfig: query.NewFacetConfig(
			append([]query.FacetOption{
				query.FacetHasKey(TorrentTagFacetKey),
				query.FacetHasLabel("Torrent Tag"),
				query.FacetUsesAndLogic(),
				query.FacetHasAggregationOption(query.RequireJoin(model.TableNameTorrentContent)),
				query.FacetTriggersCte(),
			}, options...)...,
		),
	}
}

type torrentTagFacet struct {
	query.FacetConfig
}

func (torrentTagFacet) Values(ctx query.FacetContext) (map[string]string, error) {
	q := ctx.Query().TorrentTag

	tags, tagsErr := q.WithContext(ctx.Context()).Distinct(q.Name).Find()
	if tagsErr != nil {
		return nil, tagsErr
	}

	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		values[tag.Name] = tag.Name
	}

	return values, nil
}

func (torrentTagFacet) Criteria(filter query.FacetFilter) []query.Criteria {
	criteria := make([]query.Criteria, len(filter))
	for i, tag := range filter.Values() {
		criteria[i] = TorrentTagCriteria(tag)
	}

	return criteria
}

func (f torrentTagFacet) Aggregate(ctx query.FacetContext, values map[string]string) (query.AggregationItems, error) {
	type tagCount struct {
		Val   string `gorm:"column:val"`
		Count int64  `gorm:"column:count"`
	}

	var rows []tagCount

	if !ctx.HasFilter() {
		if err := ctx.Query().TorrentTag.WithContext(ctx.Context()).UnderlyingDB().
			Table("torrent_tags").
			Select("name AS val, count(*) AS count").
			Group("name").
			Find(&rows).Error; err != nil {
			return nil, err
		}
	} else {
		q, qErr := ctx.NewAggregationQuery(
			f.AggregationOption,
			query.WithCurrentFacet(f.Key()),
		)
		if qErr != nil {
			return nil, qErr
		}
		subSelect := q.UnderlyingDB().Select("DISTINCT " + ctx.TableName() + ".info_hash")

		if err := ctx.Query().TorrentTag.WithContext(ctx.Context()).UnderlyingDB().
			Table("torrent_tags").
			Select("torrent_tags.name AS val, count(DISTINCT torrent_tags.info_hash) AS count").
			Where("torrent_tags.info_hash IN (?)", subSelect).
			Group("torrent_tags.name").
			Find(&rows).Error; err != nil {
			return nil, err
		}
	}

	items := make(query.AggregationItems, len(values))
	groupedCounts := make(map[string]int64, len(rows))
	for _, r := range rows {
		groupedCounts[r.Val] = r.Count
	}
	filter := f.Filter()
	for key, count := range groupedCounts {
		if count > 0 || filter.HasKey(key) {
			if label, ok := values[key]; ok {
				items[key] = query.AggregationItem{
					Label: label,
					Count: uint(count),
				}
			}
		}
	}
	for key := range filter {
		if _, ok := items[key]; !ok {
			if label, ok := values[key]; ok {
				items[key] = query.AggregationItem{
					Label: label,
					Count: 0,
				}
			}
		}
	}

	return items, nil
}

