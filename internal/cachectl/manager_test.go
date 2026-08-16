package cachectl

import (
	"context"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type memoryStore struct {
	values []model.CacheGeneration
	next   model.PurgeJob
}

func (s *memoryStore) ListCacheGenerations(context.Context) ([]model.CacheGeneration, error) {
	return s.values, nil
}

func (s *memoryStore) PurgeCache(_ context.Context, scope string, repositoryID int64, objectID, operator string) (model.PurgeJob, error) {
	s.next = model.PurgeJob{ID: s.next.ID + 1, Scope: scope, RepositoryID: repositoryID, ObjectID: objectID,
		OldGeneration: 1, NewGeneration: 2, ReclaimState: "pending", Operator: operator}
	return s.next, nil
}

func (s *memoryStore) PendingPurgeJobs(context.Context, int) ([]model.PurgeJob, error) {
	return nil, nil
}

func (s *memoryStore) UpdatePurgeJob(context.Context, int64, string, int64, string) error {
	return nil
}

func TestCanonicalObjectIDNormalizesQueryAndPath(t *testing.T) {
	a := CanonicalObjectID(7, "u1", "//pool/./file.deb", "b=2&a=1")
	b := CanonicalObjectID(7, "u1", "/pool/file.deb", "a=1&b=2")
	if a != b {
		t.Fatalf("canonical identities differ: %s %s", a, b)
	}
	if a == CanonicalObjectID(8, "u1", "/pool/file.deb", "a=1&b=2") {
		t.Fatal("repository id not bound")
	}
	ordered := CanonicalObjectID(7, "u1", "/pool/file.deb", "tag=stable&tag=edge")
	reversed := CanonicalObjectID(7, "u1", "/pool/file.deb", "tag=edge&tag=stable")
	if ordered == reversed {
		t.Fatal("repeated query-value order was discarded")
	}
}

func TestKeyUsesLoadedGenerationSnapshotAndPurgeUpdatesAtomically(t *testing.T) {
	store := &memoryStore{values: []model.CacheGeneration{{Scope: "global", Generation: 4}, {Scope: "repository", RepositoryID: 7, Generation: 9}}}
	cfg := config.Default()
	cfg.Cache.Path = "/nonexistent/mirrorrelay-cache-test"
	manager := New(cfg, store)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	key, objectID, err := manager.Key(context.Background(), 7, "u1", "/file", "")
	if err != nil || !strings.HasPrefix(key, "v5:4:9:1:") {
		t.Fatalf("unexpected key %q object %q error %v", key, objectID, err)
	}
	job, err := manager.Purge(context.Background(), "object", 7, objectID, "admin")
	if err != nil || job.NewGeneration != 2 {
		t.Fatalf("purge failed: %#v %v", job, err)
	}
	key, _, _ = manager.Key(context.Background(), 7, "u1", "/file", "")
	if !strings.HasPrefix(key, "v5:4:9:2:") {
		t.Fatalf("object purge was not visible in hot-path snapshot: %s", key)
	}
}
