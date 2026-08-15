package cachectl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/model"
)

type Store interface {
	ListCacheGenerations(context.Context) ([]model.CacheGeneration, error)
	PurgeCache(context.Context, string, int64, string, string) (model.PurgeJob, error)
	PendingPurgeJobs(context.Context, int) ([]model.PurgeJob, error)
	UpdatePurgeJob(context.Context, int64, string, int64, string) error
}

type usage struct {
	Bytes     int64     `json:"bytes"`
	Files     int64     `json:"files"`
	ScannedAt time.Time `json:"scanned_at"`
	Error     string    `json:"error,omitempty"`
}

type Manager struct {
	cfg   config.Config
	store Store

	mu           sync.RWMutex
	loaded       bool
	global       int64
	repositories map[int64]int64
	objects      map[string]int64
	lastReclaim  model.PurgeJob
	usage        usage
	reclaimBytes map[int64]int64
}

func New(cfg config.Config, store Store) *Manager {
	return &Manager{
		cfg:          cfg,
		store:        store,
		global:       1,
		repositories: make(map[int64]int64),
		objects:      make(map[string]int64),
		reclaimBytes: make(map[int64]int64),
	}
}

func (m *Manager) Load(ctx context.Context) error {
	values, err := m.store.ListCacheGenerations(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.global = 1
	m.repositories = make(map[int64]int64)
	m.objects = make(map[string]int64)
	for _, value := range values {
		switch value.Scope {
		case "global":
			m.global = value.Generation
		case "repository":
			m.repositories[value.RepositoryID] = value.Generation
		case "object":
			m.objects[objectMapKey(value.RepositoryID, value.ObjectID)] = value.Generation
		}
	}
	m.loaded = true
	return nil
}

func CanonicalObjectID(repositoryID int64, upstreamIdentity, path, rawQuery string) string {
	query := canonicalQuery(rawQuery)
	sum := sha256.Sum256([]byte(fmt.Sprintf("v5\x00%d\x00%s\x00%s\x00%s", repositoryID, upstreamIdentity, cleanPath(path), query)))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) Key(_ context.Context, repositoryID int64, upstreamIdentity, path, rawQuery string) (string, string, error) {
	objectID := CanonicalObjectID(repositoryID, upstreamIdentity, path, rawQuery)
	m.mu.RLock()
	loaded := m.loaded
	global := m.global
	repository := m.repositories[repositoryID]
	object := m.objects[objectMapKey(repositoryID, objectID)]
	m.mu.RUnlock()
	if !loaded {
		return "", "", errors.New("cache generation state is not loaded")
	}
	if repository == 0 {
		repository = 1
	}
	if object == 0 {
		object = 1
	}
	return fmt.Sprintf("v5:%d:%d:%d:%s", global, repository, object, objectID), objectID, nil
}

func (m *Manager) Purge(ctx context.Context, scope string, repositoryID int64, objectID, operator string) (model.PurgeJob, error) {
	job, err := m.store.PurgeCache(ctx, scope, repositoryID, objectID, operator)
	if err != nil {
		return job, err
	}
	m.mu.Lock()
	switch scope {
	case "global":
		m.global = job.NewGeneration
	case "repository":
		m.repositories[repositoryID] = job.NewGeneration
	case "object":
		m.objects[objectMapKey(repositoryID, objectID)] = job.NewGeneration
	}
	m.mu.Unlock()
	return job, nil
}

func (m *Manager) StartReclaimer(ctx context.Context) {
	go func() {
		reclaimTicker := time.NewTicker(5 * time.Second)
		usageTicker := time.NewTicker(30 * time.Second)
		defer reclaimTicker.Stop()
		defer usageTicker.Stop()
		m.scanUsage()
		m.reclaim(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-reclaimTicker.C:
				m.reclaim(ctx)
			case <-usageTicker.C:
				m.scanUsage()
			}
		}
	}()
}

// Generation-based invalidation is complete before this worker runs. Nginx
// owns its cache index, so the worker observes its inactive/max_size cleanup
// window instead of deleting guessed cache paths.
func (m *Manager) reclaim(ctx context.Context) {
	jobs, err := m.store.PendingPurgeJobs(ctx, 32)
	if err != nil {
		return
	}
	for _, job := range jobs {
		m.mu.Lock()
		baseline, tracked := m.reclaimBytes[job.ID]
		if !tracked {
			baseline = m.usage.Bytes
			m.reclaimBytes[job.ID] = baseline
		}
		m.mu.Unlock()
		if job.ReclaimState == "pending" {
			if err := m.store.UpdatePurgeJob(ctx, job.ID, "running", 0, ""); err != nil {
				continue
			}
			job.ReclaimState = "running"
		}
		if time.Since(job.CreatedAt) < m.cfg.Cache.Inactive+m.cfg.Cache.CleanupInterval {
			continue
		}
		m.scanUsage()
		m.mu.RLock()
		current := m.usage
		m.mu.RUnlock()
		if current.Error != "" {
			_ = m.store.UpdatePurgeJob(ctx, job.ID, "failed", 0, current.Error)
			continue
		}
		reclaimed := baseline - current.Bytes
		if reclaimed < 0 {
			reclaimed = 0
		}
		if err := m.store.UpdatePurgeJob(ctx, job.ID, "completed", reclaimed, ""); err != nil {
			_ = m.store.UpdatePurgeJob(ctx, job.ID, "failed", 0, err.Error())
			continue
		}
		job.ReclaimState = "completed"
		job.ReclaimedBytes = reclaimed
		job.Error = ""
		job.UpdatedAt = time.Now()
		m.mu.Lock()
		m.lastReclaim = job
		delete(m.reclaimBytes, job.ID)
		m.mu.Unlock()
	}
}

func (m *Manager) scanUsage() {
	current := usage{ScannedAt: time.Now()}
	err := filepath.WalkDir(m.cfg.Cache.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		current.Files++
		current.Bytes += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		current.Error = err.Error()
	}
	m.mu.Lock()
	m.usage = current
	m.mu.Unlock()
}

func (m *Manager) Summary() map[string]any {
	m.mu.RLock()
	last := m.lastReclaim
	currentUsage := m.usage
	global := m.global
	repositoryGenerations := make(map[int64]int64, len(m.repositories))
	for key, value := range m.repositories {
		repositoryGenerations[key] = value
	}
	m.mu.RUnlock()
	return map[string]any{
		"path":                   m.cfg.Cache.Path,
		"maximum_bytes":          m.cfg.Cache.MaxSizeBytes,
		"max_bytes":              m.cfg.Cache.MaxSizeBytes,
		"maximum_files":          m.cfg.Cache.MaxFiles,
		"minimum_free_bytes":     m.cfg.Cache.MinimumFreeBytes,
		"inactive_seconds":       int64(m.cfg.Cache.Inactive.Seconds()),
		"logical_purge":          "generation-v5",
		"physical_reclaim":       "asynchronous-nginx-cache-manager",
		"global_generation":      global,
		"repository_generations": repositoryGenerations,
		"bytes":                  currentUsage.Bytes,
		"files":                  currentUsage.Files,
		"usage_scanned_at":       currentUsage.ScannedAt,
		"usage_error":            currentUsage.Error,
		"last_reclaim":           last,
	}
}

func canonicalQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	return values.Encode()
}

func cleanPath(value string) string {
	if value == "" {
		return "/"
	}
	parts := strings.Split(value, "/")
	out := parts[:0]
	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return "/" + strings.Join(out, "/")
}

func objectMapKey(repositoryID int64, objectID string) string {
	return strconv.FormatInt(repositoryID, 10) + ":" + objectID
}
