package classify

import (
	"context"
	"errors"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/tmdb"
)

func tmdbSearchMovie(
	ctx context.Context,
	client tmdb.Client,
	title string,
	year model.Year,
) (model.Content, error) {
	req := tmdb.SearchMovieRequest{
		Query:        title,
		IncludeAdult: true,
	}
	if !year.IsNil() {
		req.Year = year
	}

	searchResult, searchErr := client.SearchMovie(ctx, req)
	if searchErr != nil {
		return model.Content{}, searchErr
	}

	bestMatch, ok := levenshteinFindBestMatch[tmdb.SearchMovieResult](
		title,
		searchResult.Results,
		func(item tmdb.SearchMovieResult) []string {
			return []string{item.Title, item.OriginalTitle}
		},
	)

	if !ok {
		return model.Content{}, ErrUnmatched
	}

	return tmdbGetMovieByTMDBID(ctx, client, bestMatch.ID)
}

func tmdbSearchTVShow(
	ctx context.Context,
	client tmdb.Client,
	title string,
	year model.Year,
) (model.Content, error) {
	req := tmdb.SearchTvRequest{
		Query:        title,
		IncludeAdult: true,
	}
	if !year.IsNil() {
		req.FirstAirDateYear = year
	}

	searchResult, searchErr := client.SearchTv(ctx, req)
	if searchErr != nil {
		return model.Content{}, searchErr
	}

	bestMatch, ok := levenshteinFindBestMatch[tmdb.SearchTvResult](
		title,
		searchResult.Results,
		func(item tmdb.SearchTvResult) []string {
			return []string{item.Name, item.OriginalName}
		},
	)

	if !ok {
		return model.Content{}, ErrUnmatched
	}

	return tmdbGetTVShowByTMDBID(ctx, client, bestMatch.ID)
}

func tmdbGetMovieByTMDBID(
	ctx context.Context,
	client tmdb.Client,
	id int64,
) (movie model.Content, err error) {
	d, getDetailsErr := client.MovieDetails(ctx, tmdb.MovieDetailsRequest{
		ID: id,
	})
	if getDetailsErr != nil {
		if errors.Is(getDetailsErr, tmdb.ErrNotFound) {
			getDetailsErr = ErrUnmatched
		}

		err = getDetailsErr

		return
	}

	return tmdb.MovieDetailsToMovieModel(d)
}

func tmdbGetTVShowByTMDBID(
	ctx context.Context,
	client tmdb.Client,
	id int64,
) (tvShow model.Content, err error) {
	d, getDetailsErr := client.TvDetails(ctx, tmdb.TvDetailsRequest{
		SeriesID:         id,
		AppendToResponse: []string{"external_ids"},
	})
	if getDetailsErr != nil {
		if errors.Is(getDetailsErr, tmdb.ErrNotFound) {
			getDetailsErr = ErrUnmatched
		}

		err = getDetailsErr

		return
	}

	return tmdb.TvShowDetailsToTvShowModel(d)
}
