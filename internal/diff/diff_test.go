package diff_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	configdiff "nacos-config-sync-tools/internal/diff"
	"nacos-config-sync-tools/internal/rules"
)

func TestCompareYAMLFindsUnignoredChangesAndSensitiveWarnings(t *testing.T) {
	source := `
server:
  port: 8080
spring:
  datasource:
    url: jdbc:mysql://10.0.0.1:3306/test
feature:
  enabled: true
`
	target := `
server:
  port: 8081
spring:
  datasource:
    url: jdbc:mysql://10.0.1.1:3306/dev
redis:
  address: redis://127.0.0.1:6379/0
`
	ruleSet := rules.RuleSet{
		Rules: []rules.IgnoreRule{
			{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url"},
		},
	}

	result, err := configdiff.CompareYAML("dxtx-user-server-dev.yml", source, target, ruleSet)
	if err != nil {
		t.Fatalf("CompareYAML returned error: %v", err)
	}

	if result.HasChange("spring.datasource.url") {
		t.Fatalf("ignored datasource URL should not be returned as a sync change: %+v", result.Changes)
	}
	if !result.HasChange("server.port") {
		t.Fatalf("expected server.port change, got %+v", result.Changes)
	}
	if !result.HasChange("feature.enabled") {
		t.Fatalf("expected feature.enabled add, got %+v", result.Changes)
	}
	if !result.HasWarning("redis.address") {
		t.Fatalf("expected target-only redis.address sensitive warning, got %+v", result.Warnings)
	}
}

func TestCompareYAMLDoesNotWarnForUnchangedSensitiveValues(t *testing.T) {
	source := `
spring:
  datasource:
    url: jdbc:mysql://192.168.110.188:3306/dxtx_user
user:
  defaultAvatar: https://assets-dev.dongvoyage.cn/static/images/default/avatar.png
rocketmq:
  name-server: 192.168.110.188:9876
`
	target := `
spring:
  datasource:
    url: jdbc:mysql://192.168.110.188:3306/dxtx_user
user:
  defaultAvatar: https://assets-dev.dongvoyage.cn/static/images/default/avatar.png
rocketmq:
  name-server: 192.168.110.188:9876
`

	result, err := configdiff.CompareYAML("dxtx-user-server-dev.yml", source, target, rules.RuleSet{})
	if err != nil {
		t.Fatalf("CompareYAML returned error: %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("changes = %+v, want none", result.Changes)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none for unchanged sensitive values", result.Warnings)
	}
}

func TestCompareYAMLReportsSourceOnlyListItems(t *testing.T) {
	source := `
spring:
  cloud:
    gateway:
      routes:
        - id: dxtx-aggregation-server
          uri: lb://dxtx-aggregation-server
        - id: dxtx-pay-server
          uri: lb://dxtx-pay-server
sa-token:
  exclude-paths:
    - /travel/product/recommended/grouping
    - /pay/pay/order/notify
    - /travel/product/detail/**
`
	target := `
spring:
  cloud:
    gateway:
      routes:
        - id: dxtx-aggregation-server
          uri: lb://dxtx-aggregation-server
sa-token:
  exclude-paths:
    - /travel/product/recommended/grouping
`

	result, err := configdiff.CompareYAML("gateway.yml", source, target, rules.RuleSet{})
	if err != nil {
		t.Fatalf("CompareYAML returned error: %v", err)
	}

	if !hasListItem(result, "spring.cloud.gateway.routes", "id=dxtx-pay-server", "source") {
		t.Fatalf("expected source-only route list item, got %+v", result.Changes)
	}
	if !hasListItem(result, "sa-token.exclude-paths", "/pay/pay/order/notify", "source") {
		t.Fatalf("expected source-only notify path item, got %+v", result.Changes)
	}
	if !hasListItem(result, "sa-token.exclude-paths", "/travel/product/detail/**", "source") {
		t.Fatalf("expected source-only detail path item, got %+v", result.Changes)
	}
}

func TestCompareYAMLIgnoresSingleListItemRule(t *testing.T) {
	source := `
sys:
  auth:
    exclude-paths:
    - /gateway/**
    - /pay/pay/order/notify
    - /travel/product/detail/**
`
	target := `
sys:
  auth:
    exclude-paths:
    - /gateway/**
`
	ruleSet := rules.RuleSet{Rules: []rules.IgnoreRule{
		{DataID: "gateway.yml", Path: "sys.auth.exclude-paths[/pay/pay/order/notify]"},
	}}

	result, err := configdiff.CompareYAML("gateway.yml", source, target, ruleSet)
	if err != nil {
		t.Fatalf("CompareYAML returned error: %v", err)
	}
	if hasListItem(result, "sys.auth.exclude-paths", "/pay/pay/order/notify", "source") {
		t.Fatalf("ignored list item should not be returned, got %+v", result.Changes)
	}
	if !hasListItem(result, "sys.auth.exclude-paths", "/travel/product/detail/**", "source") {
		t.Fatalf("non-ignored list item should still be returned, got %+v", result.Changes)
	}
}

func TestApplySourcePathPreservesTargetOrderAndComments(t *testing.T) {
	source := `
server:
  port: 8100
spring:
  application:
    name: user
feature:
  enabled: true
`
	target := `
# target comment
aliyun:
  oss:
    bucket: dev
server:
  # keep server comment
  port: 8101
spring:
  application:
    name: user
`

	merged, err := configdiff.ApplySourcePath(source, target, "server.port")
	if err != nil {
		t.Fatalf("ApplySourcePath returned error: %v", err)
	}
	if !strings.Contains(merged, "port: 8100") {
		t.Fatalf("merged YAML did not apply source port:\n%s", merged)
	}
	if !strings.Contains(merged, "target comment") || !strings.Contains(merged, "keep server comment") {
		t.Fatalf("merged YAML should preserve target comments:\n%s", merged)
	}
	if strings.Index(merged, "aliyun:") > strings.Index(merged, "server:") {
		t.Fatalf("top-level target order changed:\n%s", merged)
	}
	if strings.Contains(merged, "feature:") {
		t.Fatalf("single-path apply should not insert unrelated source keys:\n%s", merged)
	}
}

func hasListItem(result configdiff.Result, path, key, side string) bool {
	for _, change := range result.Changes {
		if change.Path != path {
			continue
		}
		for _, item := range change.ListItems {
			if item.Key == key && item.Side == side {
				return true
			}
		}
	}
	return false
}

func TestApplySourcePathInsertsMissingKeyUsingSourceSiblingOrder(t *testing.T) {
	source := `
youzan:
  rate-limit:
    enabled: true
    qps: 15
    backgroundQps: 10
    realtime-wait-ms: 1500
    background-log-wait-ms: 10000
`
	target := `
youzan:
  rate-limit:
    enabled: true
    qps: 15
    realtime-wait-ms: 1500
    background-log-wait-ms: 10000
    redis-key-prefix: yz-health:rate-limit:youzan-api
`

	merged, err := configdiff.ApplySourcePath(source, target, "youzan.rate-limit.backgroundQps")
	if err != nil {
		t.Fatalf("ApplySourcePath returned error: %v", err)
	}

	background := strings.Index(merged, "backgroundQps: 10")
	realtime := strings.Index(merged, "realtime-wait-ms: 1500")
	redis := strings.Index(merged, "redis-key-prefix:")
	if background == -1 {
		t.Fatalf("merged YAML did not insert backgroundQps:\n%s", merged)
	}
	if !(background < realtime && background < redis) {
		t.Fatalf("backgroundQps should be inserted near source sibling order, got:\n%s", merged)
	}
}

func TestMergeYAMLPreservesIgnoredTargetValues(t *testing.T) {
	source := `
server:
  port: 8080
spring:
  datasource:
    url: jdbc:mysql://10.0.0.1:3306/test
feature:
  enabled: true
`
	target := `
server:
  port: 8081
spring:
  datasource:
    url: jdbc:mysql://10.0.1.1:3306/dev
`
	ruleSet := rules.RuleSet{
		Rules: []rules.IgnoreRule{
			{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url"},
		},
	}

	merged, err := configdiff.MergeYAML("dxtx-user-server-dev.yml", source, target, ruleSet)
	if err != nil {
		t.Fatalf("MergeYAML returned error: %v", err)
	}

	var out map[string]any
	if err := yaml.Unmarshal([]byte(merged), &out); err != nil {
		t.Fatalf("merged YAML is invalid: %v\n%s", err, merged)
	}

	server := out["server"].(map[string]any)
	if server["port"] != 8080 {
		t.Fatalf("server.port = %v, want 8080", server["port"])
	}
	spring := out["spring"].(map[string]any)
	datasource := spring["datasource"].(map[string]any)
	if datasource["url"] != "jdbc:mysql://10.0.1.1:3306/dev" {
		t.Fatalf("datasource url = %v, want target-side ignored value", datasource["url"])
	}
	feature := out["feature"].(map[string]any)
	if feature["enabled"] != true {
		t.Fatalf("feature.enabled = %v, want true", feature["enabled"])
	}
}

func TestApplyListItemChangeAddsSingleSourceItem(t *testing.T) {
	source := `
auth:
  exclude-paths:
    - /gateway/**
    - /admin/**
    - /doc.html
`
	target := `
auth:
  exclude-paths:
    - /gateway/**
    - /doc.html
`

	merged, err := configdiff.ApplyListItemChange(source, target, "auth.exclude-paths", "/admin/**", "add")
	if err != nil {
		t.Fatalf("ApplyListItemChange add returned error: %v", err)
	}

	if !strings.Contains(merged, "- /admin/**") {
		t.Fatalf("merged YAML did not add /admin/**:\n%s", merged)
	}
	if strings.Index(merged, "- /gateway/**") > strings.Index(merged, "- /admin/**") {
		t.Fatalf("added item should stay after /gateway/**:\n%s", merged)
	}
	if strings.Index(merged, "- /admin/**") > strings.Index(merged, "- /doc.html") {
		t.Fatalf("added item should stay before /doc.html:\n%s", merged)
	}
}

func TestApplyListItemChangeAddsObjectItemByIdentityKey(t *testing.T) {
	source := `
spring:
  cloud:
    gateway:
      routes:
        - id: dxtx-admin-server
          uri: lb://dxtx-admin-server
        - id: dxtx-pay-server
          uri: lb://dxtx-pay-server
          predicates:
            - Path=/pay/**
`
	target := `
spring:
  cloud:
    gateway:
      routes:
        - id: dxtx-admin-server
          uri: lb://dxtx-admin-server
`

	merged, err := configdiff.ApplyListItemChange(source, target, "spring.cloud.gateway.routes", "id=dxtx-pay-server", "add")
	if err != nil {
		t.Fatalf("ApplyListItemChange add returned error: %v", err)
	}

	if !strings.Contains(merged, "id: dxtx-pay-server") {
		t.Fatalf("merged YAML did not add dxtx-pay-server route:\n%s", merged)
	}
	if !strings.Contains(merged, "Path=/pay/**") {
		t.Fatalf("merged YAML did not keep nested route fields:\n%s", merged)
	}
	if strings.Index(merged, "id: dxtx-admin-server") > strings.Index(merged, "id: dxtx-pay-server") {
		t.Fatalf("added object item should stay after existing source sibling:\n%s", merged)
	}
}

func TestApplyListItemChangeDeletesSingleTargetItem(t *testing.T) {
	source := `
auth:
  exclude-paths:
    - /gateway/**
    - /admin/**
`
	target := `
auth:
  exclude-paths:
    - /gateway/**
    - /admin/**
    - /doc.html
    - /webjars/**
`

	merged, err := configdiff.ApplyListItemChange(source, target, "auth.exclude-paths", "/doc.html", "delete")
	if err != nil {
		t.Fatalf("ApplyListItemChange delete returned error: %v", err)
	}

	if strings.Contains(merged, "- /doc.html") {
		t.Fatalf("merged YAML should delete only /doc.html:\n%s", merged)
	}
	if !strings.Contains(merged, "- /webjars/**") {
		t.Fatalf("merged YAML should keep other target-only items:\n%s", merged)
	}
}
