package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshSessionOnlyUpdatesExistingUnexpiredRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateUser(ctx, "admin", "test-hash", "admin"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, state := range []string{"live", "expired", "missing", "revoked"} {
		t.Run(state, func(t *testing.T) {
			expires := now.Add(time.Minute)
			if state == "expired" {
				expires = now.Add(-time.Minute)
			}
			if state != "missing" {
				if err := store.PutSession(ctx, state, 1, "admin", "admin", "csrf", expires); err != nil {
					t.Fatal(err)
				}
			}
			if state == "revoked" {
				if err := store.DeleteSession(ctx, state); err != nil {
					t.Fatal(err)
				}
			}
			updated, err := store.RefreshSession(ctx, state, now, now.Add(time.Hour))
			if err != nil || updated != (state == "live") {
				t.Fatalf("refresh(%s) = %v, %v", state, updated, err)
			}
			_, _, _, _, actual, err := store.GetSession(ctx, state)
			switch state {
			case "live":
				if err != nil || !actual.Equal(now.Add(time.Hour)) {
					t.Fatalf("live expiry = %v, %v", actual, err)
				}
			case "expired":
				if err != nil || !actual.Equal(expires) {
					t.Fatalf("expired row changed: %v, %v", actual, err)
				}
			default:
				if err == nil {
					t.Fatal("refresh created a row")
				}
			}
		})
	}
}
