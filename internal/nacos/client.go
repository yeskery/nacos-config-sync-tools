package nacos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	namespace  string
	apiVersion APIVersion
	httpClient *http.Client
	token      string
}

func NewClient(options ClientOptions) *Client {
	return &Client{
		baseURL:    strings.TrimRight(options.BaseURL, "/"),
		username:   options.Username,
		password:   options.Password,
		namespace:  options.Namespace,
		apiVersion: normalizeAPIVersion(options.APIVersion),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) SetAccessTokenForTest(token string) {
	c.token = token
}

func (c *Client) Login(ctx context.Context) error {
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)
	var firstErr error
	for _, endpoint := range c.v1APIEndpoints("/v1/auth/users/login") {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusError("nacos login failed", resp)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		var body struct {
			AccessToken string `json:"accessToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		if body.AccessToken == "" {
			return errors.New("nacos login response did not include accessToken")
		}
		c.token = body.AccessToken
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("nacos login failed")
}

func (c *Client) ListConfigs(ctx context.Context, req ListRequest) ([]ConfigMeta, error) {
	switch c.apiVersion {
	case APIVersionV1, APIVersionV2:
		return c.listConfigsWithAccurateFallback(ctx, req)
	case APIVersionV3:
		return c.listConfigsV3(ctx, req)
	}

	items, err := c.listConfigs(ctx, req, "blur")
	if err == nil {
		return items, nil
	}
	if isDeprecatedAPIError(err) {
		return c.listConfigsV3(ctx, req)
	}
	fallbackItems, fallbackErr := c.listConfigs(ctx, req, "accurate")
	if fallbackErr == nil {
		return fallbackItems, nil
	}
	if isDeprecatedAPIError(fallbackErr) {
		v3Items, v3Err := c.listConfigsV3(ctx, req)
		if v3Err == nil {
			return v3Items, nil
		}
		return nil, fmt.Errorf("%w; fallback accurate search also failed: %v; v3 fallback also failed: %v", err, fallbackErr, v3Err)
	}
	return nil, fmt.Errorf("%w; fallback accurate search also failed: %v", err, fallbackErr)
}

func (c *Client) listConfigsWithAccurateFallback(ctx context.Context, req ListRequest) ([]ConfigMeta, error) {
	items, err := c.listConfigs(ctx, req, "blur")
	if err == nil {
		return items, nil
	}
	fallbackItems, fallbackErr := c.listConfigs(ctx, req, "accurate")
	if fallbackErr == nil {
		return fallbackItems, nil
	}
	return nil, fmt.Errorf("%w; fallback accurate search also failed: %v", err, fallbackErr)
}

func (c *Client) listConfigs(ctx context.Context, req ListRequest, search string) ([]ConfigMeta, error) {
	if req.PageNo == 0 {
		req.PageNo = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 500
	}
	values := c.authQuery()
	values.Set("search", search)
	values.Set("dataId", "")
	values.Set("group", req.Group)
	values.Set("appName", "")
	values.Set("config_tags", "")
	values.Set("pageNo", strconv.Itoa(req.PageNo))
	values.Set("pageSize", strconv.Itoa(req.PageSize))
	values.Set("types", "")
	if c.username != "" {
		values.Set("username", c.username)
	}
	if tenant := c.tenant(req.NamespaceID); tenant != "" {
		values.Set("tenant", tenant)
	}

	var firstErr error
	var deprecatedErr error
	for _, endpoint := range c.v1APIEndpoints("/v1/cs/configs") {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
		if err != nil {
			return nil, err
		}
		c.setAccessTokenHeader(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusError("list nacos configs failed", resp)
			if isDeprecatedAPIError(err) && deprecatedErr == nil {
				deprecatedErr = err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		var page struct {
			PageItems []ConfigMeta `json:"pageItems"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return page.PageItems, nil
	}
	if deprecatedErr != nil {
		return nil, deprecatedErr
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, errors.New("list nacos configs failed")
}

func (c *Client) listConfigsV3(ctx context.Context, req ListRequest) ([]ConfigMeta, error) {
	var firstErr error
	for _, endpoint := range c.v3ConsoleEndpoints("/v3/console/cs/config/list") {
		items, err := c.listConfigsV3At(ctx, req, endpoint)
		if err == nil {
			return items, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func (c *Client) listConfigsV3At(ctx context.Context, req ListRequest, endpoint string) ([]ConfigMeta, error) {
	if req.PageNo == 0 {
		req.PageNo = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 500
	}
	values := url.Values{}
	values.Set("dataId", "")
	values.Set("groupName", req.Group)
	values.Set("appName", "")
	values.Set("configTags", "")
	values.Set("pageNo", strconv.Itoa(req.PageNo))
	values.Set("pageSize", strconv.Itoa(req.PageSize))
	values.Set("type", "")
	values.Set("search", "blur")
	if c.username != "" {
		values.Set("username", c.username)
	}
	if tenant := c.tenant(req.NamespaceID); tenant != "" {
		values.Set("namespaceId", tenant)
	} else {
		values.Set("namespaceId", "public")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setAccessTokenHeader(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError("list nacos configs v3 failed", resp)
	}

	var response struct {
		Code int `json:"code"`
		Data struct {
			PageItems []struct {
				DataID    string `json:"dataId"`
				Group     string `json:"group"`
				GroupName string `json:"groupName"`
				Type      string `json:"type"`
			} `json:"pageItems"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	if response.Code != 0 && response.Code != 200 {
		return nil, fmt.Errorf("list nacos configs v3 failed: code %d: %s", response.Code, response.Message)
	}
	items := make([]ConfigMeta, 0, len(response.Data.PageItems))
	for _, item := range response.Data.PageItems {
		group := item.Group
		if group == "" {
			group = item.GroupName
		}
		items = append(items, ConfigMeta{DataID: item.DataID, Group: group, Type: item.Type})
	}
	return items, nil
}

func (c *Client) GetConfig(ctx context.Context, req ConfigRequest) (string, error) {
	switch c.apiVersion {
	case APIVersionV1, APIVersionV2:
		return c.getConfigLegacy(ctx, req)
	case APIVersionV3:
		return c.getConfigV3(ctx, req)
	}

	content, err := c.getConfigLegacy(ctx, req)
	if err == nil {
		return content, nil
	}
	if isDeprecatedAPIError(err) {
		return c.getConfigV3(ctx, req)
	}
	return "", err
}

func (c *Client) getConfigLegacy(ctx context.Context, req ConfigRequest) (string, error) {
	values := c.authQuery()
	values.Set("show", "all")
	values.Set("dataId", req.DataID)
	values.Set("group", req.Group)
	values.Set("tenant", c.tenant(req.NamespaceID))
	values.Set("namespaceId", "")
	var firstErr error
	var deprecatedErr error
	for _, endpoint := range c.v1APIEndpoints("/v1/cs/configs") {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
		if err != nil {
			return "", err
		}
		c.setAccessTokenHeader(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusError("get nacos config "+req.DataID+" failed", resp)
			if isDeprecatedAPIError(err) && deprecatedErr == nil {
				deprecatedErr = err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return "", err
		}
		return configContentFromResponse(data)
	}
	if deprecatedErr != nil {
		return "", deprecatedErr
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", nil
}

func (c *Client) getConfigV3(ctx context.Context, req ConfigRequest) (string, error) {
	values := url.Values{}
	values.Set("dataId", req.DataID)
	values.Set("groupName", req.Group)
	tenant := c.tenant(req.NamespaceID)
	if tenant == "" {
		tenant = "public"
	}
	values.Set("tenant", tenant)
	values.Set("namespaceId", "")

	var firstErr error
	for _, endpoint := range c.v3ConsoleEndpoints("/v3/console/cs/config") {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
		if err != nil {
			return "", err
		}
		c.setAccessTokenHeader(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusError("get nacos config v3 "+req.DataID+" failed", resp)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return "", err
		}
		return configContentFromResponse(data)
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", nil
}

func configContentFromResponse(data []byte) (string, error) {
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "{") {
		return string(data), nil
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Content string `json:"content"`
		Data    struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return string(data), nil
	}
	if response.Code != 0 && response.Code != 200 {
		return "", fmt.Errorf("get nacos config failed: code %d: %s", response.Code, response.Message)
	}
	if response.Data.Content != "" {
		return response.Data.Content, nil
	}
	if response.Content != "" {
		return response.Content, nil
	}
	return string(data), nil
}

func (c *Client) PublishConfig(ctx context.Context, req PublishRequest) error {
	switch c.apiVersion {
	case APIVersionV1, APIVersionV2:
		return c.publishConfigLegacy(ctx, req)
	case APIVersionV3:
		return c.publishConfigV3(ctx, req)
	}

	err := c.publishConfigLegacy(ctx, req)
	if err == nil {
		return nil
	}
	if isDeprecatedAPIError(err) {
		return c.publishConfigV3(ctx, req)
	}
	return err
}

func (c *Client) publishConfigLegacy(ctx context.Context, req PublishRequest) error {
	form := c.authQuery()
	form.Set("dataId", req.DataID)
	form.Set("group", req.Group)
	form.Set("content", req.Content)
	if req.Type != "" {
		form.Set("type", req.Type)
	}
	if tenant := c.tenant(req.NamespaceID); tenant != "" {
		form.Set("tenant", tenant)
	}
	query := c.publishQuery()
	var firstErr error
	for _, endpoint := range c.v1APIEndpoints("/v1/cs/configs") {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, withQuery(endpoint, query), bytes.NewBufferString(form.Encode()))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.setAccessTokenHeader(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusErrorWithBody("publish nacos config "+req.DataID+" failed", resp.StatusCode, body)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !publishResponseOK(body) {
			return fmt.Errorf("publish nacos config %s failed: %s", req.DataID, strings.TrimSpace(string(body)))
		}
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("publish nacos config failed")
}

func (c *Client) publishConfigV3(ctx context.Context, req PublishRequest) error {
	form := url.Values{}
	form.Set("dataId", req.DataID)
	form.Set("groupName", req.Group)
	form.Set("content", req.Content)
	if req.Type != "" {
		form.Set("type", req.Type)
	}
	tenant := c.tenant(req.NamespaceID)
	if tenant == "" {
		tenant = "public"
	}
	form.Set("tenant", tenant)
	query := c.publishQuery()
	var firstErr error
	for _, endpoint := range c.v3ConsoleEndpoints("/v3/console/cs/config") {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, withQuery(endpoint, query), bytes.NewBufferString(form.Encode()))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.setAccessTokenHeader(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := statusErrorWithBody("publish nacos config "+req.DataID+" failed", resp.StatusCode, body)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !publishResponseOK(body) {
			return fmt.Errorf("publish nacos config %s failed: %s", req.DataID, strings.TrimSpace(string(body)))
		}
		return nil
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("publish nacos config failed")
}

func publishResponseOK(body []byte) bool {
	text := strings.TrimSpace(string(body))
	if text == "true" {
		return true
	}
	if !strings.HasPrefix(text, "{") {
		return false
	}
	var response struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false
	}
	if response.Code != 0 && response.Code != 200 {
		return false
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return true
	}
	var ok bool
	if err := json.Unmarshal(response.Data, &ok); err == nil {
		return ok
	}
	return true
}

func statusError(prefix string, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return statusErrorWithBody(prefix, resp.StatusCode, body)
}

func statusErrorWithBody(prefix string, statusCode int, body []byte) error {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("%s: status %d", prefix, statusCode)
	}
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	return fmt.Errorf("%s: status %d: %s", prefix, statusCode, text)
}

func isDeprecatedAPIError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "Current API will be deprecated") ||
		strings.Contains(text, "/v3/console/cs/config/list")
}

func (c *Client) endpoint(path string) string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/nacos") {
		return base + path
	}
	return base + "/nacos" + path
}

func (c *Client) v1APIEndpoints(path string) []string {
	root := c.rootEndpoint(path)
	withContext := c.endpoint(path)
	if strings.HasSuffix(strings.TrimRight(c.baseURL, "/"), "/nacos") {
		if withContext == root {
			return []string{withContext}
		}
		return []string{withContext, root}
	}
	if withContext == root {
		return []string{root}
	}
	return []string{root, withContext}
}

func (c *Client) rootEndpoint(path string) string {
	base := strings.TrimRight(c.baseURL, "/")
	base = strings.TrimSuffix(base, "/nacos")
	return base + path
}

func (c *Client) v3ConsoleEndpoints(path string) []string {
	root := c.rootEndpoint(path)
	withContext := c.endpoint(path)
	if strings.HasSuffix(strings.TrimRight(c.baseURL, "/"), "/nacos") {
		if withContext == root {
			return []string{withContext}
		}
		return []string{withContext, root}
	}
	if withContext == root {
		return []string{root}
	}
	return []string{root, withContext}
}

func (c *Client) setAccessTokenHeader(req *http.Request) {
	if c.token != "" {
		req.Header.Set("accessToken", c.token)
	}
}

func (c *Client) authQuery() url.Values {
	values := url.Values{}
	if c.token != "" {
		values.Set("accessToken", c.token)
	}
	return values
}

func (c *Client) publishQuery() url.Values {
	values := url.Values{}
	if c.username != "" {
		values.Set("username", c.username)
	}
	return values
}

func withQuery(endpoint string, query url.Values) string {
	encoded := query.Encode()
	if encoded == "" {
		return endpoint
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + encoded
}

func (c *Client) tenant(override string) string {
	if override != "" {
		return override
	}
	return c.namespace
}

func normalizeAPIVersion(version APIVersion) APIVersion {
	switch strings.ToLower(string(version)) {
	case "v1":
		return APIVersionV1
	case "v2":
		return APIVersionV2
	case "v3":
		return APIVersionV3
	default:
		return APIVersionAuto
	}
}
