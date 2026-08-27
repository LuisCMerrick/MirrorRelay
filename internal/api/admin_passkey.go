package api

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Server) passkeyStatus(w http.ResponseWriter, r *http.Request) {
	rpID := auth.DefaultRPID(s.cfg.Admin.Passkey.RPID, s.cfg.HTTP.PublicBaseURL, r.Host)
	rpName := s.cfg.Admin.Passkey.RPName
	if rpName == "" {
		rpName = "MirrorRelay"
	}
	enabled := s.cfg.Admin.Passkey.Enabled
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"rp_id":   rpID,
		"rp_name": rpName,
	})
}

func (s *Server) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Admin.Passkey.Enabled {
		writeError(w, http.StatusBadRequest, "Passkey authentication is not enabled")
		return
	}
	var in struct {
		Username string `json:"username"`
	}
	if decodeJSON(w, r, &in) != nil {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if len(in.Username) > 256 {
		writeError(w, http.StatusBadRequest, "username is too long")
		return
	}

	rpID := auth.DefaultRPID(s.cfg.Admin.Passkey.RPID, s.cfg.HTTP.PublicBaseURL, r.Host)
	origin := auth.DefaultOrigin(s.cfg.Admin.Passkey.Origins, s.cfg.HTTP.PublicBaseURL, r.Host, r.TLS != nil)

	var userID int64
	var username string
	var allowedCreds []map[string]any
	if in.Username != "" {
		if u, err := s.store.UserByName(r.Context(), in.Username); err == nil {
			userID = u.ID
			username = u.Username
			if pks, err := s.store.ListPasskeysByUserID(r.Context(), u.ID); err == nil {
				for _, pk := range pks {
					allowedCreds = append(allowedCreds, map[string]any{
						"type":       "public-key",
						"id":         pk.CredentialID,
						"transports": pk.Transports,
					})
				}
			}
		}
	}

	challenge, err := s.challengeMgr.Create(userID, username, "login", rpID, origin)
	if err != nil {
		writeInternal(w, err)
		return
	}

	res := map[string]any{
		"challenge":        challenge,
		"rpId":             rpID,
		"timeout":          60000,
		"userVerification": "required",
	}
	if len(allowedCreds) > 0 {
		res["allowCredentials"] = allowedCreds
	}

	writeJSON(w, http.StatusOK, res)
}

type passkeyLoginVerifyRequest struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle,omitempty"`
	} `json:"response"`
}

func decodeCredentialID(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("credential id is required")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) == 0 || len(decoded) > 1024 {
		return nil, errors.New("invalid credential id encoding")
	}
	return decoded, nil
}

func normalizeCredentialID(id, rawID string) (string, []byte, error) {
	value := id
	if value == "" {
		value = rawID
	}
	decoded, err := decodeCredentialID(value)
	if err != nil {
		return "", nil, err
	}
	if id != "" && rawID != "" {
		rawDecoded, err := decodeCredentialID(rawID)
		if err != nil || !bytes.Equal(decoded, rawDecoded) {
			return "", nil, errors.New("credential id and rawId do not match")
		}
	}
	return base64.RawURLEncoding.EncodeToString(decoded), decoded, nil
}

func validatedPasskeyTransports(values []string) ([]string, error) {
	if len(values) > 8 {
		return nil, errors.New("too many authenticator transports")
	}
	allowed := map[string]bool{
		"ble": true, "hybrid": true, "internal": true, "nfc": true,
		"smart-card": true, "usb": true,
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !allowed[value] {
			return nil, fmt.Errorf("unsupported authenticator transport %q", value)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func (s *Server) passkeyLoginVerify(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Admin.Passkey.Enabled {
		writeError(w, http.StatusBadRequest, "Passkey authentication is not enabled")
		return
	}
	ip := s.requestClientIP(r)
	var in passkeyLoginVerifyRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	if in.Type != "public-key" {
		writeError(w, http.StatusBadRequest, "credential type must be public-key")
		return
	}
	credID, _, err := normalizeCredentialID(in.ID, in.RawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := ip + ":passkey:" + credID
	release, allowed := s.loginLimiter.Acquire(key)
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	rawClientData, err := base64.RawURLEncoding.DecodeString(in.Response.ClientDataJSON)
	if err != nil {
		rawClientData, err = base64.StdEncoding.DecodeString(in.Response.ClientDataJSON)
		if err != nil {
			release(false)
			writeError(w, http.StatusBadRequest, "invalid clientDataJSON encoding")
			return
		}
	}

	var clientData auth.ClientDataJSON
	if err := json.Unmarshal(rawClientData, &clientData); err != nil {
		release(false)
		writeError(w, http.StatusBadRequest, "invalid clientDataJSON")
		return
	}

	challengeSession, err := s.challengeMgr.Consume(clientData.Challenge, "login")
	if err != nil {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", err.Error(), false)
		writeError(w, http.StatusUnauthorized, "invalid or expired challenge")
		return
	}

	if _, err := auth.VerifyClientData(rawClientData, "webauthn.get", clientData.Challenge, challengeSession.Origin, nil); err != nil {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "client data verification failed: "+err.Error(), false)
		writeError(w, http.StatusUnauthorized, "invalid WebAuthn client data")
		return
	}

	rawAuthData, err := base64.RawURLEncoding.DecodeString(in.Response.AuthenticatorData)
	if err != nil {
		rawAuthData, err = base64.StdEncoding.DecodeString(in.Response.AuthenticatorData)
		if err != nil {
			release(false)
			writeError(w, http.StatusBadRequest, "invalid authenticatorData encoding")
			return
		}
	}

	parsedAuthData, err := auth.ParseAuthenticatorData(rawAuthData)
	if err != nil {
		release(false)
		writeError(w, http.StatusBadRequest, "parse authenticatorData: "+err.Error())
		return
	}

	if err := auth.VerifyRPID(parsedAuthData.RPIDHash, challengeSession.RPID); err != nil {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "RP ID hash mismatch: "+err.Error(), false)
		writeError(w, http.StatusUnauthorized, "RP ID mismatch")
		return
	}

	if !parsedAuthData.UserPresent || !parsedAuthData.UserVerified {
		release(false)
		writeError(w, http.StatusUnauthorized, "user presence and verification are required")
		return
	}
	if parsedAuthData.AttestedData {
		release(false)
		writeError(w, http.StatusBadRequest, "assertion authenticatorData must not contain attested credential data")
		return
	}

	pk, err := s.store.GetPasskeyByCredentialID(r.Context(), credID)
	if err != nil {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "credential not found: "+credID, false)
		writeError(w, http.StatusUnauthorized, "passkey not recognized")
		return
	}
	if challengeSession.UserID != 0 && pk.UserID != challengeSession.UserID {
		release(false)
		writeError(w, http.StatusUnauthorized, "credential does not belong to the requested account")
		return
	}
	if in.Response.UserHandle != "" {
		userHandle, err := base64.RawURLEncoding.DecodeString(in.Response.UserHandle)
		if err != nil || len(userHandle) != 8 || int64(binary.BigEndian.Uint64(userHandle)) != pk.UserID {
			release(false)
			writeError(w, http.StatusUnauthorized, "credential user handle mismatch")
			return
		}
	}

	// A positive signature counter must advance. A transition from a positive
	// value to zero is also a clone signal; only the all-zero counter mode lacks
	// anti-clone information.
	if (pk.SignCount > 0 || parsedAuthData.SignCount > 0) && parsedAuthData.SignCount <= pk.SignCount {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "cloned authenticator detected", false)
		writeError(w, http.StatusUnauthorized, "security check failed (cloned authenticator)")
		return
	}

	pubKeyBytes, err := base64.RawURLEncoding.DecodeString(pk.PublicKey)
	if err != nil {
		pubKeyBytes, err = base64.StdEncoding.DecodeString(pk.PublicKey)
		if err != nil {
			release(false)
			writeInternal(w, errors.New("corrupt stored public key"))
			return
		}
	}

	pubKey, err := auth.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		release(false)
		writeInternal(w, fmt.Errorf("parse public key: %w", err))
		return
	}

	rawSig, err := base64.RawURLEncoding.DecodeString(in.Response.Signature)
	if err != nil {
		rawSig, err = base64.StdEncoding.DecodeString(in.Response.Signature)
		if err != nil {
			release(false)
			writeError(w, http.StatusBadRequest, "invalid signature encoding")
			return
		}
	}

	if err := auth.VerifyAuthenticationSignature(pubKey, rawAuthData, rawClientData, rawSig); err != nil {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "signature verification failed: "+err.Error(), false)
		writeError(w, http.StatusUnauthorized, "passkey signature verification failed")
		return
	}

	// Update sign count and last-used time before creating a session.
	advanced, err := s.store.AdvancePasskeySignCount(r.Context(), credID, parsedAuthData.SignCount)
	if err != nil {
		release(false)
		writeInternal(w, fmt.Errorf("update passkey counter: %w", err))
		return
	}
	if !advanced {
		release(false)
		_ = s.audit(r, "", "passkey_login_failed", "session", "authenticator counter did not advance", false)
		writeError(w, http.StatusUnauthorized, "security check failed (authenticator counter changed)")
		return
	}

	// Fetch user
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		release(false)
		writeInternal(w, err)
		return
	}
	var user *model.User
	for i := range users {
		if users[i].ID == pk.UserID {
			user = &users[i]
			break
		}
	}
	if user == nil {
		release(false)
		writeError(w, http.StatusUnauthorized, "associated user account not found")
		return
	}

	release(true)
	session, err := s.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.sessions.SetCookie(w, session)
	_ = s.audit(r, user.Username, "passkey_login_success", "session", "credential_id="+credID, true)
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "role": user.Role, "csrf_token": session.CSRFToken})
}

func (s *Server) recoveryLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.requestClientIP(r)
	var in struct {
		Username     string `json:"username"`
		RecoveryCode string `json:"recovery_code"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	in.RecoveryCode = strings.TrimSpace(in.RecoveryCode)
	if !validUsername(in.Username) || in.RecoveryCode == "" || len(in.RecoveryCode) > 64 {
		writeError(w, http.StatusBadRequest, "username or recovery_code has an invalid format")
		return
	}

	key := ip + ":recovery:" + in.Username
	release, allowed := s.loginLimiter.Acquire(key)
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "too many recovery login attempts")
		return
	}

	user, err := s.store.UserByName(r.Context(), in.Username)
	if err != nil {
		release(false)
		time.Sleep(250 * time.Millisecond)
		_ = s.audit(r, in.Username, "recovery_code_failed", "session", "user not found", false)
		writeError(w, http.StatusUnauthorized, "invalid recovery code or username")
		return
	}

	codeHash := auth.HashRecoveryCode(in.RecoveryCode)
	used, err := s.store.VerifyAndUseRecoveryCode(r.Context(), user.ID, codeHash)
	if err != nil || !used {
		release(false)
		time.Sleep(250 * time.Millisecond)
		_ = s.audit(r, in.Username, "recovery_code_failed", "session", "invalid or used code", false)
		writeError(w, http.StatusUnauthorized, "invalid or already used recovery code")
		return
	}

	release(true)
	session, err := s.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.sessions.SetCookie(w, session)
	_ = s.audit(r, user.Username, "recovery_code_used", "session", "logged in via emergency recovery code", true)
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "role": user.Role, "csrf_token": session.CSRFToken})
}

func (s *Server) listAccountPasskeys(w http.ResponseWriter, r *http.Request, session auth.Session) {
	passkeys, err := s.store.ListPasskeysByUserID(r.Context(), session.UserID)
	if err != nil {
		writeInternal(w, err)
		return
	}
	codeCount, err := s.store.CountValidRecoveryCodes(r.Context(), session.UserID)
	if err != nil {
		writeInternal(w, err)
		return
	}
	user, err := s.store.UserByName(r.Context(), session.Username)
	if err != nil {
		writeInternal(w, err)
		return
	}

	type passkeyItem struct {
		ID             int64      `json:"id"`
		CredentialID   string     `json:"credential_id"`
		DisplayName    string     `json:"display_name"`
		AAGUID         string     `json:"aaguid,omitempty"`
		Transports     []string   `json:"transports,omitempty"`
		BackupEligible bool       `json:"backup_eligible"`
		BackupState    bool       `json:"backup_state"`
		CreatedAt      time.Time  `json:"created_at"`
		LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	}

	items := make([]passkeyItem, 0, len(passkeys))
	for _, pk := range passkeys {
		items = append(items, passkeyItem{
			ID:             pk.ID,
			CredentialID:   pk.CredentialID,
			DisplayName:    pk.DisplayName,
			AAGUID:         pk.AAGUID,
			Transports:     pk.Transports,
			BackupEligible: pk.BackupEligible,
			BackupState:    pk.BackupState,
			CreatedAt:      pk.CreatedAt,
			LastUsedAt:     pk.LastUsedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"passkeys":                 items,
		"recovery_codes_remaining": codeCount,
		"password_login_disabled":  user.PasswordLoginDisabled,
		"passkey_enabled":          s.cfg.Admin.Passkey.Enabled,
	})
}

func (s *Server) registerPasskeyOptions(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if !s.cfg.Admin.Passkey.Enabled {
		writeError(w, http.StatusBadRequest, "Passkey authentication is not enabled")
		return
	}
	rpID := auth.DefaultRPID(s.cfg.Admin.Passkey.RPID, s.cfg.HTTP.PublicBaseURL, r.Host)
	origin := auth.DefaultOrigin(s.cfg.Admin.Passkey.Origins, s.cfg.HTTP.PublicBaseURL, r.Host, r.TLS != nil)
	rpName := s.cfg.Admin.Passkey.RPName
	if rpName == "" {
		rpName = "MirrorRelay"
	}

	challenge, err := s.challengeMgr.Create(session.UserID, session.Username, "register", rpID, origin)
	if err != nil {
		writeInternal(w, err)
		return
	}

	existing, _ := s.store.ListPasskeysByUserID(r.Context(), session.UserID)
	var excludeCredentials []map[string]any
	for _, pk := range existing {
		ex := map[string]any{
			"type": "public-key",
			"id":   pk.CredentialID,
		}
		if len(pk.Transports) > 0 {
			ex["transports"] = pk.Transports
		}
		excludeCredentials = append(excludeCredentials, ex)
	}

	userIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(userIDBytes, uint64(session.UserID))
	userHandle := base64.RawURLEncoding.EncodeToString(userIDBytes)

	res := map[string]any{
		"challenge": challenge,
		"rp": map[string]string{
			"name": rpName,
			"id":   rpID,
		},
		"user": map[string]string{
			"id":          userHandle,
			"name":        session.Username,
			"displayName": session.Username,
		},
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": -7},   // ES256
			{"type": "public-key", "alg": -257}, // RS256
			{"type": "public-key", "alg": -8},   // EdDSA
		},
		"timeout":     60000,
		"attestation": "none",
		"authenticatorSelection": map[string]any{
			"residentKey":        "required",
			"requireResidentKey": true,
			"userVerification":   "required",
		},
	}
	if len(excludeCredentials) > 0 {
		res["excludeCredentials"] = excludeCredentials
	}
	writeJSON(w, http.StatusOK, res)
}

type registerPasskeyVerifyRequest struct {
	DisplayName string   `json:"display_name"`
	ID          string   `json:"id"`
	RawID       string   `json:"rawId"`
	Type        string   `json:"type"`
	Transports  []string `json:"transports,omitempty"`
	Response    struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	} `json:"response"`
}

func (s *Server) registerPasskeyVerify(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if !s.cfg.Admin.Passkey.Enabled {
		writeError(w, http.StatusBadRequest, "Passkey authentication is not enabled")
		return
	}
	var in registerPasskeyVerifyRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	if in.Type != "public-key" {
		writeError(w, http.StatusBadRequest, "credential type must be public-key")
		return
	}
	credID, credentialBytes, err := normalizeCredentialID(in.ID, in.RawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawClientData, err := base64.RawURLEncoding.DecodeString(in.Response.ClientDataJSON)
	if err != nil {
		rawClientData, err = base64.StdEncoding.DecodeString(in.Response.ClientDataJSON)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid clientDataJSON encoding")
			return
		}
	}

	var clientData auth.ClientDataJSON
	if err := json.Unmarshal(rawClientData, &clientData); err != nil {
		writeError(w, http.StatusBadRequest, "invalid clientDataJSON")
		return
	}

	sess, err := s.challengeMgr.Consume(clientData.Challenge, "register")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired registration challenge")
		return
	}
	if sess.UserID != session.UserID {
		writeError(w, http.StatusForbidden, "user session mismatch")
		return
	}

	if _, err := auth.VerifyClientData(rawClientData, "webauthn.create", clientData.Challenge, sess.Origin, nil); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid WebAuthn client data: "+err.Error())
		return
	}

	rawAttestation, err := base64.RawURLEncoding.DecodeString(in.Response.AttestationObject)
	if err != nil {
		rawAttestation, err = base64.StdEncoding.DecodeString(in.Response.AttestationObject)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid attestationObject encoding")
			return
		}
	}

	rawAuthData, err := auth.ExtractAttestationAuthData(rawAttestation)
	if err != nil {
		writeError(w, http.StatusBadRequest, "extract authData from attestation: "+err.Error())
		return
	}

	parsedAuthData, err := auth.ParseAuthenticatorData(rawAuthData)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse authenticatorData: "+err.Error())
		return
	}

	if err := auth.VerifyRPID(parsedAuthData.RPIDHash, sess.RPID); err != nil {
		writeError(w, http.StatusUnauthorized, "RP ID mismatch: "+err.Error())
		return
	}

	if !parsedAuthData.UserPresent || !parsedAuthData.UserVerified {
		writeError(w, http.StatusUnauthorized, "user presence and verification are required")
		return
	}

	if !parsedAuthData.AttestedData || len(parsedAuthData.CredentialID) == 0 || len(parsedAuthData.PublicKeyPKIX) == 0 {
		writeError(w, http.StatusBadRequest, "attested credential data missing from authenticator data")
		return
	}

	if !bytes.Equal(credentialBytes, parsedAuthData.CredentialID) {
		writeError(w, http.StatusBadRequest, "credential id does not match authenticator data")
		return
	}

	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = "Passkey (" + time.Now().Format("2006-01-02 15:04") + ")"
	}
	if len(displayName) > 128 || strings.ContainsRune(displayName, '\x00') {
		writeError(w, http.StatusBadRequest, "display_name must be at most 128 bytes and contain no NUL")
		return
	}
	transports, err := validatedPasskeyTransports(in.Transports)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pk := model.PasskeyCredential{
		UserID:         session.UserID,
		CredentialID:   credID,
		PublicKey:      base64.RawURLEncoding.EncodeToString(parsedAuthData.PublicKeyPKIX),
		SignCount:      parsedAuthData.SignCount,
		AAGUID:         parsedAuthData.AAGUID,
		Transports:     transports,
		BackupEligible: parsedAuthData.BackupEligible,
		BackupState:    parsedAuthData.BackupState,
		DisplayName:    displayName,
	}

	if err := s.store.CreatePasskey(r.Context(), pk); err != nil {
		writeInternal(w, fmt.Errorf("store passkey: %w", err))
		return
	}

	_ = s.audit(r, session.Username, "passkey_register", "user", "credential_id="+credID+", name="+displayName, true)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "credential_id": credID, "display_name": displayName})
}

func (s *Server) updatePasskey(w http.ResponseWriter, r *http.Request, session auth.Session, idStr string) {
	id, err := strconv.ParseInt(strings.Trim(idStr, "/"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid passkey id")
		return
	}
	var in struct {
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "display_name cannot be empty")
		return
	}
	if len(name) > 128 || strings.ContainsRune(name, '\x00') {
		writeError(w, http.StatusBadRequest, "display_name must be at most 128 bytes and contain no NUL")
		return
	}
	if err := s.store.UpdatePasskeyDisplayName(r.Context(), id, session.UserID, name); err != nil {
		writeError(w, http.StatusNotFound, "passkey not found")
		return
	}
	_ = s.audit(r, session.Username, "passkey_rename", "user", "id="+idStr+", new_name="+name, true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request, session auth.Session, idStr string) {
	id, err := strconv.ParseInt(strings.Trim(idStr, "/"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid passkey id")
		return
	}

	user, err := s.store.UserByName(r.Context(), session.Username)
	if err == nil && user.PasswordLoginDisabled {
		count, _ := s.store.CountPasskeysByUserID(r.Context(), session.UserID)
		if count <= 1 {
			writeError(w, http.StatusForbidden, "cannot delete your last passkey while password login is disabled")
			return
		}
	}

	if err := s.store.DeletePasskey(r.Context(), id, session.UserID); err != nil {
		writeError(w, http.StatusNotFound, "passkey not found")
		return
	}
	_ = s.audit(r, session.Username, "passkey_delete", "user", "id="+idStr, true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) generateRecoveryCodes(w http.ResponseWriter, r *http.Request, session auth.Session) {
	plain, hashes, err := auth.GenerateRecoveryCodes(8)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if err := s.store.SaveRecoveryCodes(r.Context(), session.UserID, hashes); err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "recovery_code_generate", "user", "generated 8 recovery codes", true)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": plain})
}

func (s *Server) setPasswordLoginDisabled(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}

	if in.Disabled {
		pkCount, err := s.store.CountPasskeysByUserID(r.Context(), session.UserID)
		if err != nil || pkCount == 0 {
			writeError(w, http.StatusBadRequest, "at least one Passkey must be registered before disabling password login")
			return
		}
		codeCount, err := s.store.CountValidRecoveryCodes(r.Context(), session.UserID)
		if err != nil || codeCount == 0 {
			writeError(w, http.StatusBadRequest, "active recovery codes must be generated before disabling password login")
			return
		}
	}

	if in.Disabled {
		updated, err := s.store.DisablePasswordLogin(r.Context(), session.UserID)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if !updated {
			writeError(w, http.StatusConflict, "passkey or recovery-code state changed; review account security and retry")
			return
		}
	} else if err := s.store.SetPasswordLoginDisabled(r.Context(), session.UserID, false); err != nil {
		writeInternal(w, err)
		return
	}

	action := "password_login_enabled"
	if in.Disabled {
		action = "password_login_disabled"
	}
	_ = s.audit(r, session.Username, action, "user", session.Username, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "password_login_disabled": in.Disabled})
}
