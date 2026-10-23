package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"nacos-config-sync-tools/internal/credentials"
	configdiff "nacos-config-sync-tools/internal/diff"
	"nacos-config-sync-tools/internal/nacos"
	"nacos-config-sync-tools/internal/rules"
	"nacos-config-sync-tools/internal/store"
	configsync "nacos-config-sync-tools/internal/sync"
)

type App struct {
	ctx         context.Context
	plan        configsync.Plan
	store       *store.FileStore
	credentials credentials.Store
}

type EndpointInput struct {
	Name        string `json:"name"`
	BaseURL     string `json:"baseUrl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PasswordRef string `json:"passwordRef"`
	NamespaceID string `json:"namespaceId"`
	APIVersion  string `json:"apiVersion"`
}

type ScanInput struct {
	Source    EndpointInput `json:"source"`
	Target    EndpointInput `json:"target"`
	SourceEnv string        `json:"sourceEnv"`
	TargetEnv string        `json:"targetEnv"`
	Group     string        `json:"group"`
}

type SavedEndpoint struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	BaseURL     string `json:"baseUrl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PasswordRef string `json:"passwordRef"`
	NamespaceID string `json:"namespaceId"`
	APIVersion  string `json:"apiVersion"`
}

func NewApp() *App {
	return &App{
		store:       store.NewFileStore(defaultSettingsPath()),
		credentials: credentials.NewStore(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) LoadSettings() (store.AppConfig, error) {
	return a.store.Load()
}

func (a *App) SaveSettings(cfg store.AppConfig) error {
	return a.store.Save(cfg)
}

func (a *App) SavedEndpoints() ([]SavedEndpoint, error) {
	cfg, err := a.store.Load()
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	endpoints := []SavedEndpoint{}
	add := func(settings store.EndpointSettings) error {
		if strings.TrimSpace(settings.BaseURL) == "" {
			return nil
		}
		endpoint, err := a.savedEndpoint(settings)
		if err != nil {
			return err
		}
		key := endpointKey(endpoint)
		if index, ok := seen[key]; ok {
			if endpoints[index].Password == "" && endpoint.Password != "" {
				endpoints[index].Password = endpoint.Password
				endpoints[index].PasswordRef = endpoint.PasswordRef
			}
			return nil
		}
		seen[key] = len(endpoints)
		endpoints = append(endpoints, endpoint)
		return nil
	}
	for _, profile := range cfg.Profiles {
		if err := add(profile.Source); err != nil {
			return nil, err
		}
		if err := add(profile.Target); err != nil {
			return nil, err
		}
	}
	return endpoints, nil
}

func (a *App) SaveProfile(input ScanInput) (store.AppConfig, error) {
	if input.Group == "" {
		input.Group = "DEFAULT_GROUP"
	}
	source, err := a.endpointSettings("source", input.Source)
	if err != nil {
		return store.AppConfig{}, err
	}
	target, err := a.endpointSettings("target", input.Target)
	if err != nil {
		return store.AppConfig{}, err
	}
	cfg, err := a.store.Load()
	if err != nil {
		return store.AppConfig{}, err
	}
	profile := store.Profile{
		Name:      input.SourceEnv + "-to-" + input.TargetEnv,
		Source:    source,
		Target:    target,
		SourceEnv: input.SourceEnv,
		TargetEnv: input.TargetEnv,
		Group:     input.Group,
	}
	replaced := false
	for i, existing := range cfg.Profiles {
		if existing.Name == profile.Name {
			cfg.Profiles[i] = profile
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Profiles = append(cfg.Profiles, profile)
	}
	if err := a.store.Save(cfg); err != nil {
		return store.AppConfig{}, err
	}
	return cfg, nil
}

func (a *App) savedEndpoint(settings store.EndpointSettings) (SavedEndpoint, error) {
	password := ""
	if settings.PasswordRef != "" {
		loaded, err := a.credentials.Load(settings.PasswordRef)
		if err != nil {
			return SavedEndpoint{}, err
		}
		password = loaded
	}
	apiVersion := settings.APIVersion
	if apiVersion == "" {
		apiVersion = "v3"
	}
	endpoint := SavedEndpoint{
		Name:        settings.Name,
		BaseURL:     settings.BaseURL,
		Username:    settings.Username,
		Password:    password,
		PasswordRef: settings.PasswordRef,
		NamespaceID: settings.NamespaceID,
		APIVersion:  apiVersion,
	}
	endpoint.ID = endpointKey(endpoint)
	endpoint.Label = endpointLabel(endpoint)
	return endpoint, nil
}

func endpointKey(endpoint SavedEndpoint) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(endpoint.BaseURL)),
		strings.TrimSpace(endpoint.Username),
		strings.TrimSpace(endpoint.NamespaceID),
		strings.TrimSpace(endpoint.APIVersion),
	}, "|")
}

func endpointLabel(endpoint SavedEndpoint) string {
	parts := []string{}
	parts = append(parts, endpoint.BaseURL)
	if endpoint.NamespaceID != "" {
		parts = append(parts, "ns:"+endpoint.NamespaceID)
	}
	if endpoint.APIVersion != "" {
		parts = append(parts, endpoint.APIVersion)
	}
	return strings.Join(parts, " · ")
}

func (a *App) AddIgnoreRule(rule store.IgnoreRule) (store.AppConfig, error) {
	if rule.DataID == "" || rule.Path == "" {
		return store.AppConfig{}, errors.New("dataId and path are required")
	}
	cfg, err := a.store.Load()
	if err != nil {
		return store.AppConfig{}, err
	}
	for _, existing := range cfg.IgnoreRules {
		if existing.DataID == rule.DataID && existing.Path == rule.Path {
			return cfg, nil
		}
	}
	cfg.IgnoreRules = append(cfg.IgnoreRules, rule)
	if err := a.store.Save(cfg); err != nil {
		return store.AppConfig{}, err
	}
	return cfg, nil
}

func (a *App) RemoveIgnoreRule(rule store.IgnoreRule) (store.AppConfig, error) {
	if rule.DataID == "" || rule.Path == "" {
		return store.AppConfig{}, errors.New("dataId and path are required")
	}
	cfg, err := a.store.Load()
	if err != nil {
		return store.AppConfig{}, err
	}
	next := cfg.IgnoreRules[:0]
	for _, existing := range cfg.IgnoreRules {
		if existing.DataID == rule.DataID && existing.Path == rule.Path {
			continue
		}
		next = append(next, existing)
	}
	cfg.IgnoreRules = next
	if err := a.store.Save(cfg); err != nil {
		return store.AppConfig{}, err
	}
	return cfg, nil
}

func (a *App) Scan(input ScanInput) (configsync.Plan, error) {
	if input.Group == "" {
		input.Group = "DEFAULT_GROUP"
	}
	sourcePassword, err := a.resolvePassword(input.Source)
	if err != nil {
		return configsync.Plan{}, err
	}
	targetPassword, err := a.resolvePassword(input.Target)
	if err != nil {
		return configsync.Plan{}, err
	}
	source := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    input.Source.BaseURL,
		Username:   input.Source.Username,
		Password:   sourcePassword,
		Namespace:  input.Source.NamespaceID,
		APIVersion: nacos.APIVersion(input.Source.APIVersion),
	})
	target := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    input.Target.BaseURL,
		Username:   input.Target.Username,
		Password:   targetPassword,
		Namespace:  input.Target.NamespaceID,
		APIVersion: nacos.APIVersion(input.Target.APIVersion),
	})
	if err := source.Login(a.ctx); err != nil {
		return configsync.Plan{}, err
	}
	if err := target.Login(a.ctx); err != nil {
		return configsync.Plan{}, err
	}

	cfg, err := a.store.Load()
	if err != nil {
		return configsync.Plan{}, err
	}
	ruleSet := rules.RuleSet{}
	for _, rule := range cfg.IgnoreRules {
		ruleSet.Rules = append(ruleSet.Rules, rules.IgnoreRule(rule))
	}
	plan, err := configsync.NewPlanner(source, target, &ruleSet).Build(a.ctx, configsync.BuildRequest{
		Group:             input.Group,
		SourceNamespaceID: input.Source.NamespaceID,
		TargetNamespaceID: input.Target.NamespaceID,
		SourceEnv:         input.SourceEnv,
		TargetEnv:         input.TargetEnv,
	})
	if err != nil {
		return configsync.Plan{}, err
	}
	a.plan = plan
	return plan, nil
}

func (a *App) ApplyChange(targetDataID, path, currentContent string) (string, error) {
	item, err := a.planItemByTarget(targetDataID)
	if err != nil {
		return "", err
	}
	return configdiff.ApplySourcePath(item.SourceContent, currentContent, path)
}

func (a *App) KeepTargetChange(targetDataID, path, currentContent string) (string, error) {
	item, err := a.planItemByTarget(targetDataID)
	if err != nil {
		return "", err
	}
	return configdiff.ApplySourcePath(item.TargetContent, currentContent, path)
}

func (a *App) ApplyListItemChange(targetDataID, path, itemKey, action, currentContent string) (string, error) {
	item, err := a.planItemByTarget(targetDataID)
	if err != nil {
		return "", err
	}
	return configdiff.ApplyListItemChange(item.SourceContent, currentContent, path, itemKey, action)
}

func (a *App) ApplyAllChanges(targetDataID, currentContent string) (string, error) {
	item, err := a.planItemByTarget(targetDataID)
	if err != nil {
		return "", err
	}
	content := currentContent
	for _, change := range item.Changes {
		content, err = configdiff.ApplySourcePath(item.SourceContent, content, change.Path)
		if err != nil {
			return "", err
		}
	}
	return content, nil
}

func (a *App) Publish(input ScanInput, selectedTargetDataIDs []string, editedContents map[string]string) (configsync.PublishResult, error) {
	if len(a.plan.Items) == 0 {
		return configsync.PublishResult{}, errors.New("scan before publishing")
	}
	targetPassword, err := a.resolvePassword(input.Target)
	if err != nil {
		return configsync.PublishResult{}, err
	}
	target := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    input.Target.BaseURL,
		Username:   input.Target.Username,
		Password:   targetPassword,
		Namespace:  input.Target.NamespaceID,
		APIVersion: nacos.APIVersion(input.Target.APIVersion),
	})
	if err := target.Login(a.ctx); err != nil {
		return configsync.PublishResult{}, err
	}
	result, err := configsync.NewExecutor(target).Publish(a.ctx, a.plan, selectedTargetDataIDs, editedContents)
	if err != nil {
		return result, err
	}
	if len(result.Logs) > 0 {
		cfg, loadErr := a.store.Load()
		if loadErr != nil {
			return result, loadErr
		}
		for _, log := range result.Logs {
			if !log.Success {
				continue
			}
			cfg.RecentLogs = append(cfg.RecentLogs, store.SyncLog{
				SourceDataID: log.SourceDataID,
				TargetDataID: log.TargetDataID,
				ChangedPaths: log.ChangedPaths,
				Time:         log.Time.Format("2006-01-02T15:04:05Z07:00"),
				Success:      log.Success,
			})
		}
		if saveErr := a.store.Save(cfg); saveErr != nil {
			return result, saveErr
		}
	}
	return result, nil
}

func (a *App) planItemByTarget(targetDataID string) (configsync.PlanItem, error) {
	if len(a.plan.Items) == 0 {
		return configsync.PlanItem{}, errors.New("scan before applying changes")
	}
	for _, item := range a.plan.Items {
		if item.TargetDataID == targetDataID {
			return item, nil
		}
	}
	return configsync.PlanItem{}, errors.New("config item not found in last scan")
}

func (a *App) endpointSettings(role string, endpoint EndpointInput) (store.EndpointSettings, error) {
	ref := endpoint.PasswordRef
	if endpoint.Password != "" {
		var err error
		ref, err = a.credentials.Save(credentialTarget(role, endpoint), endpoint.Username, endpoint.Password)
		if err != nil {
			return store.EndpointSettings{}, err
		}
	}
	return store.EndpointSettings{
		Name:        endpoint.Name,
		BaseURL:     endpoint.BaseURL,
		Username:    endpoint.Username,
		NamespaceID: endpoint.NamespaceID,
		APIVersion:  endpoint.APIVersion,
		PasswordRef: ref,
	}, nil
}

func (a *App) resolvePassword(endpoint EndpointInput) (string, error) {
	if endpoint.Password != "" {
		return endpoint.Password, nil
	}
	if endpoint.PasswordRef == "" {
		return "", nil
	}
	return a.credentials.Load(endpoint.PasswordRef)
}

func credentialTarget(role string, endpoint EndpointInput) string {
	return "nacos-config-sync-tools/" + role + "/" + endpoint.BaseURL + "/" + endpoint.NamespaceID
}

func defaultSettingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "nacos-config-sync-tools", "settings.json")
}
