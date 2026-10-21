package tmdb

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
)

type Requester interface {
	Request(ctx context.Context, path string, queryParams map[string]string, result any) (*resty.Response, error)
}

type requester struct {
	resty *resty.Client
}

func (r requester) Request(
	ctx context.Context,
	path string,
	queryParams map[string]string,
	result any,
) (*resty.Response, error) {
	backoff := 500 * time.Millisecond
	maxRetries := 5

	for attempt := 0; attempt <= maxRetries; attempt++ {
		res, err := r.resty.R().
			SetContext(ctx).
			SetQueryParams(queryParams).
			SetResult(&result).
			Get(path)

		if err == nil {
			if res.IsSuccess() {
				return res, nil
			}
			if res.StatusCode() == http.StatusTooManyRequests {
				if attempt < maxRetries {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(backoff):
						backoff *= 2
						continue
					}
				}
			}
			switch res.StatusCode() {
			case http.StatusUnauthorized:
				return res, ErrUnauthorized
			case http.StatusNotFound:
				return res, ErrNotFound
			default:
				return res, newError(res.Status())
			}
		}

		if attempt == maxRetries {
			return res, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}

	return nil, errors.New("tmdb request failed after retries")
}
