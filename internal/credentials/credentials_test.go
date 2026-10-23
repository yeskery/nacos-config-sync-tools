package credentials_test

import (
	"path/filepath"
	"testing"

	"nacos-config-sync-tools/internal/credentials"
)

func TestMakeAndParseRef(t *testing.T) {
	ref := credentials.MakeRef("nacos-config-sync/source/http://test-nacos:8848/nacos", "nacos/user")

	target, username, err := credentials.ParseRef(ref)
	if err != nil {
		t.Fatalf("ParseRef returned error: %v", err)
	}
	if target != "nacos-config-sync/source/http://test-nacos:8848/nacos" {
		t.Fatalf("target = %q", target)
	}
	if username != "nacos/user" {
		t.Fatalf("username = %q", username)
	}
}

func TestParseRefRejectsUnsupportedSchemes(t *testing.T) {
	if _, _, err := credentials.ParseRef("manual://source"); err == nil {
		t.Fatal("expected unsupported scheme to return an error")
	}
}

func TestFileStoreSaveLoadDelete(t *testing.T) {
	store := credentials.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	ref, err := store.Save("nacos-config-sync-tools/source/http://localhost:8848/", "nacos", "secret-pass")
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := store.Load(ref)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded != "secret-pass" {
		t.Fatalf("loaded = %q, want %q", loaded, "secret-pass")
	}
	if err := store.Delete(ref); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := store.Load(ref); err == nil {
		t.Fatal("expected Load after Delete to return an error")
	}
}
