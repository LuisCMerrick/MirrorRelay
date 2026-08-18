package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if !s.requireRole(w, session, "admin") {
		return
	}
	var in struct {
		URL    string `json:"url"`
		Secret string `json:"secret"`
	}
	_ = decodeJSON(w, r, &in)

	cfg := s.cfg.Webhook
	if in.URL != "" {
		cfg.URL = in.URL
	}
	if in.Secret != "" {
		cfg.Secret = in.Secret
	}
	if cfg.URL == "" {
		writeError(w, 400, "webhook URL is required")
		return
	}

	payload := model.WebhookPayload{
		Event:     "test",
		Timestamp: time.Now().UTC(),
		Title:     "MirrorRelay Webhook Test",
		Message:   fmt.Sprintf("This is a test notification triggered by %s from MirrorRelay Web UI.", session.Username),
		Data: map[string]any{
			"operator": session.Username,
			"version":  s.build.Version,
		},
	}

	if err := s.webhook.SendSync(r.Context(), payload); err != nil {
		_ = s.audit(r, session.Username, "webhook_test", "webhook", err.Error(), false)
		writeError(w, 502, fmt.Sprintf("failed to deliver test webhook: %v", err))
		return
	}

	_ = s.audit(r, session.Username, "webhook_test", "webhook", cfg.URL, true)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "test webhook sent successfully"})
}

func (s *Server) dispatchAlert(event, title, message string, data map[string]any) {
	if s.webhook != nil {
		s.webhook.Dispatch(event, title, message, data)
	}
}
