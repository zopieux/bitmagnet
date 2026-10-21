package torrentmetrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/metrics"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"gorm.io/gorm"
)

type Bucket struct {
	Source  string
	Bucket  time.Time
	Updated bool
	Count   uint
}

type rawBucket struct {
	Source  string
	Bucket  string
	Updated bool
	Count   uint
}

type Request struct {
	BucketDuration metrics.BucketDuration
	Sources        []string
	StartTime      time.Time
	EndTime        time.Time
	Updated        model.NullBool
}

type Client interface {
	Request(context.Context, Request) ([]Bucket, error)
}

type client struct {
	db *gorm.DB
}

func (c client) Request(ctx context.Context, req Request) ([]Bucket, error) {
	// SQLite equivalent of date_trunc for the bucket duration.
	var truncExpr string
	switch req.BucketDuration {
	case "minute":
		truncExpr = "strftime('%Y-%m-%d %H:%M:00', updated_at)"
	case "hour":
		truncExpr = "strftime('%Y-%m-%d %H:00:00', updated_at)"
	case "day":
		truncExpr = "date(updated_at)"
	default:
		return nil, fmt.Errorf("unsupported bucket duration: %s", req.BucketDuration)
	}

	params := []any{}

	var conditions []string
	if !req.StartTime.IsZero() {
		conditions = append(conditions, "datetime(updated_at) >= datetime(?)")
		params = append(params, req.StartTime.UTC().Format("2006-01-02 15:04:05"))
	}

	if !req.EndTime.IsZero() {
		conditions = append(conditions, "datetime(updated_at) <= datetime(?)")
		params = append(params, req.EndTime.UTC().Format("2006-01-02 15:04:05"))
	}

	if req.Sources != nil {
		conditions = append(conditions, "source IN ?")
		params = append(params, req.Sources)
	}

	if req.Updated.Valid {
		sign := ">"
		if !req.Updated.Bool {
			sign = "<="
		}

		conditions = append(conditions, "datetime(updated_at) "+sign+" datetime(created_at, '+1 hour')")
	}

	conditionClause := ""
	if len(conditions) > 0 {
		conditionClause = "WHERE (" + strings.Join(conditions, " AND ") + ")"
	}

	var rawResult []rawBucket
	if err := c.db.WithContext(ctx).Raw(fmt.Sprintf(`select
        source,
        %s as bucket,
        datetime(updated_at) > datetime(created_at, '+1 hour') as updated,
        count(*) as count
        from torrents_torrent_sources
       `, truncExpr)+
		conditionClause+
		`
    group by source, bucket, updated
    order by source, bucket, updated`,
		params...,
	).Scan(&rawResult).Error; err != nil {
		return nil, err
	}

	result := make([]Bucket, len(rawResult))
	for i, rb := range rawResult {
		// SQLite strftime returns "YYYY-MM-DD HH:MM:SS"; parse as UTC.
		t, parseErr := time.Parse("2006-01-02 15:04:05", rb.Bucket)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse bucket time %q: %w", rb.Bucket, parseErr)
		}
		result[i] = Bucket{
			Source:  rb.Source,
			Bucket:  t,
			Updated: rb.Updated,
			Count:   rb.Count,
		}
	}

	return result, nil
}
