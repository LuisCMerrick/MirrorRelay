package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type auditRecorderAdapter struct {
	server *Server
}

func (a *auditRecorderAdapter) Record(user, action, object, detail string, ok bool) {
	if a.server != nil && a.server.store != nil {
		_ = a.server.store.AddAudit(context.Background(), model.AuditEntry{
			Time:      time.Now(),
			Username:  user,
			ClientIP:  "127.0.0.1",
			Action:    action,
			Object:    object,
			Detail:    detail,
			Succeeded: ok,
		})
	}
}

type clientExample struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Format      string `json:"format,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	return decodeJSONLimit(w, r, out, 1<<20)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, out any, maximum int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		writeError(w, 400, "invalid JSON: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeInternal(w http.ResponseWriter, _ error) { writeError(w, 500, "internal server error") }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}
