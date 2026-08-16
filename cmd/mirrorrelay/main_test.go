package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
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
