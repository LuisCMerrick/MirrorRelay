package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/database"
)

type pausedSessionStore struct {
	SessionStore
	once   sync.Once
	read   chan struct{}
	resume chan struct{}
}

func (s *pausedSessionStore) GetSession(ctx context.Context, key string) (int64, string, string, string, time.Time, error) {
	id, username, role, csrf, expires, err := s.SessionStore.GetSession(ctx, key)
	s.once.Do(func() { close(s.read); <-s.resume })
	return id, username, role, csrf, expires, err
}

func TestRevokedSessionCannotBeRestoredByRefresh(t *testing.T) {
	for _, operation := range []string{"revoke", "logout", "password-reset"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			db, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.CreateUser(ctx, "test-admin", "unused-test-hash", "admin"); err != nil {
				t.Fatal(err)
			}
			user, err := db.UserByName(ctx, "test-admin")
			if err != nil {
				t.Fatal(err)
			}
			paused := &pausedSessionStore{SessionStore: db, read: make(chan struct{}), resume: make(chan struct{})}
			var resumeOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(paused.resume) }) }
			defer resume()
			sessions := NewSessions(paused, time.Hour)
			session, err := sessions.Create(user.ID, user.Username, user.Role)
			if err != nil {
				t.Fatal(err)
			}
			key := sessionKey(session.ID)
			if err := db.PutSession(ctx, key, user.ID, user.Username, user.Role, session.CSRFToken, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "https://admin.example/admin/api/v1/auth/session", nil)
			req.AddCookie(&http.Cookie{Name: CookieName, Value: session.ID})
			done := make(chan bool, 1)
			go func() { _, ok := sessions.Get(req); done <- ok }()
			select {
			case <-paused.read:
			case <-time.After(5 * time.Second):
				t.Fatal("session read did not start")
			}
			switch operation {
			case "revoke":
				err = sessions.RevokeUser(ctx, user.ID, "")
			case "logout":
				sessions.Delete(req)
			case "password-reset":
				err = db.ResetPasswordAndSessions(ctx, user.ID, "replacement-test-hash")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, _, err := db.GetSession(ctx, key); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("revocation did not delete row: %v", err)
			}
			resume()
			select {
			case ok := <-done:
				if ok {
					t.Fatal("revoked in-flight session was accepted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("session refresh did not finish")
			}
			if _, ok := sessions.Get(req); ok {
				t.Fatal("revoked token was restored")
			}
			if _, _, _, _, _, err := db.GetSession(ctx, key); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("revoked row reappeared: %v", err)
			}
		})
	}
}

func TestSessionRefreshPreservesLiveSessions(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			sessions := NewSessions(nil, time.Hour)
			if persistent {
				db, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if err := db.CreateUser(context.Background(), "test-admin", "hash", "admin"); err != nil {
					t.Fatal(err)
				}
				sessions.store = db
			}
			session, err := sessions.Create(1, "test-admin", "admin")
			if err != nil {
				t.Fatal(err)
			}
			session.ExpiresAt = time.Now().Add(time.Minute)
			sessions.items[session.ID] = session
			if sessions.store != nil {
				if err := sessions.store.PutSession(context.Background(), sessionKey(session.ID), 1, session.Username, session.Role, session.CSRFToken, session.ExpiresAt); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest("GET", "https://admin.example/admin/", nil)
			req.AddCookie(&http.Cookie{Name: CookieName, Value: session.ID})
			refreshed, ok := sessions.Get(req)
			if !ok || time.Until(refreshed.ExpiresAt) < 59*time.Minute || refreshed.CSRFToken != session.CSRFToken {
				t.Fatalf("live refresh failed: %+v %v", refreshed, ok)
			}
		})
	}
}
