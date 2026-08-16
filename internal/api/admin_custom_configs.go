package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

func findMirror(values []model.Mirror, id int64) (model.Mirror, bool) {
	for _, m := range values {
		if m.ID == id {
			return m, true
		}
	}
	return model.Mirror{}, false
}

func replaceCandidate(values []model.Mirror, replacement model.Mirror) []model.Mirror {
	out := append([]model.Mirror(nil), values...)
	for i := range out {
		if out[i].ID == replacement.ID {
			out[i] = replacement
			return out
		}
	}
	return append(out, replacement)
}

func removeCandidate(values []model.Mirror, id int64) []model.Mirror {
	out := make([]model.Mirror, 0, len(values))
	for _, value := range values {
		if value.ID != id {
			out = append(out, value)
		}
	}
	return out
}

func validationMessage(result string, err error) string {
	if strings.TrimSpace(result) == "" {
		return err.Error()
	}
	return err.Error() + ": " + strings.TrimSpace(result)
}

func (s *Server) createCustomConfig(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value model.CustomConfig
	if decodeJSON(w, r, &value) != nil {
		return
	}
	value.Name = strings.TrimSpace(value.Name)
	value.Context = strings.ToLower(strings.TrimSpace(value.Context))
	if err := upstreamnginx.ValidateCustomName(value.Name); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := upstreamnginx.ValidateCustom(value.Context, value.Content); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	value.LastResult = "syntax policy passed"
	custom, err := s.store.ListCustomConfigs(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	desired, err := s.store.ListMirrors(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, append(custom, value)); err != nil {
		writeError(w, 422, validationMessage(result, err))
		return
	}
	created, err := s.store.CreateCustomConfig(r.Context(), value)
	if err != nil {
		if database.IsConflict(err) {
			writeError(w, 409, "custom configuration name already exists")
			return
		}
		writeInternal(w, err)
		return
	}
	if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "create custom config "+created.Name); err != nil {
		_ = s.audit(r, session.Username, "custom_config_create", "managed-upstream-nginx", err.Error(), false)
		writeError(w, 422, "custom configuration saved but activation failed: "+err.Error())
		return
	}
	_ = s.audit(r, session.Username, "custom_config_create", "managed-upstream-nginx", created.Name, true)
	writeJSON(w, 201, created)
}

func (s *Server) customConfigAction(w http.ResponseWriter, r *http.Request, session auth.Session, tail string) {
	id, err := strconv.ParseInt(strings.Trim(tail, "/"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid custom configuration id")
		return
	}
	current, err := s.store.CustomConfig(r.Context(), id)
	if err != nil {
		writeError(w, 404, "custom configuration not found")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, current)
		return
	}
	if r.Method == http.MethodDelete {
		custom, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		candidate := make([]model.CustomConfig, 0, len(custom)-1)
		for _, value := range custom {
			if value.ID != id {
				candidate = append(candidate, value)
			}
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, candidate); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		if err := s.store.DeleteCustomConfig(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "delete custom config "+current.Name); err != nil {
			writeError(w, 422, "custom configuration deleted but activation failed: "+err.Error())
			return
		}
		_ = s.audit(r, session.Username, "custom_config_delete", "managed-upstream-nginx", current.Name, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPut {
		var value model.CustomConfig
		if decodeJSON(w, r, &value) != nil {
			return
		}
		value.ID = id
		value.Name = strings.TrimSpace(value.Name)
		value.Context = strings.ToLower(strings.TrimSpace(value.Context))
		if err := upstreamnginx.ValidateCustomName(value.Name); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err := upstreamnginx.ValidateCustom(value.Context, value.Content); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		value.LastResult = "syntax policy passed"
		custom, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		for i := range custom {
			if custom[i].ID == id {
				custom[i] = value
			}
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, custom); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		updated, err := s.store.UpdateCustomConfig(r.Context(), value)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "update custom config "+updated.Name); err != nil {
			writeError(w, 422, "custom configuration saved but activation failed: "+err.Error())
			return
		}
		_ = s.audit(r, session.Username, "custom_config_update", "managed-upstream-nginx", updated.Name, true)
		writeJSON(w, 200, updated)
		return
	}
	writeError(w, 405, "method not allowed")
}
