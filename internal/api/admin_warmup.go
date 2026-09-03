package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/warmup"
)

func (s *Server) listWarmupJobs(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, 200, []model.WarmupJob{})
		return
	}
	jobs, err := s.store.ListWarmupJobs(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if jobs == nil {
		jobs = []model.WarmupJob{}
	}
	writeJSON(w, 200, jobs)
}

func (s *Server) createWarmupJob(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var job model.WarmupJob
	if decodeJSON(w, r, &job) != nil {
		return
	}
	if err := prepareWarmupJob(&job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.store.Mirror(r.Context(), job.MirrorID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "mirror_id does not identify an existing repository")
		return
	} else if err != nil {
		writeInternal(w, err)
		return
	}

	created, err := s.store.CreateWarmupJob(r.Context(), job)
	if err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "create", fmt.Sprintf("warmup_job:%d", created.ID), created.Name, true)
	writeJSON(w, 201, created)
}

func (s *Server) warmupJobAction(w http.ResponseWriter, r *http.Request, session auth.Session, raw string) {
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 400, "invalid job id")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, 400, "invalid job id")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			job, err := s.store.GetWarmupJob(r.Context(), id)
			if errors.Is(err, database.ErrWarmupJobNotFound) {
				writeError(w, 404, "warmup job not found")
				return
			}
			if err != nil {
				writeInternal(w, err)
				return
			}
			writeJSON(w, 200, job)
		case http.MethodPut:
			if !s.requireRole(w, session, "admin", "operator") {
				return
			}
			var job model.WarmupJob
			if decodeJSON(w, r, &job) != nil {
				return
			}
			job.ID = id
			if err := prepareWarmupJob(&job); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if _, err := s.store.Mirror(r.Context(), job.MirrorID); errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "mirror_id does not identify an existing repository")
				return
			} else if err != nil {
				writeInternal(w, err)
				return
			}
			updated, err := s.store.UpdateWarmupJob(r.Context(), job)
			if errors.Is(err, database.ErrWarmupJobNotFound) {
				writeError(w, 404, "warmup job not found")
				return
			}
			if err != nil {
				writeInternal(w, err)
				return
			}
			_ = s.audit(r, session.Username, "update", fmt.Sprintf("warmup_job:%d", id), updated.Name, true)
			writeJSON(w, 200, updated)
		case http.MethodDelete:
			if !s.requireRole(w, session, "admin", "operator") {
				return
			}
			if s.warmupEngine != nil {
				_ = s.warmupEngine.CancelJob(id)
			}
			err := s.store.DeleteWarmupJob(r.Context(), id)
			if errors.Is(err, database.ErrWarmupJobNotFound) {
				writeError(w, 404, "warmup job not found")
				return
			}
			if err != nil {
				writeInternal(w, err)
				return
			}
			_ = s.audit(r, session.Username, "delete", fmt.Sprintf("warmup_job:%d", id), "", true)
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "run":
			if !s.requireRole(w, session, "admin", "operator") {
				return
			}
			if s.warmupEngine == nil {
				writeError(w, 400, "warmup engine is not initialized")
				return
			}
			if err := s.warmupEngine.RunJob(r.Context(), id); errors.Is(err, database.ErrWarmupJobNotFound) {
				writeError(w, http.StatusNotFound, "warmup job not found")
				return
			} else if err != nil {
				writeError(w, 400, err.Error())
				return
			}
			_ = s.audit(r, session.Username, "run", fmt.Sprintf("warmup_job:%d", id), "manual trigger", true)
			writeJSON(w, 200, map[string]any{"ok": true, "status": "running"})
		case "cancel":
			if !s.requireRole(w, session, "admin", "operator") {
				return
			}
			if s.warmupEngine == nil {
				writeError(w, 400, "warmup engine is not initialized")
				return
			}
			if err := s.warmupEngine.CancelJob(id); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			_ = s.audit(r, session.Username, "cancel", fmt.Sprintf("warmup_job:%d", id), "manual cancel", true)
			writeJSON(w, 200, map[string]any{"ok": true, "status": "cancelled"})
		default:
			writeError(w, 404, "not found")
		}
		return
	}

	writeError(w, 404, "not found")
}

func prepareWarmupJob(job *model.WarmupJob) error {
	job.Name = strings.TrimSpace(job.Name)
	if job.Name == "" {
		return errors.New("name is required")
	}
	if job.MirrorID <= 0 {
		return errors.New("mirror_id is required")
	}
	patterns, err := warmup.NormalizeURLPatterns(job.URLPatterns)
	if err != nil {
		return err
	}
	job.URLPatterns = patterns
	if err := warmup.ValidateSchedule(job.CronExpression); err != nil {
		return errors.New("invalid cron expression: " + err.Error())
	}
	job.NextRunAt = ""
	next, err := warmup.NextRunAt(job.CronExpression, time.Now().UTC())
	if err != nil {
		return errors.New("invalid cron expression: " + err.Error())
	}
	if !next.IsZero() {
		job.NextRunAt = next.UTC().Format(time.RFC3339)
	}
	return nil
}

func (s *Server) warmupStatus(w http.ResponseWriter, r *http.Request) {
	if s.warmupEngine == nil {
		writeJSON(w, 200, map[string]any{
			"enabled":         false,
			"running_jobs":    0,
			"max_concurrency": 0,
			"total_warmups":   0,
			"bytes_warmed":    0,
		})
		return
	}
	writeJSON(w, 200, s.warmupEngine.Status())
}
