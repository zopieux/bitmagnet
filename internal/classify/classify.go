package classify

import (
	"context"
	"errors"

	"github.com/bitmagnet-io/bitmagnet/internal/classify/parsers"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/tmdb"
)

// ErrUnmatched is returned when no content match is found.
var ErrUnmatched = errors.New("no match found")

func Classify(
	ctx context.Context,
	torrent model.Torrent,
	tmdbClient tmdb.Client,
) (Result, error) {
	return Result{}, nil
}

// Classify runs the straight-line classification pipeline for a torrent:
// 1. Parse date from the torrent name.
// 2. Parse video content attributes (title, year, episodes, resolution, etc.).
// 3. If a base title was found and TMDB is enabled, search TMDB for matching content.
//
// There are no workflows, flags, or conditions — just this function.
// TMDB lookup is skipped if tmdbClient is nil.
func classifyLegacy(
	ctx context.Context,
	torrent model.Torrent,
	tmdbClient tmdb.Client,
) (Result, error) {
	var result Result

	// 1. Parse date from name.
	if date := parsers.ParseDate(torrent.Name); !date.IsNil() {
		result.Date = date
	}

	if attrs, err := parsers.ParseVideoContent(torrent, result.ContentType, result.Date); err == nil {
		result.MergeAttributes(attrs)
	}

	// 4. TMDB lookup (skipped if no client or no base title).
	if tmdbClient == nil || !result.BaseTitle.Valid {
		return result, nil
	}

	var content *model.Content

	switch result.ContentType.ContentType {
	case model.ContentTypeTvShow:
		tvShow, searchErr := tmdbSearchTVShow(ctx, tmdbClient, result.BaseTitle.String, result.Date.Year)
		if searchErr != nil {
			if errors.Is(searchErr, ErrUnmatched) {
				// No TMDB match is not a failure; return parsed attributes.
				return result, nil
			}
			return result, searchErr
		}

		content = &tvShow
	default:
		if len(result.Episodes) > 0 {
			// Episodes but not a TV show; skip TMDB.
			return result, nil
		}

		movie, searchErr := tmdbSearchMovie(ctx, tmdbClient, result.BaseTitle.String, result.Date.Year)
		if searchErr != nil {
			if errors.Is(searchErr, ErrUnmatched) {
				// No TMDB match is not a failure; return parsed attributes.
				return result, nil
			}
			return result, searchErr
		}

		content = &movie
	}

	result.AttachContent(content)

	return result, nil
}

var _ = classifyLegacy
