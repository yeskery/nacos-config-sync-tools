package matcher

import (
	"fmt"
	"slices"
	"strings"
)

var DefaultEnvironments = []string{"local", "dev", "test", "prod"}

type Kind string

const (
	KindShared      Kind = "shared"
	KindEnvironment Kind = "environment"
	KindUnknown     Kind = "unknown"
)

type Status string

const (
	StatusMatched            Status = "matched"
	StatusSkippedEnvironment Status = "skipped_environment"
	StatusInvalid            Status = "invalid"
)

type MatchInfo struct {
	SourceDataID string `json:"sourceDataId"`
	TargetDataID string `json:"targetDataId"`
	ServiceName  string `json:"serviceName"`
	Kind         Kind   `json:"kind"`
	Status       Status `json:"status"`
	Message      string `json:"message,omitempty"`
}

func TargetDataID(sourceDataID, sourceEnv, targetEnv string, environments []string) (string, MatchInfo) {
	info := MatchInfo{SourceDataID: sourceDataID, Kind: KindUnknown}
	if !strings.HasSuffix(sourceDataID, ".yml") && !strings.HasSuffix(sourceDataID, ".yaml") {
		info.Status = StatusInvalid
		info.Message = "only .yml and .yaml dataIds are supported"
		return "", info
	}

	ext := extension(sourceDataID)
	base := strings.TrimSuffix(sourceDataID, ext)
	if base == "" {
		info.Status = StatusInvalid
		info.Message = "empty service name"
		return "", info
	}

	for _, env := range environments {
		suffix := "-" + env
		if strings.HasSuffix(base, suffix) {
			service := strings.TrimSuffix(base, suffix)
			if service == "" {
				info.Status = StatusInvalid
				info.Message = "empty service name"
				return "", info
			}
			info.ServiceName = service
			info.Kind = KindEnvironment
			if env != sourceEnv {
				info.Status = StatusSkippedEnvironment
				info.Message = fmt.Sprintf("source dataId belongs to %s environment, not %s", env, sourceEnv)
				return "", info
			}
			target := service + "-" + targetEnv + ext
			info.TargetDataID = target
			info.Status = StatusMatched
			return target, info
		}
	}

	if strings.Contains(base, "-") {
		lastSegment := base[strings.LastIndex(base, "-")+1:]
		if looksLikeEnv(lastSegment) && !slices.Contains(environments, lastSegment) {
			info.Status = StatusInvalid
			info.Message = "dataId has an unsupported environment suffix"
			return "", info
		}
	}

	info.ServiceName = base
	info.Kind = KindShared
	info.TargetDataID = sourceDataID
	info.Status = StatusMatched
	return sourceDataID, info
}

func extension(dataID string) string {
	if strings.HasSuffix(dataID, ".yaml") {
		return ".yaml"
	}
	return ".yml"
}

func looksLikeEnv(value string) bool {
	switch strings.ToLower(value) {
	case "local", "dev", "test", "prod", "stage", "staging", "pre", "uat", "sit":
		return true
	default:
		return false
	}
}
