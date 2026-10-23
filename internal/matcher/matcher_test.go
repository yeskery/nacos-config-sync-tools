package matcher_test

import (
	"testing"

	"nacos-config-sync-tools/internal/matcher"
)

func TestTargetDataIDMapsEnvironmentSpecificConfig(t *testing.T) {
	got, info := matcher.TargetDataID("dxtx-user-server-test.yml", "test", "dev", matcher.DefaultEnvironments)

	if got != "dxtx-user-server-dev.yml" {
		t.Fatalf("target dataId = %q, want %q", got, "dxtx-user-server-dev.yml")
	}
	if info.Kind != matcher.KindEnvironment || info.Status != matcher.StatusMatched {
		t.Fatalf("info = %+v, want environment matched", info)
	}
}

func TestTargetDataIDKeepsSharedConfigName(t *testing.T) {
	got, info := matcher.TargetDataID("dxtx-user-server.yml", "test", "dev", matcher.DefaultEnvironments)

	if got != "dxtx-user-server.yml" {
		t.Fatalf("target dataId = %q, want shared dataId unchanged", got)
	}
	if info.Kind != matcher.KindShared || info.Status != matcher.StatusMatched {
		t.Fatalf("info = %+v, want shared matched", info)
	}
}

func TestTargetDataIDSkipsOtherEnvironmentConfig(t *testing.T) {
	got, info := matcher.TargetDataID("dxtx-user-server-prod.yml", "test", "dev", matcher.DefaultEnvironments)

	if got != "" {
		t.Fatalf("target dataId = %q, want skipped empty target", got)
	}
	if info.Status != matcher.StatusSkippedEnvironment {
		t.Fatalf("status = %q, want %q", info.Status, matcher.StatusSkippedEnvironment)
	}
}

func TestTargetDataIDRejectsUnsupportedNames(t *testing.T) {
	got, info := matcher.TargetDataID("README.md", "test", "dev", matcher.DefaultEnvironments)

	if got != "" {
		t.Fatalf("target dataId = %q, want empty target", got)
	}
	if info.Status != matcher.StatusInvalid {
		t.Fatalf("status = %q, want %q", info.Status, matcher.StatusInvalid)
	}
}
