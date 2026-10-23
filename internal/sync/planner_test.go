package sync_test

import (
	"context"
	"testing"

	"nacos-config-sync-tools/internal/nacos"
	configsync "nacos-config-sync-tools/internal/sync"
)

type fakeNacosClient struct {
	items        []nacos.ConfigMeta
	contents     map[string]string
	listRequests []nacos.ListRequest
	getRequests  []nacos.ConfigRequest
	published    []nacos.PublishRequest
	failPublish  bool
}

func (f *fakeNacosClient) ListConfigs(_ context.Context, req nacos.ListRequest) ([]nacos.ConfigMeta, error) {
	f.listRequests = append(f.listRequests, req)
	return f.items, nil
}

func (f *fakeNacosClient) GetConfig(_ context.Context, req nacos.ConfigRequest) (string, error) {
	f.getRequests = append(f.getRequests, req)
	return f.contents[req.DataID], nil
}

func (f *fakeNacosClient) PublishConfig(_ context.Context, req nacos.PublishRequest) error {
	if f.failPublish {
		return errPublishFailed
	}
	f.published = append(f.published, req)
	return nil
}

type testError string

func (e testError) Error() string { return string(e) }

const errPublishFailed = testError("publish failed")

func TestPlannerCreatesPlanForExistingAndMissingTargets(t *testing.T) {
	source := &fakeNacosClient{
		items: []nacos.ConfigMeta{
			{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP"},
			{DataID: "dxtx-user-server-test.yml", Group: "DEFAULT_GROUP"},
			{DataID: "dxtx-user-server-prod.yml", Group: "DEFAULT_GROUP"},
		},
		contents: map[string]string{
			"dxtx-user-server.yml":      "feature:\n  enabled: true\n",
			"dxtx-user-server-test.yml": "server:\n  port: 8080\n",
		},
	}
	target := &fakeNacosClient{
		items: []nacos.ConfigMeta{
			{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP"},
		},
		contents: map[string]string{
			"dxtx-user-server.yml": "feature:\n  enabled: false\n",
		},
	}
	planner := configsync.NewPlanner(source, target, nil)

	plan, err := planner.Build(context.Background(), configsync.BuildRequest{
		Group:             "DEFAULT_GROUP",
		SourceEnv:         "test",
		TargetEnv:         "dev",
		SourceNamespaceID: "test-ns",
		TargetNamespaceID: "dev-ns",
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(plan.Items) != 3 {
		t.Fatalf("plan item count = %d, want 3: %+v", len(plan.Items), plan.Items)
	}
	shared := plan.Find("dxtx-user-server.yml")
	if shared == nil || shared.Status != configsync.ItemStatusChanged || !shared.TargetExists {
		t.Fatalf("shared item = %+v, want changed existing target", shared)
	}
	env := plan.Find("dxtx-user-server-test.yml")
	if env == nil || env.TargetDataID != "dxtx-user-server-dev.yml" || env.Status != configsync.ItemStatusCreate || env.TargetExists {
		t.Fatalf("env item = %+v, want create dxtx-user-server-dev.yml", env)
	}
	skipped := plan.Find("dxtx-user-server-prod.yml")
	if skipped == nil || skipped.Status != configsync.ItemStatusSkipped {
		t.Fatalf("prod item = %+v, want skipped", skipped)
	}
	if source.listRequests[0].NamespaceID != "test-ns" {
		t.Fatalf("source list namespace = %q, want test-ns", source.listRequests[0].NamespaceID)
	}
	if target.listRequests[0].NamespaceID != "dev-ns" {
		t.Fatalf("target list namespace = %q, want dev-ns", target.listRequests[0].NamespaceID)
	}
	if source.getRequests[0].NamespaceID != "test-ns" {
		t.Fatalf("source get namespace = %q, want test-ns", source.getRequests[0].NamespaceID)
	}
	if target.getRequests[0].NamespaceID != "dev-ns" {
		t.Fatalf("target get namespace = %q, want dev-ns", target.getRequests[0].NamespaceID)
	}
}

func TestPlannerMarksTargetExtraWhenTargetOnlyHasAdditionalValues(t *testing.T) {
	source := &fakeNacosClient{
		items: []nacos.ConfigMeta{{DataID: "dxtx-gateway-server.yml", Group: "DEFAULT_GROUP"}},
		contents: map[string]string{
			"dxtx-gateway-server.yml": `
auth:
  exclude-paths:
    - /gateway/**
    - /admin/**
`,
		},
	}
	target := &fakeNacosClient{
		items: []nacos.ConfigMeta{{DataID: "dxtx-gateway-server.yml", Group: "DEFAULT_GROUP"}},
		contents: map[string]string{
			"dxtx-gateway-server.yml": `
auth:
  exclude-paths:
    - /gateway/**
    - /admin/**
    - /doc.html
feature:
  target-only: true
`,
		},
	}
	planner := configsync.NewPlanner(source, target, nil)

	plan, err := planner.Build(context.Background(), configsync.BuildRequest{
		Group:     "DEFAULT_GROUP",
		SourceEnv: "local",
		TargetEnv: "dev",
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	item := plan.Find("dxtx-gateway-server.yml")
	if item == nil {
		t.Fatal("expected plan item for gateway config")
	}
	if item.Status != configsync.ItemStatusTargetExtra {
		t.Fatalf("status = %q, want target extra; item = %+v", item.Status, item)
	}
}

func TestExecutorPublishesSelectedPreviewContentOnly(t *testing.T) {
	target := &fakeNacosClient{}
	executor := configsync.NewExecutor(target)
	plan := configsync.Plan{
		Group:             "DEFAULT_GROUP",
		TargetNamespaceID: "dev-ns",
		Items: []configsync.PlanItem{
			{SourceDataID: "a-test.yml", TargetDataID: "a-dev.yml", Status: configsync.ItemStatusChanged, PreviewContent: "a: 1\n"},
			{SourceDataID: "b-test.yml", TargetDataID: "b-dev.yml", Status: configsync.ItemStatusSkipped, PreviewContent: "b: 1\n"},
		},
	}

	result, err := executor.Publish(context.Background(), plan, []string{"a-dev.yml"}, nil)
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	if len(target.published) != 1 {
		t.Fatalf("published count = %d, want 1", len(target.published))
	}
	if target.published[0].DataID != "a-dev.yml" || target.published[0].Content != "a: 1\n" {
		t.Fatalf("published request = %+v", target.published[0])
	}
	if target.published[0].NamespaceID != "dev-ns" {
		t.Fatalf("publish namespace = %q, want dev-ns", target.published[0].NamespaceID)
	}
	if len(result.Logs) != 1 || !result.Logs[0].Success {
		t.Fatalf("result logs = %+v, want one success log", result.Logs)
	}
}

func TestExecutorSkipsTargetExtraItems(t *testing.T) {
	target := &fakeNacosClient{}
	executor := configsync.NewExecutor(target)
	plan := configsync.Plan{
		Group:             "DEFAULT_GROUP",
		TargetNamespaceID: "dev-ns",
		Items: []configsync.PlanItem{
			{SourceDataID: "a.yml", TargetDataID: "a.yml", Status: configsync.ItemStatusTargetExtra, PreviewContent: "a: 1\n"},
		},
	}

	result, err := executor.Publish(context.Background(), plan, []string{"a.yml"}, nil)
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if len(target.published) != 0 {
		t.Fatalf("published target extra item, requests = %+v", target.published)
	}
	if len(result.Logs) != 0 {
		t.Fatalf("logs = %+v, want no publish logs", result.Logs)
	}
}

func TestExecutorPublishesEditedContentWhenProvided(t *testing.T) {
	target := &fakeNacosClient{}
	executor := configsync.NewExecutor(target)
	plan := configsync.Plan{
		Group:             "DEFAULT_GROUP",
		TargetNamespaceID: "dev-ns",
		Items: []configsync.PlanItem{
			{SourceDataID: "a-test.yml", TargetDataID: "a-dev.yml", Status: configsync.ItemStatusChanged, PreviewContent: "preview: true\n"},
		},
	}

	result, err := executor.Publish(context.Background(), plan, []string{"a-dev.yml"}, map[string]string{
		"a-dev.yml": "edited: true\n",
	})
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	if len(result.Logs) != 1 || !result.Logs[0].Success {
		t.Fatalf("result logs = %+v, want one success log", result.Logs)
	}
	if len(target.published) != 1 || target.published[0].Content != "edited: true\n" {
		t.Fatalf("published request = %+v, want edited content", target.published)
	}
}
