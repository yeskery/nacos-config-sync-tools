package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nacos-config-sync-tools/internal/store"
)

func TestFileStoreSavesProfilesRulesAndNoPlaintextPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	fileStore := store.NewFileStore(path)
	cfg := store.AppConfig{
		Profiles: []store.Profile{
			{
				Name:      "test-to-dev",
				Source:    store.EndpointSettings{Name: "test", BaseURL: "http://test-nacos:8848/nacos", Username: "nacos", PasswordRef: "keychain://source"},
				Target:    store.EndpointSettings{Name: "dev", BaseURL: "http://dev-nacos:8848/nacos", Username: "nacos", PasswordRef: "keychain://target"},
				SourceEnv: "test",
				TargetEnv: "dev",
				Group:     "DEFAULT_GROUP",
			},
		},
		IgnoreRules: []store.IgnoreRule{{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url", Reason: "environment database"}},
	}

	if err := fileStore.Save(cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("settings file should not contain plaintext password material:\n%s", string(raw))
	}

	loaded, err := fileStore.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Profiles) != 1 || loaded.Profiles[0].Name != "test-to-dev" {
		t.Fatalf("loaded profiles = %+v", loaded.Profiles)
	}
	if len(loaded.IgnoreRules) != 1 || loaded.IgnoreRules[0].Path != "spring.datasource.url" {
		t.Fatalf("loaded rules = %+v", loaded.IgnoreRules)
	}
}
