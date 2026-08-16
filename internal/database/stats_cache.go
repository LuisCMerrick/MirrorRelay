package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

func (s *Store) LoadStatsHourly(ctx context.Context, since string) ([]stats.PersistentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hour,mirror_id,requests,bytes,upstream_bytes,cache_bytes,cache_hits,cache_misses,upstream_errors,status_2xx,status_3xx,status_4xx,status_5xx FROM stats_hourly WHERE hour>=? ORDER BY hour,mirror_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []stats.PersistentRecord
	for rows.Next() {
		var record stats.PersistentRecord
		if err := rows.Scan(&record.Hour, &record.MirrorID, &record.Counters.Requests, &record.Counters.Bytes,
			&record.Counters.UpstreamBytes, &record.Counters.CacheBytes, &record.Counters.CacheHits,
			&record.Counters.CacheMisses, &record.Counters.UpstreamErrors, &record.Counters.Status2xx,
			&record.Counters.Status3xx, &record.Counters.Status4xx, &record.Counters.Status5xx); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) SaveStatsHourly(ctx context.Context, records []stats.PersistentRecord, before string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		counters := record.Counters
		if _, err := tx.ExecContext(ctx, `INSERT INTO stats_hourly(hour,mirror_id,requests,bytes,cache_hits,cache_misses,upstream_bytes,cache_bytes,upstream_errors,status_2xx,status_3xx,status_4xx,status_5xx)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(hour,mirror_id) DO UPDATE SET requests=excluded.requests,bytes=excluded.bytes,cache_hits=excluded.cache_hits,cache_misses=excluded.cache_misses,upstream_bytes=excluded.upstream_bytes,cache_bytes=excluded.cache_bytes,upstream_errors=excluded.upstream_errors,status_2xx=excluded.status_2xx,status_3xx=excluded.status_3xx,status_4xx=excluded.status_4xx,status_5xx=excluded.status_5xx`,
			record.Hour, record.MirrorID, counters.Requests, counters.Bytes, counters.CacheHits, counters.CacheMisses,
			counters.UpstreamBytes, counters.CacheBytes, counters.UpstreamErrors, counters.Status2xx,
			counters.Status3xx, counters.Status4xx, counters.Status5xx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stats_hourly WHERE hour<?`, before); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CacheGenerations(ctx context.Context, repositoryID int64, objectID string) (int64, int64, int64, error) {
	global, err := s.cacheGeneration(ctx, "global", 0, "")
	if err != nil {
		return 0, 0, 0, err
	}
	repository, err := s.cacheGeneration(ctx, "repository", repositoryID, "")
	if err != nil {
		return 0, 0, 0, err
	}
	object, err := s.cacheGeneration(ctx, "object", repositoryID, objectID)
	return global, repository, object, err
}

func (s *Store) ListCacheGenerations(ctx context.Context) ([]model.CacheGeneration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope,repository_id,object_id,generation FROM cache_generations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var generations []model.CacheGeneration
	for rows.Next() {
		var generation model.CacheGeneration
		if err := rows.Scan(&generation.Scope, &generation.RepositoryID, &generation.ObjectID, &generation.Generation); err != nil {
			return nil, err
		}
		generations = append(generations, generation)
	}
	return generations, rows.Err()
}

func (s *Store) ListPurgeJobs(ctx context.Context, limit int) ([]model.PurgeJob, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.PurgeJob
	for rows.Next() {
		var job model.PurgeJob
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration,
			&job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated); err != nil {
			return nil, err
		}
		job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) cacheGeneration(ctx context.Context, scope string, repositoryID int64, objectID string) (int64, error) {
	var generation int64
	err := s.db.QueryRowContext(ctx, `SELECT generation FROM cache_generations WHERE scope=? AND repository_id=? AND object_id=?`, scope, repositoryID, objectID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	return generation, err
}

func (s *Store) PurgeCache(ctx context.Context, scope string, repositoryID int64, objectID, operator string) (model.PurgeJob, error) {
	if scope != "global" && scope != "repository" && scope != "object" {
		return model.PurgeJob{}, errors.New("invalid cache purge scope")
	}
	if scope == "global" {
		repositoryID, objectID = 0, ""
	}
	if scope == "repository" {
		objectID = ""
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.PurgeJob{}, err
	}
	defer tx.Rollback()
	old := int64(1)
	err = tx.QueryRowContext(ctx, `SELECT generation FROM cache_generations WHERE scope=? AND repository_id=? AND object_id=?`, scope, repositoryID, objectID).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.PurgeJob{}, err
	}
	next := old + 1
	now := nowText()
	_, err = tx.ExecContext(ctx, `INSERT INTO cache_generations(scope,repository_id,object_id,generation,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(scope,repository_id,object_id) DO UPDATE SET generation=excluded.generation,updated_at=excluded.updated_at`, scope, repositoryID, objectID, next, now)
	if err != nil {
		return model.PurgeJob{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO purge_jobs(scope,repository_id,object_id,old_generation,new_generation,reclaim_state,operator,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?,?)`, scope, repositoryID, objectID, old, next, operator, now, now)
	if err != nil {
		return model.PurgeJob{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.PurgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.PurgeJob{}, err
	}
	return s.PurgeJob(ctx, id)
}

func (s *Store) PurgeJob(ctx context.Context, id int64) (model.PurgeJob, error) {
	var job model.PurgeJob
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration, &job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated)
	job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
	return job, err
}

func (s *Store) PendingPurgeJobs(ctx context.Context, limit int) ([]model.PurgeJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs WHERE reclaim_state IN ('pending','running') ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.PurgeJob
	for rows.Next() {
		var job model.PurgeJob
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration, &job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated); err != nil {
			return nil, err
		}
		job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) UpdatePurgeJob(ctx context.Context, id int64, state string, reclaimed int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE purge_jobs SET reclaim_state=?,reclaimed_bytes=?,error=?,updated_at=? WHERE id=?`, state, reclaimed, message, nowText(), id)
	return err
}
