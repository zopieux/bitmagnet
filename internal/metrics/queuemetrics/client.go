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

	params := []any{}

	var conditions []string
	if !req.StartTime.IsZero() {
		conditions = append(conditions, "(status != 'pending' OR datetime(created_at) >= datetime(?))")
		conditions = append(conditions, "(status = 'pending' OR datetime(ran_at) >= datetime(?))")
		start := req.StartTime.UTC().Format("2006-01-02 15:04:05")
		params = append(params, start, start)
	}

	if !req.EndTime.IsZero() {
		conditions = append(conditions, "(status != 'pending' OR datetime(created_at) <= datetime(?))")
		conditions = append(conditions, "(status = 'pending' OR datetime(ran_at) <= datetime(?))")
		end := req.EndTime.UTC().Format("2006-01-02 15:04:05")
		params = append(params, end, end)
	}

	if req.Queues != nil {
		conditions = append(conditions, "queue IN ?")
		params = append(params, req.Queues)
	}

	if req.Statuses != nil {
		conditions = append(conditions, "status IN ?")
		params = append(params, req.Statuses)
	}

	conditionClause := ""
	if len(conditions) > 0 {
		conditionClause = "WHERE (" + strings.Join(conditions, " AND ") + ")"
	}

	var rawResult []rawBucket
	createdAtTrunc := fmt.Sprintf(truncExpr, "created_at")
	ranAtTrunc := fmt.Sprintf(truncExpr, "ran_at")
	if err := c.db.WithContext(ctx).Raw(fmt.Sprintf(`select queue,
        status,
        %s as created_at_bucket,
        coalesce(%s, '') as ran_at_bucket,
        count(*) as count,
        coalesce(sum(strftime('%%s', ran_at) - strftime('%%s', run_after)), 0) as latency
        from queue_jobs
       `, createdAtTrunc, ranAtTrunc)+
		conditionClause+
		`
    group by queue, status, created_at_bucket, ran_at_bucket
    order by queue, status, created_at_bucket, ran_at_bucket`,
		params...,
	).Scan(&rawResult).Error; err != nil {
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
