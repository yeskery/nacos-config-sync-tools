package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	configdiff "nacos-config-sync-tools/internal/diff"
	"nacos-config-sync-tools/internal/matcher"
	"nacos-config-sync-tools/internal/nacos"
	"nacos-config-sync-tools/internal/rules"
)

type NacosClient interface {
	ListConfigs(ctx context.Context, req nacos.ListRequest) ([]nacos.ConfigMeta, error)
	GetConfig(ctx context.Context, req nacos.ConfigRequest) (string, error)
	PublishConfig(ctx context.Context, req nacos.PublishRequest) error
}

type ItemStatus string

const (
	ItemStatusUnchanged   ItemStatus = "unchanged"
	ItemStatusChanged     ItemStatus = "changed"
	ItemStatusCreate      ItemStatus = "create"
	ItemStatusTargetExtra ItemStatus = "targetExtra"
	ItemStatusSkipped     ItemStatus = "skipped"
	ItemStatusInvalid     ItemStatus = "invalid"
	ItemStatusError       ItemStatus = "error"
)

type BuildRequest struct {
	Group             string `json:"group"`
	NamespaceID       string `json:"namespaceId,omitempty"`
	SourceNamespaceID string `json:"sourceNamespaceId,omitempty"`
	TargetNamespaceID string `json:"targetNamespaceId,omitempty"`
	SourceEnv         string `json:"sourceEnv"`
	TargetEnv         string `json:"targetEnv"`
}

type Plan struct {
	Group             string     `json:"group"`
	NamespaceID       string     `json:"namespaceId,omitempty"`
	SourceNamespaceID string     `json:"sourceNamespaceId,omitempty"`
	TargetNamespaceID string     `json:"targetNamespaceId,omitempty"`
	SourceEnv         string     `json:"sourceEnv"`
	TargetEnv         string     `json:"targetEnv"`
	Items             []PlanItem `json:"items"`
}

func (p Plan) Find(sourceDataID string) *PlanItem {
	for i := range p.Items {
		if p.Items[i].SourceDataID == sourceDataID {
			return &p.Items[i]
		}
	}
	return nil
}

type PlanItem struct {
	SourceDataID   string               `json:"sourceDataId"`
	TargetDataID   string               `json:"targetDataId,omitempty"`
	ServiceName    string               `json:"serviceName,omitempty"`
	Kind           matcher.Kind         `json:"kind"`
	Status         ItemStatus           `json:"status"`
	TargetExists   bool                 `json:"targetExists"`
	SourceContent  string               `json:"sourceContent,omitempty"`
	TargetContent  string               `json:"targetContent,omitempty"`
	PreviewContent string               `json:"previewContent,omitempty"`
	Changes        []configdiff.Change  `json:"changes,omitempty"`
	Warnings       []configdiff.Warning `json:"warnings,omitempty"`
	Message        string               `json:"message,omitempty"`
}

type Planner struct {
	source NacosClient
	target NacosClient
	rules  rules.RuleSet
}

func NewPlanner(source, target NacosClient, ruleSet *rules.RuleSet) *Planner {
	var rs rules.RuleSet
	if ruleSet != nil {
		rs = *ruleSet
	}
	return &Planner{source: source, target: target, rules: rs}
}

func (p *Planner) Build(ctx context.Context, req BuildRequest) (Plan, error) {
	sourceNamespaceID := namespaceOrFallback(req.SourceNamespaceID, req.NamespaceID)
	targetNamespaceID := namespaceOrFallback(req.TargetNamespaceID, req.NamespaceID)
	sourceItems, err := p.source.ListConfigs(ctx, nacos.ListRequest{Group: req.Group, NamespaceID: sourceNamespaceID})
	if err != nil {
		return Plan{}, err
	}
	targetItems, err := p.target.ListConfigs(ctx, nacos.ListRequest{Group: req.Group, NamespaceID: targetNamespaceID})
	if err != nil {
		return Plan{}, err
	}
	targets := map[string]nacos.ConfigMeta{}
	for _, item := range targetItems {
		targets[item.DataID] = item
	}

	plan := Plan{
		Group:             req.Group,
		NamespaceID:       req.NamespaceID,
		SourceNamespaceID: sourceNamespaceID,
		TargetNamespaceID: targetNamespaceID,
		SourceEnv:         req.SourceEnv,
		TargetEnv:         req.TargetEnv,
	}
	for _, sourceMeta := range sourceItems {
		targetDataID, match := matcher.TargetDataID(sourceMeta.DataID, req.SourceEnv, req.TargetEnv, matcher.DefaultEnvironments)
		item := PlanItem{
			SourceDataID: sourceMeta.DataID,
			TargetDataID: targetDataID,
			ServiceName:  match.ServiceName,
			Kind:         match.Kind,
			Message:      match.Message,
		}
		if match.Status == matcher.StatusSkippedEnvironment {
			item.Status = ItemStatusSkipped
			plan.Items = append(plan.Items, item)
			continue
		}
		if match.Status == matcher.StatusInvalid {
			item.Status = ItemStatusInvalid
			plan.Items = append(plan.Items, item)
			continue
		}

		sourceContent, err := p.source.GetConfig(ctx, nacos.ConfigRequest{DataID: sourceMeta.DataID, Group: req.Group, NamespaceID: sourceNamespaceID})
		if err != nil {
			item.Status = ItemStatusError
			item.Message = err.Error()
			plan.Items = append(plan.Items, item)
			continue
		}
		targetContent := ""
		_, item.TargetExists = targets[targetDataID]
		if item.TargetExists {
			targetContent, err = p.target.GetConfig(ctx, nacos.ConfigRequest{DataID: targetDataID, Group: req.Group, NamespaceID: targetNamespaceID})
			if err != nil {
				item.Status = ItemStatusError
				item.Message = err.Error()
				plan.Items = append(plan.Items, item)
				continue
			}
		}

		result, err := configdiff.CompareYAML(targetDataID, sourceContent, targetContent, p.rules)
		if err != nil {
			item.Status = ItemStatusError
			item.Message = err.Error()
			plan.Items = append(plan.Items, item)
			continue
		}
		preview, err := configdiff.MergeYAML(targetDataID, sourceContent, targetContent, p.rules)
		if err != nil {
			item.Status = ItemStatusError
			item.Message = err.Error()
			plan.Items = append(plan.Items, item)
			continue
		}
		item.SourceContent = sourceContent
		item.TargetContent = targetContent
		item.PreviewContent = preview
		item.Changes = result.Changes
		item.Warnings = result.Warnings
		switch {
		case !item.TargetExists:
			item.Status = ItemStatusCreate
		case len(result.Changes) == 0:
			item.Status = ItemStatusUnchanged
		case targetExtraOnly(result.Changes):
			item.Status = ItemStatusTargetExtra
		default:
			item.Status = ItemStatusChanged
		}
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

type PublishResult struct {
	Logs []OperationLog `json:"logs"`
}

type OperationLog struct {
	SourceDataID string    `json:"sourceDataId"`
	TargetDataID string    `json:"targetDataId"`
	ChangedPaths []string  `json:"changedPaths"`
	Time         time.Time `json:"time"`
	Success      bool      `json:"success"`
	Error        string    `json:"error,omitempty"`
}

type Executor struct {
	target NacosClient
}

func NewExecutor(target NacosClient) *Executor {
	return &Executor{target: target}
}

func (e *Executor) Publish(ctx context.Context, plan Plan, selectedTargetDataIDs []string, editedContents map[string]string) (PublishResult, error) {
	selected := map[string]struct{}{}
	for _, dataID := range selectedTargetDataIDs {
		selected[dataID] = struct{}{}
	}
	var result PublishResult
	for _, item := range plan.Items {
		if _, ok := selected[item.TargetDataID]; !ok {
			continue
		}
		if item.Status != ItemStatusChanged && item.Status != ItemStatusCreate {
			continue
		}
		content := item.PreviewContent
		if edited, ok := editedContents[item.TargetDataID]; ok {
			content = edited
		}
		log := OperationLog{
			SourceDataID: item.SourceDataID,
			TargetDataID: item.TargetDataID,
			ChangedPaths: changePaths(item.Changes),
			Time:         time.Now(),
		}
		err := e.target.PublishConfig(ctx, nacos.PublishRequest{
			DataID:      item.TargetDataID,
			Group:       plan.Group,
			NamespaceID: namespaceOrFallback(plan.TargetNamespaceID, plan.NamespaceID),
			Type:        "yaml",
			Content:     content,
		})
		if err != nil {
			log.Error = err.Error()
			result.Logs = append(result.Logs, log)
			return result, err
		}
		log.Success = true
		result.Logs = append(result.Logs, log)
	}
	return result, nil
}

func namespaceOrFallback(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func changePaths(changes []configdiff.Change) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return paths
}

func targetExtraOnly(changes []configdiff.Change) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		switch change.Kind {
		case configdiff.ChangeDelete:
			continue
		case configdiff.ChangeModify:
			if !targetListSuperset(change.SourceValue, change.TargetValue) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func targetListSuperset(source, target any) bool {
	sourceValues, sourceOK := source.([]any)
	targetValues, targetOK := target.([]any)
	if !sourceOK || !targetOK {
		return false
	}
	sourceCounts := valueCounts(sourceValues)
	targetCounts := valueCounts(targetValues)
	hasExtra := false
	for key, sourceCount := range sourceCounts {
		if targetCounts[key] < sourceCount {
			return false
		}
		if targetCounts[key] > sourceCount {
			hasExtra = true
		}
	}
	for key, targetCount := range targetCounts {
		if sourceCounts[key] == 0 && targetCount > 0 {
			hasExtra = true
		}
	}
	return hasExtra
}

func valueCounts(values []any) map[string]int {
	counts := map[string]int{}
	for _, value := range values {
		counts[valueKey(value)]++
	}
	return counts
}

func valueKey(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
