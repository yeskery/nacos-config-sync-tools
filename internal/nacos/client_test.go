package nacos_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nacos-config-sync-tools/internal/nacos"
)

func TestClientLoginListGetAndPublish(t *testing.T) {
	var publishedForm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/nacos/v1/auth/users/login":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse login form: %v", err)
			}
			if r.Form.Get("username") != "nacos" || r.Form.Get("password") != "secret" {
				t.Fatalf("login form = %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "token-123"})
		case r.URL.Path == "/nacos/v1/cs/configs" && r.Method == http.MethodGet && r.URL.Query().Get("search") == "blur":
			if r.URL.Query().Get("accessToken") != "token-123" {
				t.Fatalf("missing token on list request: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pageItems": []map[string]any{
					{"dataId": "dxtx-user-server.yml", "group": "DEFAULT_GROUP", "type": "yaml"},
				},
			})
		case r.URL.Path == "/nacos/v1/cs/configs" && r.Method == http.MethodGet:
			if r.URL.Query().Get("dataId") != "dxtx-user-server.yml" {
				t.Fatalf("get config query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte("feature:\n  enabled: true\n"))
		case r.URL.Path == "/nacos/v1/cs/configs" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse publish form: %v", err)
			}
			publishedForm = r.Form.Encode()
			_, _ = w.Write([]byte("true"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:   server.URL + "/nacos",
		Username:  "nacos",
		Password:  "secret",
		Namespace: "dev-ns",
	})

	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server.yml" {
		t.Fatalf("items = %+v", items)
	}
	content, err := client.GetConfig(context.Background(), nacos.ConfigRequest{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("GetConfig returned error: %v", err)
	}
	if !strings.Contains(content, "enabled: true") {
		t.Fatalf("content = %q", content)
	}
	if err := client.PublishConfig(context.Background(), nacos.PublishRequest{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP", Content: "a: 1\n", Type: "yaml"}); err != nil {
		t.Fatalf("PublishConfig returned error: %v", err)
	}
	if !strings.Contains(publishedForm, "dataId=dxtx-user-server.yml") || !strings.Contains(publishedForm, "content=a%3A+1%0A") {
		t.Fatalf("publish form = %q", publishedForm)
	}
}

func TestClientIncludesResponseBodyInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":500,"message":"invalid username or password"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:  server.URL,
		Username: "nacos",
		Password: "bad",
	})

	err := client.Login(context.Background())
	if err == nil {
		t.Fatal("expected login error")
	}
	if !strings.Contains(err.Error(), "invalid username or password") {
		t.Fatalf("error = %q, want response body included", err.Error())
	}
}

func TestClientFallsBackToV3ConsoleConfigList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/nacos/v1/cs/configs" || r.URL.Path == "/v1/cs/configs":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"message":"Current API will be deprecated, please use API(s) GET ${contextPath:nacos}/v3/console/cs/config/list instead"}`))
		case r.URL.Path == "/nacos/v3/console/cs/config/list":
			if r.Header.Get("accessToken") != "token-123" {
				t.Fatalf("missing access token header on v3 fallback: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("groupName") != "DEFAULT_GROUP" {
				t.Fatalf("v3 groupName = %q", r.URL.Query().Get("groupName"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"pageItems": []map[string]any{
						{"dataId": "dxtx-user-server.yml", "groupName": "DEFAULT_GROUP", "type": "yaml"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{BaseURL: server.URL + "/nacos"})
	client.SetAccessTokenForTest("token-123")

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server.yml" || items[0].Group != "DEFAULT_GROUP" {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientFallsBackToV3ConsoleConfigListWithoutNacosContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/nacos/v1/cs/configs" || r.URL.Path == "/v1/cs/configs":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"message":"Current API will be deprecated, please use API(s) GET ${contextPath:nacos}/v3/console/cs/config/list instead"}`))
		case r.URL.Path == "/nacos/v3/console/cs/config/list":
			http.Error(w, `{"message":"No endpoint GET /nacos/v3/console/cs/config/list."}`, http.StatusNotFound)
		case r.URL.Path == "/v3/console/cs/config/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"pageItems": []map[string]any{
						{"dataId": "dxtx-user-server-dev.yml", "groupName": "DEFAULT_GROUP", "type": "yaml"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{BaseURL: server.URL + "/nacos"})
	client.SetAccessTokenForTest("token-123")

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server-dev.yml" {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientUsesV3ConsoleListShapeFromBrowserCurl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/nacos/v1/cs/configs" || r.URL.Path == "/v1/cs/configs":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"message":"Current API will be deprecated, please use API(s) GET ${contextPath:nacos}/v3/console/cs/config/list instead"}`))
		case r.URL.Path == "/v3/console/cs/config/list":
			if r.Header.Get("accessToken") != "token-123" {
				t.Fatalf("accessToken header = %q, want token-123", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("namespaceId") != "public" {
				t.Fatalf("namespaceId = %q, want public", r.URL.Query().Get("namespaceId"))
			}
			if r.URL.Query().Get("groupName") != "DEFAULT_GROUP" {
				t.Fatalf("groupName = %q", r.URL.Query().Get("groupName"))
			}
			if r.URL.Query().Get("username") != "nacos" {
				t.Fatalf("username = %q, want nacos", r.URL.Query().Get("username"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"pageItems": []map[string]any{
						{"dataId": "dxtx-user-server-local.yml", "groupName": "DEFAULT_GROUP", "type": "yaml"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:   server.URL,
		Username:  "nacos",
		Namespace: "public",
	})
	client.SetAccessTokenForTest("token-123")

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server-local.yml" {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientSupportsNacos252RootV1ConfigListShapeFromBrowserCurl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/nacos/v1/cs/configs":
			http.Error(w, `{"message":"No endpoint GET /nacos/v1/cs/configs."}`, http.StatusNotFound)
		case r.URL.Path == "/v1/cs/configs":
			if r.Header.Get("accessToken") != "token-252" {
				t.Fatalf("accessToken header = %q, want token-252", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("config_tags") != "" {
				t.Fatalf("config_tags = %q, want empty", r.URL.Query().Get("config_tags"))
			}
			if r.URL.Query().Get("types") != "" {
				t.Fatalf("types = %q, want empty", r.URL.Query().Get("types"))
			}
			if r.URL.Query().Get("username") != "nacos" {
				t.Fatalf("username = %q, want nacos", r.URL.Query().Get("username"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pageItems": []map[string]any{
					{"dataId": "dxtx-user-server-test.yml", "group": "DEFAULT_GROUP", "type": "yaml"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:  server.URL,
		Username: "nacos",
	})
	client.SetAccessTokenForTest("token-252")

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server-test.yml" {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientAPIVersionV3UsesConsoleListDirectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cs/configs", "/nacos/v1/cs/configs":
			t.Fatalf("V3 mode should not call legacy v1 list endpoint: %s", r.URL.String())
		case "/v3/console/cs/config/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"pageItems": []map[string]any{
						{"dataId": "dxtx-user-server-dev.yml", "groupName": "DEFAULT_GROUP", "type": "yaml"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{BaseURL: server.URL, APIVersion: nacos.APIVersionV3})
	client.SetAccessTokenForTest("token-v3")

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server-dev.yml" {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientAPIVersionV3GetsConsoleConfigDirectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cs/configs", "/nacos/v1/cs/configs":
			t.Fatalf("V3 mode should not call legacy v1 get endpoint: %s", r.URL.String())
		case "/v3/console/cs/config":
			if r.Header.Get("accessToken") != "token-v3" {
				t.Fatalf("accessToken header = %q, want token-v3", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("dataId") != "dxtx-user-server.yml" {
				t.Fatalf("dataId = %q, want dxtx-user-server.yml", r.URL.Query().Get("dataId"))
			}
			if r.URL.Query().Get("groupName") != "DEFAULT_GROUP" {
				t.Fatalf("groupName = %q, want DEFAULT_GROUP", r.URL.Query().Get("groupName"))
			}
			if r.URL.Query().Get("tenant") != "public" {
				t.Fatalf("tenant = %q, want public", r.URL.Query().Get("tenant"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"content": "feature:\n  enabled: true\n",
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    server.URL,
		Namespace:  "public",
		APIVersion: nacos.APIVersionV3,
	})
	client.SetAccessTokenForTest("token-v3")

	content, err := client.GetConfig(context.Background(), nacos.ConfigRequest{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("GetConfig returned error: %v", err)
	}
	if !strings.Contains(content, "enabled: true") {
		t.Fatalf("content = %q", content)
	}
}

func TestClientAPIVersionV3PublishesConsoleConfigDirectly(t *testing.T) {
	var publishedForm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cs/configs", "/nacos/v1/cs/configs":
			t.Fatalf("V3 mode should not call legacy v1 publish endpoint: %s", r.URL.String())
		case "/v3/console/cs/config":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.Header.Get("accessToken") != "token-v3" {
				t.Fatalf("accessToken header = %q, want token-v3", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("username") != "nacos" {
				t.Fatalf("username query = %q, want nacos", r.URL.Query().Get("username"))
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse publish form: %v", err)
			}
			if r.Form.Get("groupName") != "DEFAULT_GROUP" {
				t.Fatalf("groupName = %q, want DEFAULT_GROUP", r.Form.Get("groupName"))
			}
			if r.Form.Get("tenant") != "public" {
				t.Fatalf("tenant = %q, want public", r.Form.Get("tenant"))
			}
			if r.Form.Get("dataId") != "dxtx-user-server.yml" {
				t.Fatalf("dataId = %q, want dxtx-user-server.yml", r.Form.Get("dataId"))
			}
			publishedForm = r.Form.Encode()
			_, _ = w.Write([]byte("true"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    server.URL,
		Username:   "nacos",
		Namespace:  "public",
		APIVersion: nacos.APIVersionV3,
	})
	client.SetAccessTokenForTest("token-v3")

	err := client.PublishConfig(context.Background(), nacos.PublishRequest{
		DataID:  "dxtx-user-server.yml",
		Group:   "DEFAULT_GROUP",
		Content: "feature:\n  enabled: true\n",
		Type:    "yaml",
	})
	if err != nil {
		t.Fatalf("PublishConfig returned error: %v", err)
	}
	if !strings.Contains(publishedForm, "content=feature%3A%0A++enabled%3A+true%0A") {
		t.Fatalf("publish form = %q", publishedForm)
	}
}

func TestClientAPIVersionV2PublishesLegacyConfigDirectly(t *testing.T) {
	var publishedForm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/console/cs/config", "/nacos/v3/console/cs/config":
			t.Fatalf("V2 mode should not call v3 publish endpoint: %s", r.URL.String())
		case "/v1/cs/configs":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.Header.Get("accessToken") != "token-v2" {
				t.Fatalf("accessToken header = %q, want token-v2", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("username") != "nacos" {
				t.Fatalf("username query = %q, want nacos", r.URL.Query().Get("username"))
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse publish form: %v", err)
			}
			if r.Form.Get("group") != "DEFAULT_GROUP" {
				t.Fatalf("group = %q, want DEFAULT_GROUP", r.Form.Get("group"))
			}
			if r.Form.Get("tenant") != "test-ns" {
				t.Fatalf("tenant = %q, want test-ns", r.Form.Get("tenant"))
			}
			if r.Form.Get("dataId") != "dxtx-user-server-test.yml" {
				t.Fatalf("dataId = %q, want dxtx-user-server-test.yml", r.Form.Get("dataId"))
			}
			publishedForm = r.Form.Encode()
			_, _ = w.Write([]byte("true"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    server.URL,
		Username:   "nacos",
		Namespace:  "test-ns",
		APIVersion: nacos.APIVersionV2,
	})
	client.SetAccessTokenForTest("token-v2")

	err := client.PublishConfig(context.Background(), nacos.PublishRequest{
		DataID:  "dxtx-user-server-test.yml",
		Group:   "DEFAULT_GROUP",
		Content: "feature:\n  enabled: true\n",
		Type:    "yaml",
	})
	if err != nil {
		t.Fatalf("PublishConfig returned error: %v", err)
	}
	if !strings.Contains(publishedForm, "content=feature%3A%0A++enabled%3A+true%0A") {
		t.Fatalf("publish form = %q", publishedForm)
	}
}

func TestClientAPIVersionV2GetsShowAllConfigContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/console/cs/config", "/nacos/v3/console/cs/config":
			t.Fatalf("V2 mode should not call v3 get endpoint: %s", r.URL.String())
		case "/v1/cs/configs":
			if r.Header.Get("accessToken") != "token-v2" {
				t.Fatalf("accessToken header = %q, want token-v2", r.Header.Get("accessToken"))
			}
			if r.URL.Query().Get("show") != "all" {
				t.Fatalf("show = %q, want all", r.URL.Query().Get("show"))
			}
			if r.URL.Query().Get("tenant") != "test-ns" {
				t.Fatalf("tenant = %q, want test-ns", r.URL.Query().Get("tenant"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dataId":  "dxtx-user-server.yml",
				"group":   "DEFAULT_GROUP",
				"content": "spring:\n  application:\n    name: dxtx-user\n",
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{
		BaseURL:    server.URL,
		Namespace:  "test-ns",
		APIVersion: nacos.APIVersionV2,
	})
	client.SetAccessTokenForTest("token-v2")

	content, err := client.GetConfig(context.Background(), nacos.ConfigRequest{DataID: "dxtx-user-server.yml", Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("GetConfig returned error: %v", err)
	}
	if !strings.Contains(content, "name: dxtx-user") {
		t.Fatalf("content = %q", content)
	}
}

func TestClientAPIVersionV2UsesLegacyConfigList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/console/cs/config/list", "/nacos/v3/console/cs/config/list":
			t.Fatalf("V2 mode should not call v3 list endpoint: %s", r.URL.String())
		case "/v1/cs/configs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pageItems": []map[string]any{
					{"dataId": "dxtx-user-server-test.yml", "group": "DEFAULT_GROUP", "type": "yaml"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := nacos.NewClient(nacos.ClientOptions{BaseURL: server.URL, APIVersion: nacos.APIVersionV2})

	items, err := client.ListConfigs(context.Background(), nacos.ListRequest{Group: "DEFAULT_GROUP"})
	if err != nil {
		t.Fatalf("ListConfigs returned error: %v", err)
	}
	if len(items) != 1 || items[0].DataID != "dxtx-user-server-test.yml" {
		t.Fatalf("items = %+v", items)
	}
}
