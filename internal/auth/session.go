package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const CookieName = "repogate_session"

type Session struct {
	ID        string
	UserID    int64
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

type Sessions struct {
	mu    sync.Mutex
	items map[string]Session
	ttl   time.Duration
	store SessionStore
}

type SessionStore interface {
	PutSession(context.Context, string, int64, string, string, time.Time) error
	GetSession(context.Context, string) (int64, string, string, time.Time, error)
	DeleteSession(context.Context, string) error
}

func NewSessions(store SessionStore, ttl time.Duration) *Sessions {
	return &Sessions{items: make(map[string]Session), ttl: ttl, store: store}
}

func randomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Sessions) Create(userID int64, username string) (Session, error) {
	id, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	session := Session{ID: id, UserID: userID, Username: username, CSRFToken: csrf, ExpiresAt: time.Now().Add(s.ttl)}
	if s.store != nil {
		if err := s.store.PutSession(context.Background(), sessionKey(id), userID, username, csrf, session.ExpiresAt); err != nil {
			return Session{}, err
		}
	}
	s.mu.Lock()
	s.items[id] = session
	s.pruneLocked()
	s.mu.Unlock()
	return session, nil
}

func (s *Sessions) Get(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.items[cookie.Value]
	if s.store != nil {
		userID, username, csrf, expires, err := s.store.GetSession(r.Context(), sessionKey(cookie.Value))
		if err == nil {
			session = Session{ID: cookie.Value, UserID: userID, Username: username, CSRFToken: csrf, ExpiresAt: expires}
			ok = true
		} else {
			ok = false
		}
	}
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.items, cookie.Value)
		if s.store != nil {
			_ = s.store.DeleteSession(r.Context(), sessionKey(cookie.Value))
		}
		return Session{}, false
	}
	session.ExpiresAt = time.Now().Add(s.ttl)
	s.items[cookie.Value] = session
	if s.store != nil {
		_ = s.store.PutSession(r.Context(), sessionKey(cookie.Value), session.UserID, session.Username, session.CSRFToken, session.ExpiresAt)
	}
	return session, true
}

func (s *Sessions) Delete(r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		s.mu.Lock()
		delete(s.items, c.Value)
		s.mu.Unlock()
		if s.store != nil {
			_ = s.store.DeleteSession(r.Context(), sessionKey(c.Value))
		}
	}
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Sessions) SetCookie(w http.ResponseWriter, session Session) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: session.ID, Path: "/", Expires: session.ExpiresAt, MaxAge: int(s.ttl.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (s *Sessions) pruneLocked() {
	now := time.Now()
	for k, v := range s.items {
		if now.After(v.ExpiresAt) {
			delete(s.items, k)
		}
	}
}

type LoginLimiter struct {
	mu     sync.Mutex
	items  map[string]*attempt
	window time.Duration
	max    int
}
type attempt struct {
	start    time.Time
	failures int
}

func NewLoginLimiter(window time.Duration, max int) *LoginLimiter {
	return &LoginLimiter{items: make(map[string]*attempt), window: window, max: max}
}
func (l *LoginLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.items[key]
	if a == nil {
		return true
	}
	if time.Since(a.start) > l.window {
		delete(l.items, key)
		return true
	}
	return a.failures < l.max
}
func (l *LoginLimiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.items[key]
	if a == nil || time.Since(a.start) > l.window {
		l.items[key] = &attempt{start: time.Now(), failures: 1}
		return
	}
	a.failures++
}
func (l *LoginLimiter) Success(key string) { l.mu.Lock(); delete(l.items, key); l.mu.Unlock() }
