package main

import (
	"path/filepath"
	"testing"

	"nacos-config-sync-tools/internal/store"
)

type fakeCredentialStore struct {
	saved map[string]string
}

func (f *fakeCredentialStore) Save(target, username, password string) (string, error) {
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	ref := "fake://" + target + "/" + username
	f.saved[ref] = password
	return ref, nil
}

func (f *fakeCredentialStore) Load(ref string) (string, error) {
	return f.saved[ref], nil
}

func (f *fakeCredentialStore) Delete(ref string) error {
	delete(f.saved, ref)
	return nil
}

func TestSaveProfileStoresPasswordRefsOnly(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	creds := &fakeCredentialStore{}
	app := &App{
		store:       store.NewFileStore(settingsPath),
		credentials: creds,
	}

	cfg, err := app.SaveProfile(ScanInput{
		Source:    EndpointInput{Name: "test", BaseURL: "http://test-nacos:8848/nacos", Username: "nacos", Password: "source-secret", NamespaceID: "test-ns"},
		Target:    EndpointInput{Name: "dev", BaseURL: "http://dev-nacos:8848/nacos", Username: "nacos", Password: "target-secret", NamespaceID: "dev-ns"},
		SourceEnv: "test",
		TargetEnv: "dev",
		Group:     "DEFAULT_GROUP",
	})
	if err != nil {
		t.Fatalf("SaveProfile returned error: %v", err)
	}

	if len(cfg.Profiles) != 1 {
		t.Fatalf("profiles = %+v, want one profile", cfg.Profiles)
	}
	profile := cfg.Profiles[0]
	if profile.Source.PasswordRef == "" || profile.Target.PasswordRef == "" {
		t.Fatalf("password refs were not saved: %+v", profile)
	}
	if got, err := app.resolvePassword(EndpointInput{PasswordRef: profile.Source.PasswordRef}); err != nil || got != "source-secret" {
		t.Fatalf("source resolved password = %q, err = %v", got, err)
	}
	loaded, err := app.store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Profiles[0].Source.PasswordRef == "source-secret" || loaded.Profiles[0].Target.PasswordRef == "target-secret" {
		t.Fatalf("plaintext secret was written into settings: %+v", loaded.Profiles[0])
	}
}

func TestSavedEndpointsDeduplicatesProfilesAndLoadsPasswords(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	creds := &fakeCredentialStore{
		saved: map[string]string{
			"fake://source/local/nacos": "local-secret",
			"fake://target/dev/nacos":  "dev-secret",
		},
	}
	app := &App{
		store:       store.NewFileStore(settingsPath),
		credentials: creds,
	}
	if err := app.store.Save(store.AppConfig{
		Profiles: []store.Profile{
			{
				Name:      "local-to-dev",
				Source:    store.EndpointSettings{Name: "local", BaseURL: "http://localhost:8080", Username: "nacos", NamespaceID: "", APIVersion: "v3", PasswordRef: "fake://source/local/nacos"},
				Target:    store.EndpointSettings{Name: "dev", BaseURL: "http://dev-nacos:8080", Username: "nacos", NamespaceID: "dev", APIVersion: "v3", PasswordRef: "fake://target/dev/nacos"},
				SourceEnv: "local",
				TargetEnv: "dev",
				Group:     "DEFAULT_GROUP",
			},
			{
				Name:      "local-to-test",
				Source:    store.EndpointSettings{Name: "local copy", BaseURL: "http://localhost:8080", Username: "nacos", NamespaceID: "", APIVersion: "v3", PasswordRef: "fake://source/local/nacos"},
				Target:    store.EndpointSettings{Name: "test", BaseURL: "http://test-nacos:8848", Username: "nacos", NamespaceID: "test", APIVersion: "v2"},
				SourceEnv: "local",
				TargetEnv: "test",
				Group:     "DEFAULT_GROUP",
			},
		},
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	endpoints, err := app.SavedEndpoints()
	if err != nil {
		t.Fatalf("SavedEndpoints returned error: %v", err)
	}

	if len(endpoints) != 3 {
		t.Fatalf("endpoints = %+v, want three unique endpoints", endpoints)
	}
	if endpoints[0].BaseURL != "http://localhost:8080" || endpoints[0].Password != "local-secret" {
		t.Fatalf("first endpoint did not load saved local credential: %+v", endpoints[0])
	}
	if endpoints[1].BaseURL != "http://dev-nacos:8080" || endpoints[1].Password != "dev-secret" {
		t.Fatalf("second endpoint did not load saved dev credential: %+v", endpoints[1])
	}
	if endpoints[2].BaseURL != "http://test-nacos:8848" || endpoints[2].Password != "" {
		t.Fatalf("third endpoint should keep empty password when no ref exists: %+v", endpoints[2])
	}
}

func TestRemoveIgnoreRuleDeletesOnlyMatchingRule(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	app := &App{
		store:       store.NewFileStore(settingsPath),
		credentials: &fakeCredentialStore{},
	}
	if _, err := app.AddIgnoreRule(store.IgnoreRule{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url", Reason: "db"}); err != nil {
		t.Fatalf("AddIgnoreRule datasource returned error: %v", err)
	}
	if _, err := app.AddIgnoreRule(store.IgnoreRule{DataID: "dxtx-user-server-dev.yml", Path: "spring.cloud.nacos.discovery.server-addr", Reason: "nacos"}); err != nil {
		t.Fatalf("AddIgnoreRule nacos returned error: %v", err)
	}

	cfg, err := app.RemoveIgnoreRule(store.IgnoreRule{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url"})
	if err != nil {
		t.Fatalf("RemoveIgnoreRule returned error: %v", err)
	}

	if len(cfg.IgnoreRules) != 1 {
		t.Fatalf("ignoreRules = %+v, want one remaining rule", cfg.IgnoreRules)
	}
	if cfg.IgnoreRules[0].Path != "spring.cloud.nacos.discovery.server-addr" {
		t.Fatalf("remaining rule = %+v, want nacos server-addr rule", cfg.IgnoreRules[0])
	}
}
