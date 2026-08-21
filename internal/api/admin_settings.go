package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/appearance"
	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

var webSettingsFileOnly = []string{
	"server.frontend_socket",
	"server.frontend_socket_mode",
	"runtime.*",
	"ingress.snippet_path",
	"redirect.pin_validated_ip",
	"tls.certificate",
	"tls.private_key",
	"database.path",
	"cache.path",
	"logging.path",
	"admin.*",
	"upstream_nginx.binary",
	"upstream_nginx.prefix",
	"upstream_nginx.pid",
	"upstream_nginx.log_path",
	"upstream_nginx.upstream_socket",
	"upstream_nginx.upstream_socket_mode",
	"upstream_nginx.ca_bundle",
}

func normalizeForComparison(w config.WebSettings) config.WebSettings {
	w.UIEnhancement = model.UIEnhancementConfig{}
	w.Warmup = model.WarmupConfig{}
	if w.Security.AdminCIDRs == nil {
		w.Security.AdminCIDRs = []string{}
	}
	normalizeDuration := func(s *string) {
		if *s != "" {
			if d, err := time.ParseDuration(*s); err == nil {
				*s = d.String()
			}
		}
	}
	normalizeDuration(&w.HTTP.ReadTimeout)
	normalizeDuration(&w.HTTP.WriteTimeout)
	normalizeDuration(&w.HTTP.IdleTimeout)
	normalizeDuration(&w.Cache.Inactive)
	normalizeDuration(&w.Cache.MetadataTTL)
	normalizeDuration(&w.Cache.PackageTTL)
	normalizeDuration(&w.Cache.CleanupInterval)
	normalizeDuration(&w.Cache.WaitForFill)
	normalizeDuration(&w.Security.SessionTimeout)
	normalizeDuration(&w.Security.LoginWindow)
	normalizeDuration(&w.Transport.DialTimeout)
	normalizeDuration(&w.Transport.KeepAlive)
	normalizeDuration(&w.Transport.TLSHandshakeTimeout)
	normalizeDuration(&w.Transport.ResponseHeaderTimeout)
	normalizeDuration(&w.Transport.IdleConnTimeout)
	normalizeDuration(&w.Health.WorkerInterval)
	normalizeDuration(&w.Shutdown.GracePeriod)
	normalizeDuration(&w.UpstreamNginx.ResolverRefresh)
	normalizeDuration(&w.UpstreamNginx.RestartWindow)
	normalizeDuration(&w.UpstreamNginx.RestartInitialBackoff)
	normalizeDuration(&w.UpstreamNginx.RestartMaxBackoff)
	if w.Webhook != nil {
		webhook := *w.Webhook
		webhook.Events = append([]string{}, w.Webhook.Events...)
		w.Webhook = &webhook
		normalizeDuration(&w.Webhook.Timeout)
	}
	return w
}

func webSettingsEqual(left, right config.WebSettings) bool {
	leftNorm := normalizeForComparison(left)
	rightNorm := normalizeForComparison(right)
	leftJSON, leftErr := json.Marshal(leftNorm)
	rightJSON, rightErr := json.Marshal(rightNorm)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func (s *Server) webSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	current := config.WebSettingsFrom(s.cfg)
	fromFile := config.WebSettingsFrom(s.fileConfig)
	stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if !found {
		restartRequired := !webSettingsEqual(fromFile, current)
		redactWebSettingsForRole(&fromFile, session.Role)
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": fromFile, "source": "configuration_file", "restart_required": restartRequired, "file_only": webSettingsFileOnly,
		})
		return
	}
	settings, err := config.DecodeWebSettings([]byte(stored))
	if err != nil {
		writeInternal(w, err)
		return
	}
	candidate, err := settings.Apply(s.fileConfig)
	if err != nil {
		writeInternal(w, err)
		return
	}
	// Normalize newly introduced optional sections so older persisted settings
	// documents remain editable without losing their YAML-backed values.
	settings = config.WebSettingsFrom(candidate)
	restartRequired := !webSettingsEqual(settings, current)
	redactWebSettingsForRole(&settings, session.Role)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings, "source": "web_ui", "restart_required": restartRequired, "file_only": webSettingsFileOnly,
	})
}

func redactWebSettingsForRole(settings *config.WebSettings, role string) {
	if role != "admin" && settings.Webhook != nil {
		if settings.Webhook.URL != "" {
			settings.Webhook.URL = redactedValue
		}
		settings.Webhook.Secret = ""
	}
}

func (s *Server) updateWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input config.WebSettings
	if decodeJSON(w, r, &input) != nil {
		return
	}
	candidate, err := input.Apply(s.fileConfig)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_update", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	normalized := config.WebSettingsFrom(candidate)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if err := s.store.PutSetting(r.Context(), config.WebSettingsKey, string(encoded)); err != nil {
		writeInternal(w, err)
		return
	}
	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_update", "configuration", "saved Web UI override", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": normalized, "source": "web_ui", "restart_required": restartRequired, "file_only": webSettingsFileOnly,
	})
}

func (s *Server) resetWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := s.store.DeleteSetting(r.Context(), config.WebSettingsKey); err != nil {
		writeInternal(w, err)
		return
	}
	fromFile := config.WebSettingsFrom(s.fileConfig)
	_ = s.audit(r, session.Username, "settings_reset", "configuration", "restore configuration file values after restart", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": fromFile, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, config.WebSettingsFrom(s.cfg)), "file_only": webSettingsFileOnly,
	})
}

func (s *Server) updateAppearance(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input model.UIEnhancementConfig
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if err := config.ValidateUIEnhancement(&input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if err := s.store.PutSetting(r.Context(), database.AppearanceSettingsKey, string(encoded)); err != nil {
		writeInternal(w, err)
		return
	}
	if s.appearance == nil {
		s.appearance = appearance.New(s.cfg.UIEnhancement)
	}
	s.appearance.Store(input)
	_ = s.audit(r, session.Username, "appearance_update", "appearance", "updated UI appearance settings", true)
	writeJSON(w, http.StatusOK, input)
}

func (s *Server) resetAppearance(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := s.store.DeleteSetting(r.Context(), database.AppearanceSettingsKey); err != nil {
		writeInternal(w, err)
		return
	}
	if s.appearance == nil {
		s.appearance = appearance.New(s.cfg.UIEnhancement)
	}
	s.appearance.Store(s.fileConfig.UIEnhancement)
	_ = s.audit(r, session.Username, "appearance_reset", "appearance", "reset UI appearance settings to defaults", true)
	writeJSON(w, http.StatusOK, s.appearanceConfig())
}
