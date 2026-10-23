package rules

import (
	"net"
	"regexp"
	"strings"
)

type SensitiveKind string

const (
	SensitiveNone  SensitiveKind = "none"
	SensitiveIP    SensitiveKind = "ip"
	SensitiveURL   SensitiveKind = "url"
	SensitiveJDBC  SensitiveKind = "jdbc"
	SensitiveRedis SensitiveKind = "redis"
	SensitiveMQ    SensitiveKind = "mq"
	SensitiveHost  SensitiveKind = "host"
)

type IgnoreRule struct {
	DataID string `json:"dataId"`
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

type RuleSet struct {
	Rules []IgnoreRule `json:"rules"`
}

var (
	urlRe   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s'"]+`)
	hostRe  = regexp.MustCompile(`(?i)\b([a-z0-9-]+\.)+[a-z]{2,}\b`)
	ipRe    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	mqHints = []string{"rocketmq", "namesrv", "kafka", "rabbitmq", "amqp://", "pulsar://"}
	tlds    = map[string]struct{}{
		"cn": {}, "com": {}, "net": {}, "org": {}, "io": {}, "dev": {}, "top": {}, "vip": {},
		"cc": {}, "me": {}, "co": {}, "app": {}, "cloud": {}, "tech": {}, "site": {}, "xyz": {},
	}
)

func (r RuleSet) ShouldIgnore(dataID, path string) bool {
	for _, rule := range r.Rules {
		if (rule.DataID == "" || rule.DataID == dataID) && rule.Path == path {
			return true
		}
	}
	return false
}

func (r *RuleSet) Add(rule IgnoreRule) {
	if r.ShouldIgnore(rule.DataID, rule.Path) {
		return
	}
	r.Rules = append(r.Rules, rule)
}

func DetectSensitiveKind(value string) SensitiveKind {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "jdbc:"):
		return SensitiveJDBC
	case strings.Contains(lower, "redis://") || strings.Contains(lower, "redis.") || strings.Contains(lower, "redis:"):
		return SensitiveRedis
	case containsAny(lower, mqHints):
		return SensitiveMQ
	case urlRe.MatchString(value):
		return SensitiveURL
	}

	if match := ipRe.FindString(value); match != "" {
		if parsed := net.ParseIP(match); parsed != nil {
			return SensitiveIP
		}
	}
	if looksLikeDomain(value) {
		return SensitiveHost
	}
	return SensitiveNone
}

func looksLikeDomain(value string) bool {
	for _, match := range hostRe.FindAllString(value, -1) {
		parts := strings.Split(strings.ToLower(match), ".")
		if len(parts) < 2 {
			continue
		}
		if _, ok := tlds[parts[len(parts)-1]]; ok {
			return true
		}
	}
	return false
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
