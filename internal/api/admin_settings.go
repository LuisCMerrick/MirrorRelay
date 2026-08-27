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

func applyWebSettings(settings config.WebSettings, base config.Config) (config.Config, error) {
	candidate, err := settings.Apply(base)
	if err != nil {
		return base, err
	}
	return config.ApplyEnvironment(candidate)
}

func (s *Server) storedWebConfig(ctx context.Context) (config.Config, bool, error) {
	raw, found, err := s.store.Setting(ctx, config.WebSettingsKey)
	if err != nil {
		return s.fileConfig, false, fmt.Errorf("read stored settings: %w", err)
	}
	if !found {
		return s.fileConfig, false, nil
	}
	settings, err := config.DecodeWebSettings([]byte(raw))
	if err != nil {
		return s.fileConfig, true, fmt.Errorf("decode stored settings: %w", err)
	}
	candidate, err := applyWebSettings(settings, s.fileConfig)
	if err != nil {
		return s.fileConfig, true, fmt.Errorf("apply stored settings: %w", err)
	}
	return candidate, true, nil
}

func (s *Server) webSettings(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	currentConfig := s.cfg
	currentConfig.UIEnhancement = s.appearanceConfig()
	current := config.WebSettingsFrom(currentConfig)
	fromFileConfig := s.fileConfig
	fromFileConfig.UIEnhancement = s.appearanceConfig()
	fromFile := config.WebSettingsFrom(fromFileConfig)
	candidate, found, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if !found {
		restartRequired := !webSettingsEqual(fromFile, current)
		safeSettings := redactedWebSettings(fromFile)
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": safeSettings, "source": "configuration_file", "restart_required": restartRequired, "file_only": config.WebSettingsFileOnlyPaths(),
		})
		return
	}
	candidate.UIEnhancement = s.appearanceConfig()
	settings := config.WebSettingsFrom(candidate)
	restartRequired := !webSettingsEqual(settings, current)
	safeSettings := redactedWebSettings(settings)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": safeSettings, "source": "web_ui", "restart_required": restartRequired, "file_only": config.WebSettingsFileOnlyPaths(),
	})
}

func redactedWebSettings(settings config.WebSettings) config.WebSettings {
	safe := settings
	if settings.Webhook != nil {
		webhook := *settings.Webhook
		webhook.Events = append([]string{}, settings.Webhook.Events...)
		safe.Webhook = &webhook
	}
	safe.Distributed.Nodes = append([]config.DistributedNodeSeed{}, settings.Distributed.Nodes...)
	if safe.Webhook != nil {
		if safe.Webhook.URL != "" {
			safe.Webhook.URL = redactedValue
		}
		if safe.Webhook.Secret != "" {
			safe.Webhook.Secret = redactedValue
		}
	}
	if safe.Distributed.Token != "" {
		safe.Distributed.Token = redactedValue
	}
	if safe.Distributed.MutationToken != "" {
		safe.Distributed.MutationToken = redactedValue
	}
	for i := range safe.Distributed.Nodes {
		if safe.Distributed.Nodes[i].MutationToken != "" {
			safe.Distributed.Nodes[i].MutationToken = redactedValue
		}
	}
	return safe
}

func (s *Server) putWebSettingsWithAppearance(ctx context.Context, webSettingsJSON string, appearanceConfig model.UIEnhancementConfig, version model.SettingVersion) (model.SettingVersion, error) {
	appearanceJSON, err := json.Marshal(appearanceConfig)
	if err != nil {
		return version, err
	}
	return s.store.PutSettingsWithVersion(ctx, map[string]string{
		config.WebSettingsKey:          webSettingsJSON,
		database.AppearanceSettingsKey: string(appearanceJSON),
	}, version, s.cfg.UpstreamNginx.HistoryLimit)
}

func operationalWebSettingsJSON(settings config.WebSettings, fileAppearance model.UIEnhancementConfig) ([]byte, error) {
	stored := settings
	// Appearance is persisted and published through its dedicated record. Keep
	// the operational JSON at the YAML fallback so deleting the appearance
	// override consistently restores YAML after a restart as well.
	stored.UIEnhancement = fileAppearance
	return json.Marshal(stored)
}

func (s *Server) publishAppearance(value model.UIEnhancementConfig) {
	if s.appearance == nil {
		s.appearance = appearance.New(s.cfg.UIEnhancement)
	}
	s.appearance.Store(value)
}

func (s *Server) updateWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input config.WebSettings
	if decodeJSON(w, r, &input) != nil {
		return
	}
	baseConfig, _, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	baseConfig.UIEnhancement = s.appearanceConfig()

	candidate, err := applyWebSettings(input, baseConfig)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_update", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	normalized := config.WebSettingsFrom(candidate)
	encoded, err := operationalWebSettingsJSON(normalized, s.fileConfig.UIEnhancement)
	if err != nil {
		writeInternal(w, err)
		return
	}

	oldWS := config.WebSettingsFrom(baseConfig)
	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := redactedWebSettings(normalized)
	safeJSON, _ := json.Marshal(safeWS)

	summary := fmt.Sprintf("%d setting(s) updated", len(diff))
	if _, err := s.putWebSettingsWithAppearance(r.Context(), string(encoded), normalized.UIEnhancement, model.SettingVersion{
		Operator:    session.Username,
		Source:      "web_ui",
		Description: summary,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}); err != nil {
		writeInternal(w, err)
		return
	}
	s.publishAppearance(normalized.UIEnhancement)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_update", "configuration", summary, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": safeWS, "source": "web_ui", "restart_required": restartRequired, "file_only": config.WebSettingsFileOnlyPaths(), "diff": diff,
	})
}

func (s *Server) resetWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	currentConfig, _, _ := s.storedWebConfig(r.Context()) // Reset remains the recovery path for invalid stored JSON.
	currentConfig.UIEnhancement = s.appearanceConfig()
	fromFileConfig := s.fileConfig
	fromFileConfig.UIEnhancement = s.appearanceConfig()
	fromFile := config.WebSettingsFrom(fromFileConfig)
	diff := config.ComputeSettingsDiff(config.WebSettingsFrom(currentConfig), fromFile)
	diffBytes, _ := json.Marshal(diff)
	safeWS := redactedWebSettings(fromFile)
	safeJSON, _ := json.Marshal(safeWS)
	description := fmt.Sprintf("reset settings to configuration file defaults (%d setting(s) changed)", len(diff))

	if _, err := s.store.DeleteSettingWithVersion(r.Context(), config.WebSettingsKey, model.SettingVersion{
		Operator:    session.Username,
		Source:      "settings_reset",
		Description: description,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}, s.cfg.UpstreamNginx.HistoryLimit); err != nil {
		writeInternal(w, err)
		return
	}

	_ = s.audit(r, session.Username, "settings_reset", "configuration", "restore configuration file values after restart", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": safeWS, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, config.WebSettingsFrom(s.cfg)), "file_only": config.WebSettingsFileOnlyPaths(), "diff": diff,
	})
}

func (s *Server) exportSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	fullBackup := r.URL.Query().Get("full_backup") == "true"
	if fullBackup && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "full backup export requires POST with CSRF protection")
		return
	}
	currentConfig, _, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	currentConfig.UIEnhancement = s.appearanceConfig()

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

const maxSettingsImportYAMLBytes = 1 << 20

func decodeSettingsImport(w http.ResponseWriter, r *http.Request, out *importPayload) error {
	// A JSON string can require up to six bytes for each input byte after
	// escaping. Bound the envelope separately, then enforce the documented
	// limit against the decoded YAML itself.
	if err := decodeJSONLimit(w, r, out, 6*maxSettingsImportYAMLBytes+1024); err != nil {
		return err
	}
	if len(out.YAML) > maxSettingsImportYAMLBytes {
		err := fmt.Errorf("YAML configuration content exceeds 1 MiB")
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return err
	}
	return nil
}

func (s *Server) parseCandidateFromYAML(ctx context.Context, yamlText string) (config.Config, error) {
	reader := strings.NewReader(yamlText)
	imported, err := config.DecodeImportReader(reader)
	if err != nil {
		return imported, err
	}

	current, _, err := s.storedWebConfig(ctx)
	if err != nil {
		return imported, err
	}

	// Instance public base URLs must not be overwritten if omitted in imported YAML
	if imported.HTTP.PublicBaseURL == "" {
		imported.HTTP.PublicBaseURL = current.HTTP.PublicBaseURL
	}
	if imported.Distributed.Node.PublicBaseURL == "" {
		imported.Distributed.Node.PublicBaseURL = current.Distributed.Node.PublicBaseURL
	}
	if imported.Admin.Passkey.RPID == "" {
		imported.Admin.Passkey.RPID = current.Admin.Passkey.RPID
	}
	if len(imported.Admin.Passkey.Origins) == 0 {
		imported.Admin.Passkey.Origins = append([]string{}, current.Admin.Passkey.Origins...)
	}

	// Bootstrap values must continue to point to the already-open database and
	// mutation-token keyring. Imported values cannot take effect safely here.
	imported.Database.Path = current.Database.Path
	imported.Distributed.MutationTokenKeyFiles = append([]string{}, current.Distributed.MutationTokenKeyFiles...)

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
	if imported.Webhook.URL == "" {
		imported.Webhook.URL = current.Webhook.URL
	}
	if len(imported.Distributed.Nodes) > 0 && len(current.Distributed.Nodes) > 0 {
		for i, node := range imported.Distributed.Nodes {
			if node.MutationToken == "" {
				for _, prev := range current.Distributed.Nodes {
					if prev.URL == node.URL {
						imported.Distributed.Nodes[i].MutationToken = prev.MutationToken
						break
					}
				}
			}
		}
	}

	if err := config.ValidateUIEnhancement(&imported.UIEnhancement); err != nil {
		return imported, err
	}
	return config.ApplyEnvironment(imported)
}

func (s *Server) previewImportSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in importPayload
	if decodeSettingsImport(w, r, &in) != nil {
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

	current, _, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	current.UIEnhancement = s.appearanceConfig()

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
	if decodeSettingsImport(w, r, &in) != nil {
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

	current, _, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	current.UIEnhancement = s.appearanceConfig()

	oldWS := config.WebSettingsFrom(current)
	normalized := config.WebSettingsFrom(candidate)
	encoded, err := operationalWebSettingsJSON(normalized, s.fileConfig.UIEnhancement)
	if err != nil {
		writeInternal(w, err)
		return
	}

	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := redactedWebSettings(normalized)
	safeJSON, _ := json.Marshal(safeWS)

	summary := fmt.Sprintf("imported YAML configuration (%d setting(s) changed)", len(diff))
	if _, err := s.putWebSettingsWithAppearance(r.Context(), string(encoded), normalized.UIEnhancement, model.SettingVersion{
		Operator:    session.Username,
		Source:      "configuration_import",
		Description: summary,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}); err != nil {
		writeInternal(w, err)
		return
	}
	s.publishAppearance(normalized.UIEnhancement)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_import", "configuration", summary, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"settings":         safeWS,
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

	baseConfig, _, err := s.storedWebConfig(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	baseConfig.UIEnhancement = s.appearanceConfig()

	candidate, err := applyWebSettings(storedWS, baseConfig)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_rollback", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, "rollback validation failed: "+err.Error())
		return
	}
	candidate.UIEnhancement = storedWS.UIEnhancement
	if err := config.ValidateUIEnhancement(&candidate.UIEnhancement); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "rollback appearance validation failed: "+err.Error())
		return
	}

	normalized := config.WebSettingsFrom(candidate)
	encoded, err := operationalWebSettingsJSON(normalized, s.fileConfig.UIEnhancement)
	if err != nil {
		writeInternal(w, err)
		return
	}

	oldWS := config.WebSettingsFrom(baseConfig)
	diff := config.ComputeSettingsDiff(oldWS, normalized)
	diffBytes, _ := json.Marshal(diff)

	safeWS := redactedWebSettings(normalized)
	safeJSON, _ := json.Marshal(safeWS)

	desc := fmt.Sprintf("rollback to configuration version %d", version)
	if _, err := s.putWebSettingsWithAppearance(r.Context(), string(encoded), normalized.UIEnhancement, model.SettingVersion{
		Operator:    session.Username,
		Source:      "settings_rollback",
		Description: desc,
		DiffSummary: string(diffBytes),
		Settings:    string(safeJSON),
	}); err != nil {
		writeInternal(w, err)
		return
	}
	s.publishAppearance(normalized.UIEnhancement)

	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_rollback", "configuration", desc, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"settings":         safeWS,
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
