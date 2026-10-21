package queuemetrics

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
	Queue           string
	Status          model.QueueJobStatus
	Bucket time.Time
	RanAtBucket     time.Time
	Count           uint
	Latency         *time.Duration
}

type Request struct {
	BucketDuration metrics.BucketDuration
	Statuses       []model.QueueJobStatus
	Queues         []string
	StartTime      time.Time
	EndTime        time.Time
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
		truncExpr = "strftime('%%Y-%%m-%%d %%H:%%M:00', %s)"
	case "hour":
		truncExpr = "strftime('%%Y-%%m-%%d %%H:00:00', %s)"
	case "day":
		truncExpr = "date(%s)"
	default:
		return nil, fmt.Errorf("unsupported bucket duration: %s", req.BucketDuration)
	}

	createdAtTrunc := fmt.Sprintf(truncExpr, "created_at")
	ranAtTrunc := fmt.Sprintf(truncExpr, "ran_at")

	var queries []string
	var params []any

	includePending := len(req.Statuses) == 0
	var nonPendingStatuses []model.QueueJobStatus
	for _, s := range req.Statuses {
		if s == model.QueueJobStatusPending {
			includePending = true
		} else {
			nonPendingStatuses = append(nonPendingStatuses, s)
		}
	}
	includeNonPending := len(req.Statuses) == 0 || len(nonPendingStatuses) > 0

	var startStr, endStr string
	if !req.StartTime.IsZero() {
		startStr = req.StartTime.UTC().Format("2006-01-02 15:04:05")
	}
	if !req.EndTime.IsZero() {
		endStr = req.EndTime.UTC().Format("2006-01-02 15:04:05")
	}

	if includePending {
		var pendingConditions []string
		var pendingParams []any
		pendingConditions = append(pendingConditions, "status = 'pending'")
		if startStr != "" {
			pendingConditions = append(pendingConditions, "datetime(created_at) >= datetime(?)")
			pendingParams = append(pendingParams, startStr)
		}
		if endStr != "" {
			pendingConditions = append(pendingConditions, "datetime(created_at) <= datetime(?)")
			pendingParams = append(pendingParams, endStr)
		}
		if len(req.Queues) > 0 {
			pendingConditions = append(pendingConditions, "queue IN ?")
			pendingParams = append(pendingParams, req.Queues)
		}
		q := fmt.Sprintf(`select queue,
			status,
			%s as created_at_bucket,
			'' as ran_at_bucket,
			count(*) as count,
			0 as latency
			from queue_jobs
			where %s
			group by queue, status, created_at_bucket`,
			createdAtTrunc, strings.Join(pendingConditions, " AND "))
		queries = append(queries, q)
		params = append(params, pendingParams...)
	}

	if includeNonPending {
		var nonPendingConditions []string
		var nonPendingParams []any
		if len(nonPendingStatuses) > 0 {
			nonPendingConditions = append(nonPendingConditions, "status IN ?")
			nonPendingParams = append(nonPendingParams, nonPendingStatuses)
		} else {
			nonPendingConditions = append(nonPendingConditions, "status != 'pending'")
		}
		if startStr != "" {
			nonPendingConditions = append(nonPendingConditions, "datetime(ran_at) >= datetime(?)")
			nonPendingParams = append(nonPendingParams, startStr)
		}
		if endStr != "" {
			nonPendingConditions = append(nonPendingConditions, "datetime(ran_at) <= datetime(?)")
			nonPendingParams = append(nonPendingParams, endStr)
		}
		if len(req.Queues) > 0 {
			nonPendingConditions = append(nonPendingConditions, "queue IN ?")
			nonPendingParams = append(nonPendingParams, req.Queues)
		}
		q := fmt.Sprintf(`select queue,
			status,
			%s as created_at_bucket,
			coalesce(%s, '') as ran_at_bucket,
			count(*) as count,
			coalesce(sum(strftime('%%%%s', ran_at) - strftime('%%%%s', run_after)), 0) as latency
			from queue_jobs
			where %s
			group by queue, status, created_at_bucket, ran_at_bucket`,
			createdAtTrunc, ranAtTrunc, strings.Join(nonPendingConditions, " AND "))
		queries = append(queries, q)
		params = append(params, nonPendingParams...)
	}

	if len(queries) == 0 {
		return nil, nil
	}

	var sqlQuery string
	if len(queries) == 1 {
		sqlQuery = queries[0] + " order by queue, status, created_at_bucket, ran_at_bucket"
	} else {
		sqlQuery = fmt.Sprintf(`select queue, status, created_at_bucket, ran_at_bucket, count, latency
			from (
				%s
				UNION ALL
				%s
			)
			order by queue, status, created_at_bucket, ran_at_bucket`,
			queries[0], queries[1])
	}

	var rawResult []rawBucket
	if err := c.db.WithContext(ctx).Raw(sqlQuery, params...).Scan(&rawResult).Error; err != nil {
		return nil, err
	}

	result := make([]Bucket, len(rawResult))
	for i, raw := range rawResult {
		b, bucketErr := raw.bucket()
		if bucketErr != nil {
			return nil, bucketErr
		}
		result[i] = b
	}

	return result, nil
}

type rawBucket struct {
	Queue       string
	Status      model.QueueJobStatus
	Bucket      string `gorm:"column:created_at_bucket"`
	RanAtBucket string `gorm:"column:ran_at_bucket"`
	Count       uint
	Latency     model.Duration
}

func (b rawBucket) bucket() (Bucket, error) {
	var latency *time.Duration

	if b.Latency > 0 {
		l := time.Duration(b.Latency)
		latency = &l
	}

	// SQLite strftime returns "YYYY-MM-DD HH:MM:SS"; parse as UTC.
	bucketTime, parseErr := time.Parse("2006-01-02 15:04:05", b.Bucket)
	if parseErr != nil {
		return Bucket{}, fmt.Errorf("failed to parse bucket time %q: %w", b.Bucket, parseErr)
	}

	var ranAtTime time.Time
	if b.RanAtBucket != "" {
		ranAtTime, parseErr = time.Parse("2006-01-02 15:04:05", b.RanAtBucket)
		if parseErr != nil {
			return Bucket{}, fmt.Errorf("failed to parse ran_at_bucket time %q: %w", b.RanAtBucket, parseErr)
		}
	}

	return Bucket{
		Queue:       b.Queue,
		Status:      b.Status,
		Bucket:      bucketTime,
		RanAtBucket: ranAtTime,
		Count:       b.Count,
		Latency:     latency,
	}, nil
}
