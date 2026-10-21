package server

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/queue"
	"github.com/bitmagnet-io/bitmagnet/internal/queue/handler"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"gorm.io/gen"
	"gorm.io/gorm"
)

type server struct {
	stopped       chan struct{}
	query         *dao.Query
	handlers      []handler.Handler
	gcInterval    time.Duration
	checkInterval time.Duration
	logger        *zap.SugaredLogger
}

func (s *server) Start(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)

	defer func() {
		if err != nil {
			cancel()
		}
	}()

	for _, h := range s.handlers {
		sh := serverHandler{
			Handler:       h,
			sem:           semaphore.NewWeighted(int64(h.Concurrency)),
			query:         s.query,
			checkInterval: s.checkInterval,
			logger:        s.logger.With("queue", h.Queue),
		}

		go sh.start(ctx)
	}

	go func() {
		select {
		case <-s.stopped:
			cancel()
		case <-ctx.Done():
			return
		}
	}()

	go s.runMaintenance(ctx)

	return
}

// vacuumPagesPerRun bounds the incremental vacuum in the maintenance tick:
// 1024 pages is 4 MiB at the standard 4 KiB page size, so the database file
// converges towards its live size in small steps instead of one blocking
// full VACUUM.
const vacuumPagesPerRun = 1024

// runMaintenance is the single periodic housekeeping tick. All database
// maintenance runs here, batched into one short window: expired queue jobs
// are deleted first (freeing pages), then the FTS index is optimized
// (contents rows are deleted and re-inserted on every reprocess, which
// fragments it), then an incremental vacuum releases the freed pages.
func (s *server) runMaintenance(ctx context.Context) {
	for {
		tx := s.query.QueueJob.WithContext(ctx).
			Where(
				s.query.QueueJob.Status.In(string(model.QueueJobStatusProcessed), string(model.QueueJobStatusFailed)),
			).
			UnderlyingDB().
			Where(
				"datetime(queue_jobs.ran_at, '+' || queue_jobs.archival_duration || ' seconds') < datetime('now')",
			).
			Delete(&model.QueueJob{})
		if tx.Error != nil {
			s.logger.Errorw("error deleting old queue jobs", "error", tx.Error)
		} else if tx.RowsAffected > 0 {
			s.logger.Debugw("deleted old queue jobs", "count", tx.RowsAffected)
		}
		db := s.query.QueueJob.WithContext(ctx).UnderlyingDB()
		if err := db.Exec("INSERT INTO torrent_contents_fts(torrent_contents_fts) VALUES('optimize')").Error; err != nil {
			s.logger.Debugw("fts optimize skipped", "error", err)
		}
		if err := db.Exec("PRAGMA incremental_vacuum(" + strconv.Itoa(vacuumPagesPerRun) + ")").Error; err != nil {
			s.logger.Warnw("incremental vacuum failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.gcInterval):
			continue
		}
	}
}

type serverHandler struct {
	handler.Handler
	sem           *semaphore.Weighted
	query         *dao.Query
	checkInterval time.Duration
	logger        *zap.SugaredLogger
}

func (h *serverHandler) start(ctx context.Context) {
	checkTicker := time.NewTicker(h.checkInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-checkTicker.C:
			if semErr := h.sem.Acquire(ctx, 1); semErr != nil {
				return
			}

			go func() {
				defer h.sem.Release(1)
				jobID, _, err := h.handleJob(ctx)
				// if a job was found, we should check straight away for another job,
				// otherwise we wait for the check interval
				if err == nil && jobID != "" {
					checkTicker.Reset(h.checkInterval)
				}
			}()
		}
	}
}

func (h *serverHandler) handleJob(
	ctx context.Context,
	conds ...gen.Condition,
) (jobID string, processed bool, err error) {
	// No row locking here: SQLite has a single writer, and the database is
	// opened with MaxOpenConns(1), so claims inside this transaction are
	// already serialized.
	//
	// The job is claimed in a short transaction, executed OUTSIDE the
	// transaction (the handler may do its own DB writes, which would deadlock
	// with MaxOpenConns(1) if held inside the tx), then the status is updated
	// in a second short transaction.
	var job *model.QueueJob
	err = h.query.Transaction(func(tx *dao.Query) error {
		var findErr error
		job, findErr = tx.QueueJob.WithContext(ctx).Where(
			append(
				conds,
				h.query.QueueJob.Queue.Eq(h.Queue),
				h.query.QueueJob.Status.In(
					string(model.QueueJobStatusPending),
					string(model.QueueJobStatusRetry),
				),
				h.query.QueueJob.RunAfter.Lte(time.Now()),
			)...,
		).Order(
			h.query.QueueJob.Status.Eq(string(model.QueueJobStatusRetry)),
			h.query.QueueJob.Priority,
			h.query.QueueJob.RunAfter,
		).First()
		if findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}

			return findErr
		}

		jobID = job.ID
		// Push RunAfter far into the future so a concurrent claim (or a
		// crash between claim and status update) doesn't pick up the same job.
		job.RunAfter = time.Now().Add(time.Hour)
		if _, updateErr := tx.QueueJob.WithContext(ctx).Updates(job); updateErr != nil {
			return updateErr
		}

		return nil
	})
	if err != nil || job == nil {
		return jobID, false, err
	}

	var jobErr error
	if job.Deadline.Valid && job.Deadline.Time.Before(time.Now()) {
		jobErr = ErrJobExceededDeadline

		h.logger.Debugw("job deadline is in the past, skipping", "job_id", job.ID)
	} else {
		// check if the job is being retried and increment retry count accordingly
		if job.Status != model.QueueJobStatusPending {
			job.Retries++
		}
		// execute the queue handler of this job (outside the transaction!)
		jobErr = handler.Exec(ctx, h.Handler, *job)
	}

	job.RanAt = sql.NullTime{Time: time.Now(), Valid: true}

	if jobErr != nil {
		h.logger.Errorw("job failed", "error", jobErr)

		if job.Retries < job.MaxRetries {
			job.Status = model.QueueJobStatusRetry
			job.RunAfter = queue.CalculateBackoff(job.Retries)
		} else {
			job.Status = model.QueueJobStatusFailed
		}

		job.Error = model.NewNullString(jobErr.Error())
	} else {
		job.Status = model.QueueJobStatusProcessed
		processed = true
	}

	err = h.query.Transaction(func(tx *dao.Query) error {
		_, updateErr := tx.QueueJob.WithContext(ctx).Updates(job)

		return updateErr
	})
	if err != nil {
		h.logger.Error("error handling job", "error", err)
	} else if processed {
		h.logger.Debugw("job processed", "job_id", jobID)
	}

	return
}

var ErrJobExceededDeadline = errors.New("the job did not complete before its deadline")
