package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

var ErrWarmupJobNotFound = errors.New("warmup job not found")

func (s *Store) ListWarmupJobs(ctx context.Context) ([]model.WarmupJob, error) {
	const query = `
SELECT w.id, w.mirror_id, m.name, m.slug, w.name, w.cron_expression, w.url_patterns,
       w.status, w.total_items, w.completed_items, w.failed_items, w.bytes_downloaded,
       w.error_message, w.last_run_at, w.next_run_at, w.enabled, w.created_at, w.updated_at
FROM warmup_jobs w
LEFT JOIN mirrors m ON w.mirror_id = m.id
ORDER BY w.id DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list warmup jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.WarmupJob
	for rows.Next() {
		var (
			j                    model.WarmupJob
			mName, mSlug         sql.NullString
			patternsJSON         string
			enabledInt           int
			createdAt, updatedAt string
		)
		err := rows.Scan(
			&j.ID, &j.MirrorID, &mName, &mSlug, &j.Name, &j.CronExpression, &patternsJSON,
			&j.Status, &j.TotalItems, &j.CompletedItems, &j.FailedItems, &j.BytesDownloaded,
			&j.ErrorMessage, &j.LastRunAt, &j.NextRunAt, &enabledInt, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan warmup job: %w", err)
		}
		if mName.Valid {
			j.MirrorName = mName.String
		}
		if mSlug.Valid {
			j.MirrorSlug = mSlug.String
		}
		j.Enabled = enabledInt == 1
		j.CreatedAt = parseTime(createdAt)
		j.UpdatedAt = parseTime(updatedAt)
		_ = json.Unmarshal([]byte(patternsJSON), &j.URLPatterns)
		if j.URLPatterns == nil {
			j.URLPatterns = []string{}
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) GetWarmupJob(ctx context.Context, id int64) (model.WarmupJob, error) {
	const query = `
SELECT w.id, w.mirror_id, m.name, m.slug, w.name, w.cron_expression, w.url_patterns,
       w.status, w.total_items, w.completed_items, w.failed_items, w.bytes_downloaded,
       w.error_message, w.last_run_at, w.next_run_at, w.enabled, w.created_at, w.updated_at
FROM warmup_jobs w
LEFT JOIN mirrors m ON w.mirror_id = m.id
WHERE w.id = ?`

	var (
		j                    model.WarmupJob
		mName, mSlug         sql.NullString
		patternsJSON         string
		enabledInt           int
		createdAt, updatedAt string
	)
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&j.ID, &j.MirrorID, &mName, &mSlug, &j.Name, &j.CronExpression, &patternsJSON,
		&j.Status, &j.TotalItems, &j.CompletedItems, &j.FailedItems, &j.BytesDownloaded,
		&j.ErrorMessage, &j.LastRunAt, &j.NextRunAt, &enabledInt, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WarmupJob{}, ErrWarmupJobNotFound
	}
	if err != nil {
		return model.WarmupJob{}, fmt.Errorf("get warmup job: %w", err)
	}
	if mName.Valid {
		j.MirrorName = mName.String
	}
	if mSlug.Valid {
		j.MirrorSlug = mSlug.String
	}
	j.Enabled = enabledInt == 1
	j.CreatedAt = parseTime(createdAt)
	j.UpdatedAt = parseTime(updatedAt)
	_ = json.Unmarshal([]byte(patternsJSON), &j.URLPatterns)
	if j.URLPatterns == nil {
		j.URLPatterns = []string{}
	}
	return j, nil
}

func (s *Store) CreateWarmupJob(ctx context.Context, job model.WarmupJob) (model.WarmupJob, error) {
	patternsBytes, err := json.Marshal(job.URLPatterns)
	if err != nil {
		patternsBytes = []byte("[]")
	}
	now := time.Now().UTC()
	enabledInt := 0
	if job.Enabled {
		enabledInt = 1
	}
	status := job.Status
	if status == "" {
		status = "idle"
	}

	const query = `
INSERT INTO warmup_jobs (
	mirror_id, name, cron_expression, url_patterns, status, total_items,
	completed_items, failed_items, bytes_downloaded, error_message,
	last_run_at, next_run_at, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, query,
		job.MirrorID, job.Name, job.CronExpression, string(patternsBytes), status,
		job.TotalItems, job.CompletedItems, job.FailedItems, job.BytesDownloaded,
		job.ErrorMessage, job.LastRunAt, job.NextRunAt, enabledInt,
		timeText(now), timeText(now),
	)
	if err != nil {
		return model.WarmupJob{}, fmt.Errorf("create warmup job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.WarmupJob{}, err
	}
	return s.GetWarmupJob(ctx, id)
}

func (s *Store) UpdateWarmupJob(ctx context.Context, job model.WarmupJob) (model.WarmupJob, error) {
	patternsBytes, err := json.Marshal(job.URLPatterns)
	if err != nil {
		patternsBytes = []byte("[]")
	}
	now := time.Now().UTC()
	enabledInt := 0
	if job.Enabled {
		enabledInt = 1
	}

	const query = `
UPDATE warmup_jobs SET
	mirror_id = ?, name = ?, cron_expression = ?, url_patterns = ?,
	enabled = ?, updated_at = ?
WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		job.MirrorID, job.Name, job.CronExpression, string(patternsBytes),
		enabledInt, timeText(now), job.ID,
	)
	if err != nil {
		return model.WarmupJob{}, fmt.Errorf("update warmup job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.WarmupJob{}, err
	}
	if n == 0 {
		return model.WarmupJob{}, ErrWarmupJobNotFound
	}
	return s.GetWarmupJob(ctx, job.ID)
}

func (s *Store) UpdateWarmupJobProgress(ctx context.Context, id int64, status string, total, completed, failed int, downloadedBytes int64, errMsg, lastRun, nextRun string) error {
	now := time.Now().UTC()
	const query = `
UPDATE warmup_jobs SET
	status = ?, total_items = ?, completed_items = ?, failed_items = ?,
	bytes_downloaded = ?, error_message = ?, last_run_at = ?, next_run_at = ?,
	updated_at = ?
WHERE id = ?`

	_, err := s.db.ExecContext(ctx, query,
		status, total, completed, failed, downloadedBytes, errMsg, lastRun, nextRun,
		timeText(now), id,
	)
	return err
}

func (s *Store) DeleteWarmupJob(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM warmup_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete warmup job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrWarmupJobNotFound
	}
	return nil
}
