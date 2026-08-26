package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/appearance"
	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func normalizeForComparison(w config.WebSettings) config.WebSettings {
	w.UIEnhancement = model.UIEnhancementConfig{}
	w.Warmup = config.WebWarmupSettings{}
	if w.Security.AdminCIDRs == nil {
		w.Security.AdminCIDRs = []string{}
	}
	if w.Security.TrustedProxyCIDRs == nil {
		w.Security.TrustedProxyCIDRs = []string{}
	}
	if w.Distributed.MutationTokenKeyFiles == nil {
		w.Distributed.MutationTokenKeyFiles = []string{}
	}
	if w.Distributed.Routing.ClientNetworks == nil {
		w.Distributed.Routing.ClientNetworks = []config.ClientNetworkMapping{}
	}
	if w.Distributed.Routing.Regions == nil {
		w.Distributed.Routing.Regions = []config.RegionMapping{}
	}
	if w.Distributed.Nodes == nil {
		w.Distributed.Nodes = []config.DistributedNodeSeed{}
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
	normalizeDuration(&w.Distributed.HealthCheck.Interval)
	normalizeDuration(&w.Distributed.HealthCheck.Timeout)
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

func (s *Server) activeWebConfig() config.Config {
	candidate := s.fileConfig
	if raw, found, err := s.store.Setting(nil, config.WebSettingsKey); err == nil && found {
		if decoded, err := config.DecodeWebSettings([]byte(raw)); err == nil {
			if applied, err := decoded.Apply(s.fileConfig); err == nil {
				candidate = applied
			}
		}
	}
	return candidate
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
			"settings": fromFile, "source": "configuration_file", "restart_required": restartRequired, "file_only": []string{},
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
	settings = config.WebSettingsFrom(candidate)
	restartRequired := !webSettingsEqual(settings, current)
	redactWebSettingsForRole(&settings, session.Role)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings, "source": "web_ui", "restart_required": restartRequired, "file_only": []string{},
	})
}

func redactWebSettingsForRole(settings *config.WebSettings, role string) {
	if settings == nil {
		return
	}
	if role != "admin" {
		if settings.Webhook != nil {
			if settings.Webhook.URL != "" {
				settings.Webhook.URL = redactedValue
			}
			settings.Webhook.Secret = ""
		}
		settings.Distributed.Token = ""
		settings.Distributed.MutationToken = ""
		for i := range settings.Distributed.Nodes {
			settings.Distributed.Nodes[i].MutationToken = ""
		}
	}
}

func (s *Server) updateWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input config.WebSettings
	if decodeJSON(w, r, &input) != nil {
		return
	}
	baseConfig := s.fileConfig
	if stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey); err == nil && found {
		if prevWS, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if appliedPrev, err := prevWS.Apply(s.fileConfig); err == nil {
				baseConfig = appliedPrev
			}
		}
	}

	candidate, err := input.Apply(baseConfig)
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

	oldWS := config.WebSettingsFrom(baseConfig)
	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := normalized
	redactWebSettingsForRole(&safeWS, session.Role)
	safeJSON, _ := json.Marshal(safeWS)

	summary := fmt.Sprintf("%d setting(s) updated", len(diff))
	_, _ = s.store.AddSettingVersion(r.Context(), model.SettingVersion{
		Operator:    session.Username,
		Source:      "web_ui",
		Description: summary,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}, s.cfg.UpstreamNginx.HistoryLimit)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_update", "configuration", summary, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": normalized, "source": "web_ui", "restart_required": restartRequired, "file_only": []string{}, "diff": diff,
	})
}

func (s *Server) resetWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := s.store.DeleteSetting(r.Context(), config.WebSettingsKey); err != nil {
		writeInternal(w, err)
		return
	}
	fromFile := config.WebSettingsFrom(s.fileConfig)
	safeWS := fromFile
	redactWebSettingsForRole(&safeWS, session.Role)
	safeJSON, _ := json.Marshal(safeWS)

	_, _ = s.store.AddSettingVersion(r.Context(), model.SettingVersion{
		Operator:    session.Username,
		Source:      "settings_reset",
		Description: "reset settings to configuration file defaults",
		DiffSummary: "[]",
		Settings:    string(safeJSON),
	}, s.cfg.UpstreamNginx.HistoryLimit)

	_ = s.audit(r, session.Username, "settings_reset", "configuration", "restore configuration file values after restart", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": fromFile, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, config.WebSettingsFrom(s.cfg)), "file_only": []string{},
	})
}

func (s *Server) exportSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	fullBackup := r.URL.Query().Get("full_backup") == "true"
	currentConfig := s.fileConfig
	if stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey); err == nil && found {
		if ws, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if applied, err := ws.Apply(s.fileConfig); err == nil {
				currentConfig = applied
			}
		}
	}

	yamlContent, err := config.ExportYAML(currentConfig, fullBackup)
	if err != nil {
		writeInternal(w, err)
		return
	}

	exportType := "standard"
	if fullBackup {
		exportType = "full_backup"
	}
	_ = s.audit(r, session.Username, "settings_export", "configuration", fmt.Sprintf("exported %s YAML configuration", exportType), true)

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"mirrorrelay-config.yaml\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(yamlContent))
}

type importPayload struct {
	YAML string `json:"yaml"`
}

func (s *Server) parseCandidateFromYAML(ctx context.Context, yamlText string) (config.Config, error) {
	reader := strings.NewReader(yamlText)
	imported, err := config.LoadReader(reader)
	if err != nil {
		return imported, err
	}

	current := s.fileConfig
	if stored, found, err := s.store.Setting(ctx, config.WebSettingsKey); err == nil && found {
		if ws, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if applied, err := ws.Apply(s.fileConfig); err == nil {
				current = applied
			}
		}
	}

	// Instance public base URLs must not be overwritten if omitted in imported YAML
	if imported.HTTP.PublicBaseURL == "" {
		imported.HTTP.PublicBaseURL = current.HTTP.PublicBaseURL
	}
	if imported.Distributed.Node.PublicBaseURL == "" {
		imported.Distributed.Node.PublicBaseURL = current.Distributed.Node.PublicBaseURL
	}

	// Preserve credentials if omitted in imported YAML
	if imported.Distributed.Token == "" {
		imported.Distributed.Token = current.Distributed.Token
	}
	if imported.Distributed.MutationToken == "" {
		imported.Distributed.MutationToken = current.Distributed.MutationToken
	}
	if imported.Webhook.Secret == "" {
		imported.Webhook.Secret = current.Webhook.Secret
	}
	if len(imported.Distributed.Nodes) > 0 && len(current.Distributed.Nodes) > 0 {
		for i, node := range imported.Distributed.Nodes {
			if node.MutationToken == "" {
				for _, prev := range current.Distributed.Nodes {
					if prev.URL == node.URL || prev.Name == node.Name {
						imported.Distributed.Nodes[i].MutationToken = prev.MutationToken
						break
					}
				}
			}
		}
	}

	if err := imported.NormalizeRuntime(); err != nil {
		return imported, err
	}
	if err := imported.Validate(); err != nil {
		return imported, err
	}
	return imported, nil
}

func (s *Server) previewImportSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in importPayload
	if decodeJSON(w, r, &in) != nil {
		return
	}
	if strings.TrimSpace(in.YAML) == "" {
		writeError(w, http.StatusBadRequest, "YAML configuration content is required")
		return
	}

	candidate, err := s.parseCandidateFromYAML(r.Context(), in.YAML)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Configuration validation failed: "+err.Error())
		return
	}

	current := s.fileConfig
	if stored, found, storeErr := s.store.Setting(r.Context(), config.WebSettingsKey); storeErr == nil && found {
		if ws, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if applied, err := ws.Apply(s.fileConfig); err == nil {
				current = applied
			}
		}
	}

	oldWS := config.WebSettingsFrom(current)
	newWS := config.WebSettingsFrom(candidate)
	diff := config.ComputeSettingsDiff(oldWS, newWS)

	restartRequired := !webSettingsEqual(newWS, config.WebSettingsFrom(s.cfg))
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":            true,
		"diff":             diff,
		"restart_required": restartRequired,
		"summary":          fmt.Sprintf("%d setting(s) modified", len(diff)),
	})
}

func (s *Server) applyImportSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in importPayload
	if decodeJSON(w, r, &in) != nil {
		return
	}
	if strings.TrimSpace(in.YAML) == "" {
		writeError(w, http.StatusBadRequest, "YAML configuration content is required")
		return
	}

	candidate, err := s.parseCandidateFromYAML(r.Context(), in.YAML)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_import", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, "Configuration validation failed: "+err.Error())
		return
	}

	current := s.fileConfig
	if stored, found, storeErr := s.store.Setting(r.Context(), config.WebSettingsKey); storeErr == nil && found {
		if ws, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if applied, err := ws.Apply(s.fileConfig); err == nil {
				current = applied
			}
		}
	}

	oldWS := config.WebSettingsFrom(current)
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

	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := normalized
	redactWebSettingsForRole(&safeWS, session.Role)
	safeJSON, _ := json.Marshal(safeWS)

	summary := fmt.Sprintf("imported YAML configuration (%d setting(s) changed)", len(diff))
	_, _ = s.store.AddSettingVersion(r.Context(), model.SettingVersion{
		Operator:    session.Username,
		Source:      "configuration_import",
		Description: summary,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}, s.cfg.UpstreamNginx.HistoryLimit)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_import", "configuration", summary, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"settings":         normalized,
		"restart_required": restartRequired,
		"diff":             diff,
	})
}

func (s *Server) listSettingsHistory(w http.ResponseWriter, r *http.Request, session auth.Session) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	versions, err := s.store.ListSettingVersions(r.Context(), limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if versions == nil {
		versions = []model.SettingVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) rollbackSettingsHistory(w http.ResponseWriter, r *http.Request, session auth.Session, rawVersion string) {
	version, err := strconv.ParseInt(strings.Trim(rawVersion, "/"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid configuration version")
		return
	}

	vRecord, err := s.store.GetSettingVersion(r.Context(), version)
	if err != nil {
		writeError(w, http.StatusNotFound, "configuration version not found")
		return
	}

	var storedWS config.WebSettings
	if err := json.Unmarshal([]byte(vRecord.Settings), &storedWS); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid version payload: "+err.Error())
		return
	}

	baseConfig := s.fileConfig
	if stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey); err == nil && found {
		if prevWS, err := config.DecodeWebSettings([]byte(stored)); err == nil {
			if appliedPrev, err := prevWS.Apply(s.fileConfig); err == nil {
				baseConfig = appliedPrev
			}
		}
	}

	candidate, err := storedWS.Apply(baseConfig)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_rollback", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, "rollback validation failed: "+err.Error())
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

	oldWS := config.WebSettingsFrom(baseConfig)
	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := normalized
	redactWebSettingsForRole(&safeWS, session.Role)
	safeJSON, _ := json.Marshal(safeWS)

	desc := fmt.Sprintf("rollback to configuration version %d", version)
	_, _ = s.store.AddSettingVersion(r.Context(), model.SettingVersion{
		Operator:    session.Username,
		Source:      "settings_rollback",
		Description: desc,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}, s.cfg.UpstreamNginx.HistoryLimit)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_rollback", "configuration", desc, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"settings":         normalized,
		"restart_required": restartRequired,
		"diff":             diff,
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
