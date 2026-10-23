package nacos

import "context"

type ConfigMeta struct {
	DataID string `json:"dataId"`
	Group  string `json:"group"`
	Type   string `json:"type,omitempty"`
}

type APIVersion string

const (
	APIVersionAuto APIVersion = "auto"
	APIVersionV1   APIVersion = "v1"
	APIVersionV2   APIVersion = "v2"
	APIVersionV3   APIVersion = "v3"
)

type ClientOptions struct {
	BaseURL    string
	Username   string
	Password   string
	Namespace  string
	APIVersion APIVersion
}

type ListRequest struct {
	Group       string
	NamespaceID string
	PageNo      int
	PageSize    int
}

type ConfigRequest struct {
	DataID      string
	Group       string
	NamespaceID string
}

type PublishRequest struct {
	DataID      string
	Group       string
	Content     string
	Type        string
	NamespaceID string
}

type API interface {
	ListConfigs(ctx context.Context, req ListRequest) ([]ConfigMeta, error)
	GetConfig(ctx context.Context, req ConfigRequest) (string, error)
	PublishConfig(ctx context.Context, req PublishRequest) error
}
