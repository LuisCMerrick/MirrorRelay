package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestApplyStoredWebSettingsAtStartup(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := config.Default()
	settings := config.WebSettingsFrom(base)
	settings.Server.UnixSocketEnabled = false
	settings.Server.LocalPort = 19081
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), config.WebSettingsKey, string(encoded)); err != nil {
		t.Fatal(err)
	}

	applied, err := applyStoredWebSettings(context.Background(), store, base)
	if err != nil {
		t.Fatal(err)
	}
	if network, address := applied.FrontendEndpoint(); network != "tcp" || address != "127.0.0.1:19081" {
		t.Fatalf("stored frontend endpoint = %s %s", network, address)
	}
}

func TestApplyStoredWebSettingsPreservesEnvironmentPrecedence(t *testing.T) {
	t.Setenv("MIRRORRELAY_ADMIN_HOST", "environment-admin.example.test")
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := config.Default()
	settings := config.WebSettingsFrom(base)
	settings.Admin.Host = "stored-admin.example.test"
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), config.WebSettingsKey, string(encoded)); err != nil {
		t.Fatal(err)
	}

	applied, err := applyStoredWebSettings(context.Background(), store, base)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Admin.Host != "environment-admin.example.test" {
		t.Fatalf("admin host = %q; environment override did not win", applied.Admin.Host)
	}
}

func TestApplyStoredAppearanceIsStrictAndUsesDedicatedOverride(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := config.Default()
	settings := config.WebSettingsFrom(base)
	settings.UIEnhancement.Enabled = true
	settings.UIEnhancement.Theme = "dark"
	settings.UIEnhancement.Branding.Title = "Operational fallback"
	encodedSettings, _ := json.Marshal(settings)
	if err := store.PutSetting(t.Context(), config.WebSettingsKey, string(encodedSettings)); err != nil {
		t.Fatal(err)
	}
	dedicated := settings.UIEnhancement
	dedicated.Theme = "light"
	dedicated.Branding.Title = "Dedicated appearance"
	encodedAppearance, _ := json.Marshal(dedicated)
	if err := store.PutSetting(t.Context(), database.AppearanceSettingsKey, string(encodedAppearance)); err != nil {
		t.Fatal(err)
	}

	applied, err := applyStoredWebSettings(t.Context(), store, base)
	if err != nil {
		t.Fatal(err)
	}
	if applied.UIEnhancement.Theme != "light" || applied.UIEnhancement.Branding.Title != "Dedicated appearance" {
		t.Fatalf("dedicated appearance did not win: %+v", applied.UIEnhancement)
	}

	if err := store.PutSetting(t.Context(), database.AppearanceSettingsKey, `{"unknown":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := applyStoredWebSettings(t.Context(), store, base); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("invalid stored appearance did not fail strictly: %v", err)
	}
}

func TestAdminCLIResetPasswordReadsStdinAndUsesConfiguredDatabase(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "mirrorrelay.db")
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte("database:\n  path: "+databasePath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	oldHash, err := auth.HashPassword("old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	user, created, err := store.CreateInitialAdmin(t.Context(), "admin", oldHash)
	if err != nil || !created {
		t.Fatalf("create initial admin: created=%v err=%v", created, err)
	}
	if err := store.SetPasswordLoginDisabled(t.Context(), user.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSession(t.Context(), "stale-session", user.ID, user.Username, user.Role, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	err = handleAdminCLIWithIO([]string{"reset-password", "--config", configPath, "--username", "admin", "--password-stdin"}, strings.NewReader("new-password-456\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Successfully reset password") {
		t.Fatalf("unexpected CLI output: %q", output.String())
	}

	store, err = database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	updated, err := store.UserByName(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordLoginDisabled || !auth.VerifyPassword(updated.PasswordHash, "new-password-456") {
		t.Fatal("CLI did not reset the configured user's password access")
	}
	if _, _, _, _, _, err := store.GetSession(t.Context(), "stale-session"); err == nil {
		t.Fatal("CLI password recovery did not revoke existing sessions")
	}
}

func TestAdminCLIRejectsMissingConfigurationAndPasswordArgument(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if err := handleAdminCLIWithIO([]string{"reset-passkeys", "--config", missing, "--username", "admin"}, strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "load configuration") {
		t.Fatalf("missing configuration did not fail safely: %v", err)
	}
	if err := handleAdminCLIWithIO([]string{"reset-password", "--password", "exposed", "--username", "admin"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("plaintext password argument was accepted")
	}
	if _, err := readPasswordLine(strings.NewReader(strings.Repeat("x", 1025))); err == nil {
		t.Fatal("password longer than 1024 bytes was accepted without a trailing newline")
	}
	if err := handleAdminCLIWithIO([]string{"reset-passkeys", "--password-stdin", "--username", "admin"}, strings.NewReader("ignored"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "only valid") {
		t.Fatalf("reset-passkeys accepted an inapplicable password flag: %v", err)
	}
}

func TestAdminCLIUsesClusterMutationTokenKeyring(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "mirrorrelay.db")
	keyPath := filepath.Join(directory, "cluster.key")
	configPath := filepath.Join(directory, "config.yaml")
	key := bytes.Repeat([]byte{0x5a}, 32)
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	configYAML := "database:\n  path: " + databasePath + "\ndistributed:\n  mutation_token_key_files:\n    - " + keyPath + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := database.Open(databasePath, database.WithClusterMutationTokenKeys(key))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.CreateInitialAdmin(t.Context(), "admin", hash); err != nil || !created {
		t.Fatalf("create initial admin: created=%v err=%v", created, err)
	}
	if _, err := store.CreateClusterNode(t.Context(), model.ClusterNode{
		Name: "edge-1", URL: "https://edge.example.test", MutationToken: "node-secret", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := handleAdminCLIWithIO([]string{"reset-passkeys", "--config", configPath, "--username", "admin"}, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Successfully cleared all passkeys") {
		t.Fatalf("unexpected CLI output: %q", output.String())
	}
}
