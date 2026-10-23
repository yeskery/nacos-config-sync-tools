package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type EndpointSettings struct {
	Name        string `json:"name"`
	BaseURL     string `json:"baseUrl"`
	Username    string `json:"username"`
	NamespaceID string `json:"namespaceId,omitempty"`
	APIVersion  string `json:"apiVersion,omitempty"`
	PasswordRef string `json:"passwordRef,omitempty"`
}

type Profile struct {
	Name      string           `json:"name"`
	Source    EndpointSettings `json:"source"`
	Target    EndpointSettings `json:"target"`
	SourceEnv string           `json:"sourceEnv"`
	TargetEnv string           `json:"targetEnv"`
	Group     string           `json:"group"`
}

type IgnoreRule struct {
	DataID string `json:"dataId"`
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

type SyncLog struct {
	SourceDataID string   `json:"sourceDataId"`
	TargetDataID string   `json:"targetDataId"`
	ChangedPaths []string `json:"changedPaths"`
	Operator     string   `json:"operator,omitempty"`
	Time         string   `json:"time"`
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
}

type AppConfig struct {
	Profiles    []Profile    `json:"profiles"`
	IgnoreRules []IgnoreRule `json:"ignoreRules"`
	RecentLogs  []SyncLog    `json:"recentLogs,omitempty"`
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load() (AppConfig, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return AppConfig{}, nil
	}
	if err != nil {
		return AppConfig{}, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, err
	}
	return cfg, nil
}

func (s *FileStore) Save(cfg AppConfig) error {
	if err := filepath.WalkDir(filepath.Dir(s.path), func(path string, d os.DirEntry, err error) error {
		return err
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
