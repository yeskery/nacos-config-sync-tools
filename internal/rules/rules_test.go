package rules_test

import (
	"testing"

	"nacos-config-sync-tools/internal/rules"
)

func TestDetectSensitiveValues(t *testing.T) {
	cases := map[string]rules.SensitiveKind{
		"10.20.30.40":                                       rules.SensitiveIP,
		"https://api-test.example.com/callback":             rules.SensitiveURL,
		"jdbc:mysql://10.0.0.2:3306/dxtx_user?useSSL=false": rules.SensitiveJDBC,
		"redis://:pwd@127.0.0.1:6379/0":                     rules.SensitiveRedis,
		"rocketmq.namesrv.addr=10.0.0.3:9876":               rules.SensitiveMQ,
		"plain-feature-switch":                              rules.SensitiveNone,
		"com.alibaba.druid.pool.DruidDataSource":            rules.SensitiveNone,
		"cn.dongxingtx.user.controller.internal":            rules.SensitiveNone,
		"cn.dongxingtx.user.controller,cn.dongxingtx.feign": rules.SensitiveNone,
	}

	for input, want := range cases {
		if got := rules.DetectSensitiveKind(input); got != want {
			t.Fatalf("DetectSensitiveKind(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRuleSetIgnoresByDataIDAndPath(t *testing.T) {
	set := rules.RuleSet{
		Rules: []rules.IgnoreRule{
			{DataID: "dxtx-user-server-dev.yml", Path: "spring.datasource.url", Reason: "environment database"},
		},
	}

	if !set.ShouldIgnore("dxtx-user-server-dev.yml", "spring.datasource.url") {
		t.Fatal("expected configured dataId/path to be ignored")
	}
	if set.ShouldIgnore("dxtx-user-server-test.yml", "spring.datasource.url") {
		t.Fatal("did not expect a different dataId to be ignored")
	}
}
