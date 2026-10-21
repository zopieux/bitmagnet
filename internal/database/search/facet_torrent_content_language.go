package search

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/maps"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

const LanguageFacetKey = "language"

func TorrentContentLanguageFacet(options ...query.FacetOption) query.Facet {
	return torrentContentLanguageFacet{
		FacetConfig: query.NewFacetConfig(
			append([]query.FacetOption{
				query.FacetHasKey(LanguageFacetKey),
				query.FacetHasLabel("Language"),
				query.FacetUsesLogic(model.FacetLogicOr),
				query.FacetTriggersCte(),
			}, options...)...,
		),
	}
}

type torrentContentLanguageFacet struct {
	query.FacetConfig
}

func (torrentContentLanguageFacet) Values(query.FacetContext) (map[string]string, error) {
	languageValues := model.LanguageValues()
	values := make(map[string]string, len(languageValues))

	for _, l := range languageValues {
		values[l.ID()] = l.Name()
	}

	return values, nil
}

func (torrentContentLanguageFacet) Criteria(filter query.FacetFilter) []query.Criteria {
	return []query.Criteria{
		query.GenCriteria(func(query.DBContext) (query.Criteria, error) {
			langs := make([]model.Language, 0, len(filter))
			for _, v := range filter.Values() {
				lang := model.ParseLanguage(v)
				if !lang.Valid {
					return nil, errors.New("invalid language filter specified")
				}
				langs = append(langs, lang.Language)
			}
			if len(langs) == 0 {
				return query.AndCriteria{}, nil
			}
			quoted := make([]string, len(langs))
			for i, lang := range langs {
				quoted[i] = fmt.Sprintf("'%s'", lang.ID())
			}
			return query.RawCriteria{
				// languages is a JSON array; match if any filter language is present.
				Query: fmt.Sprintf(
					"EXISTS (SELECT 1 FROM json_each(torrent_contents.languages) WHERE value IN (%s))",
					strings.Join(quoted, ","),
				),
				Joins: maps.NewInsertMap(
					maps.MapEntry[string, struct{}]{Key: model.TableNameTorrentContent},
				),
			}, nil
		}),
	}
}
