package api

import (
	"encoding/json"
	"net/http"

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

func webSettingsEqual(left, right config.WebSettings) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func (s *Server) webSettings(w http.ResponseWriter, r *http.Request) {
	current := config.WebSettingsFrom(s.cfg)
	fromFile := config.WebSettingsFrom(s.fileConfig)
	stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": fromFile, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, current), "file_only": webSettingsFileOnly,
		})
		return
	}
	settings, err := config.DecodeWebSettings([]byte(stored))
	if err != nil {
		writeInternal(w, err)
		return
	}
	if _, err := settings.Apply(s.fileConfig); err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings, "source": "web_ui", "restart_required": !webSettingsEqual(settings, current), "file_only": webSettingsFileOnly,
	})
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
	s.cfg.UIEnhancement = input
	_ = s.audit(r, session.Username, "appearance_update", "appearance", "updated UI appearance settings", true)
	writeJSON(w, http.StatusOK, input)
}

func (s *Server) resetAppearance(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := s.store.DeleteSetting(r.Context(), database.AppearanceSettingsKey); err != nil {
		writeInternal(w, err)
		return
	}
	s.cfg.UIEnhancement = s.fileConfig.UIEnhancement
	_ = s.audit(r, session.Username, "appearance_reset", "appearance", "reset UI appearance settings to defaults", true)
	writeJSON(w, http.StatusOK, s.cfg.UIEnhancement)
}
