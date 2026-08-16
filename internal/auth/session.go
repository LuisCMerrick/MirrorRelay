package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const CookieName = "mirrorrelay_session"

type Session struct {
	ID        string
	UserID    int64
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

type Sessions struct {
	mu        sync.RWMutex
	items     map[string]Session
	ttl       time.Duration
	adminPath string
	store     SessionStore
}

type SessionStore interface {
	PutSession(context.Context, string, int64, string, string, time.Time) error
	GetSession(context.Context, string) (int64, string, string, time.Time, error)
	DeleteSession(context.Context, string) error
	DeleteUserSessions(context.Context, int64, ...string) error
}

func NewSessions(store SessionStore, ttl time.Duration) *Sessions {
	return NewSessionsWithPath(store, ttl, "/admin/")
}

func NewSessionsWithPath(store SessionStore, ttl time.Duration, adminPath string) *Sessions {
	if adminPath == "" {
		adminPath = "/"
	}
	return &Sessions{items: make(map[string]Session), ttl: ttl, adminPath: adminPath, store: store}
}

func (s *Sessions) SetAdminPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		path = "/"
	}
	s.adminPath = path
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
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}
	key := sessionKey(cookie.Value)

	s.mu.RLock()
	session, ok := s.items[cookie.Value]
	s.mu.RUnlock()

	if s.store != nil {
		userID, username, csrf, expires, err := s.store.GetSession(r.Context(), key)
		if err == nil {
			session = Session{ID: cookie.Value, UserID: userID, Username: username, CSRFToken: csrf, ExpiresAt: expires}
			ok = true
		} else {
			ok = false
		}
	}

	if !ok || time.Now().After(session.ExpiresAt) {
		s.mu.Lock()
		delete(s.items, cookie.Value)
		s.mu.Unlock()
		if s.store != nil {
			_ = s.store.DeleteSession(r.Context(), key)
		}
		return Session{}, false
	}

	// Only refresh sliding expiration if remaining TTL is less than half the total TTL
	if time.Until(session.ExpiresAt) < s.ttl/2 {
		newExpiry := time.Now().Add(s.ttl)
		session.ExpiresAt = newExpiry
		if s.store != nil {
			if err := s.store.PutSession(r.Context(), key, session.UserID, session.Username, session.CSRFToken, newExpiry); err != nil {
				slog.Warn("failed to refresh session expiration", "user", session.Username, "error", err)
			}
		}
		s.mu.Lock()
		s.items[cookie.Value] = session
		s.mu.Unlock()
	}

	return session, true
}

func (s *Sessions) Delete(r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		s.mu.Lock()
		delete(s.items, c.Value)
		s.mu.Unlock()
		if s.store != nil {
			_ = s.store.DeleteSession(r.Context(), sessionKey(c.Value))
		}
	}
}

func (s *Sessions) RevokeUser(ctx context.Context, userID int64, exceptToken string) error {
	s.mu.Lock()
	for k, v := range s.items {
		if v.UserID == userID && (exceptToken == "" || k != exceptToken) {
			delete(s.items, k)
		}
	}
	s.mu.Unlock()
	if s.store != nil {
		var exceptKey string
		if exceptToken != "" {
			exceptKey = sessionKey(exceptToken)
			return s.store.DeleteUserSessions(ctx, userID, exceptKey)
		}
		return s.store.DeleteUserSessions(ctx, userID)
	}
	return nil
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Sessions) SetCookie(w http.ResponseWriter, session Session) {
	s.mu.RLock()
	cookiePath := s.adminPath
	s.mu.RUnlock()
	if cookiePath == "" {
		cookiePath = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    session.ID,
		Path:     cookiePath,
		Expires:  session.ExpiresAt,
		MaxAge:   int(s.ttl.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	s.mu.RLock()
	cookiePath := s.adminPath
	s.mu.RUnlock()
	if cookiePath == "" {
		cookiePath = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     cookiePath,
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
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
	mu       sync.Mutex
	items    map[string]*attempt
	window   time.Duration
	max      int
	maxItems int
}

type attempt struct {
	start    time.Time
	failures int
	inFlight int
}

func NewLoginLimiter(window time.Duration, max int) *LoginLimiter {
	return &LoginLimiter{
		items:    make(map[string]*attempt),
		window:   window,
		max:      max,
		maxItems: 10000,
	}
}

// Acquire atomically checks and claims a login slot. If allowed, caller MUST invoke
// the returned release function with whether the attempt succeeded.
func (l *LoginLimiter) Acquire(key string) (release func(success bool), allowed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if len(l.items) > l.maxItems {
		l.pruneLocked(now)
	}

	a := l.items[key]
	if a == nil || now.Sub(a.start) > l.window {
		a = &attempt{start: now}
		l.items[key] = a
	}

	if a.failures >= l.max || a.inFlight >= l.max {
		return nil, false
	}

	a.inFlight++
	var once sync.Once
	release = func(success bool) {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			curr := l.items[key]
			if curr == nil {
				return
			}
			curr.inFlight--
			if curr.inFlight < 0 {
				curr.inFlight = 0
			}
			if success {
				delete(l.items, key)
			} else {
				curr.failures++
			}
		})
	}
	return release, true
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
	return a.failures < l.max && a.inFlight < l.max
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

func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	delete(l.items, key)
	l.mu.Unlock()
}

func (l *LoginLimiter) pruneLocked(now time.Time) {
	for k, v := range l.items {
		if now.Sub(v.start) > l.window && v.inFlight == 0 {
			delete(l.items, k)
		}
	}
}
