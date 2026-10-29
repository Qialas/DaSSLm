package deploy

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
)

type Trace struct {
	RequestURL     string
	RequestMethod  string
	RequestHeaders string
	RequestBody    string
	ResponseBody   string
}

type Target struct {
	SiteName     string
	ProductType  string
	Region       string
	ResourceID   string
	ResourceName string
	ExtraConfig  string
}

type SiteItem struct {
	ID       any      `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Domains  []string `json:"domains"`
	Port     any      `json:"port"`
	PS       string   `json:"ps"`
	SSL      any      `json:"ssl"`
	Expire   string   `json:"expire"`
	AddTime  string   `json:"addTime"`
	Provider string   `json:"provider"`
}

type SystemTotal struct {
	MemTotal    int     `json:"memTotal"`
	MemFree     int     `json:"memFree"`
	MemRealUsed int     `json:"memRealUsed"`
	CPUNum      int     `json:"cpuNum"`
	CPURealUsed float64 `json:"cpuRealUsed"`
	Time        string  `json:"time"`
	System      string  `json:"system"`
	IsUser      int     `json:"isuser"`
	IsPort      bool    `json:"isport"`
	Version     string  `json:"version"`
}

func (info SystemTotal) Valid() bool {
	return info.MemTotal > 0 || info.CPUNum > 0 || strings.TrimSpace(info.Version) != "" || strings.TrimSpace(info.System) != ""
}

// 宝塔部署目标类型，存放在部署目标的 ProductType 中；为空表示普通网站。
const (
	BTPanelTargetSite   = ""
	BTPanelTargetDocker = "docker"
)

// NormalizeBTPanelTargetType 校验并规范宝塔部署目标类型。
func NormalizeBTPanelTargetType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "site":
		return BTPanelTargetSite, nil
	case BTPanelTargetDocker:
		return BTPanelTargetDocker, nil
	default:
		return "", fmt.Errorf("不支持的宝塔部署类型：%s", value)
	}
}

func IsBTPanelProvider(provider string) bool {
	return strings.EqualFold(provider, "btpanel") || strings.EqualFold(provider, "baota")
}

func GetSystemTotal(ctx context.Context, account model.DeployAccount, client *http.Client) (*SystemTotal, *Trace, error) {
	form := url.Values{}
	form.Set("action", "GetSystemTotal")
	var out SystemTotal
	trace, err := btPanelRequest(ctx, account, client, "/system", form, &out, "request_token")
	if err != nil {
		return nil, trace, err
	}
	return &out, trace, nil
}

func ListSites(ctx context.Context, account model.DeployAccount, client *http.Client, page, pageSize int, keyword, siteType string) ([]SiteItem, int, *Trace, error) {
	items, trace, err := listAllBTPanelSites(ctx, account, client, strings.TrimSpace(keyword))
	if err != nil {
		return nil, 0, trace, err
	}
	items = filterSiteItemsByType(items, siteType)
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return []SiteItem{}, total, trace, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return items[start:end], total, trace, nil
}

func filterSiteItemsByType(items []SiteItem, siteType string) []SiteItem {
	siteType = strings.TrimSpace(siteType)
	if siteType == "" || strings.EqualFold(siteType, "all") {
		return items
	}
	filtered := make([]SiteItem, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.Type, siteType) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func listAllBTPanelSites(ctx context.Context, account model.DeployAccount, client *http.Client, keyword string) ([]SiteItem, *Trace, error) {
	type projectEndpoint struct {
		siteType string
		path     string
		params   url.Values
	}
	fetchLimit := "1000"
	search := strings.TrimSpace(keyword)
	endpoints := []projectEndpoint{
		{siteType: "PHP", path: "/datalist/data/get_data_list", params: url.Values{"table": {"sites"}, "type": {"-1"}, "search": {search}, "p": {"1"}, "limit": {fetchLimit}, "order": {""}}},
		{siteType: "Java", path: "/mod/java/project/project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "Node", path: "/project/nodejs/get_project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "Go", path: "/project/go/get_project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "Python", path: "/project/python/GetProjectList", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "net", path: "/project/net/get_project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "Proxy", path: "/mod/proxy/com/get_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "HTML", path: "/project/html/get_project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
		{siteType: "Other", path: "/project/other/get_project_list", params: url.Values{"search": {search}, "p": {"1"}, "limit": {fetchLimit}, "type_id": {""}}},
	}

	items := make([]SiteItem, 0)
	requests := make([]map[string]string, 0, len(endpoints))
	responses := make([]map[string]any, 0, len(endpoints))
	combinedTrace := &Trace{RequestMethod: http.MethodPost, RequestHeaders: mustJSON(map[string]string{"Content-Type": "application/x-www-form-urlencoded"})}
	for _, endpoint := range endpoints {
		var out map[string]any
		trace, err := btPanelRequest(ctx, account, client, endpoint.path, endpoint.params, &out, "request_token")
		if trace != nil {
			requests = append(requests, map[string]string{"url": trace.RequestURL, "body": trace.RequestBody})
			responses = append(responses, map[string]any{"url": trace.RequestURL, "body": trace.ResponseBody, "error": errorString(err)})
		}
		if err != nil {
			if endpoint.siteType == "PHP" {
				combinedTrace.RequestURL = traceValue(trace, "RequestURL")
				combinedTrace.RequestBody = mustJSON(requests)
				combinedTrace.ResponseBody = mustJSON(responses)
				return nil, combinedTrace, err
			}
			continue
		}
		items = append(items, btPanelSiteItemsFromResponse(endpoint.siteType, account.Provider, out, search)...)
	}
	combinedTrace.RequestURL = strings.TrimRight(strings.TrimSpace(account.Endpoint), "/") + "/datalist/data/get_data_list + project get_project_list"
	combinedTrace.RequestBody = mustJSON(requests)
	combinedTrace.ResponseBody = mustJSON(responses)
	return items, combinedTrace, nil
}

func DeployCertificate(ctx context.Context, account model.DeployAccount, client *http.Client, siteName, certPEM, keyPEM string) (*Trace, error) {
	return DeployCertificateToTarget(ctx, account, client, Target{SiteName: siteName}, certPEM, keyPEM)
}

func DeployCertificateToTarget(ctx context.Context, account model.DeployAccount, client *http.Client, target Target, certPEM, keyPEM string) (*Trace, error) {
	if IsTencentCloudProvider(account.Provider) {
		certificateID, trace, err := uploadTencentCertificate(ctx, account, client, targetLabel(target), certPEM, keyPEM)
		if err != nil {
			return trace, err
		}
		return deployTencentCertificateInstance(ctx, account, client, certificateID, target, trace)
	}
	if !IsBTPanelProvider(account.Provider) {
		return nil, fmt.Errorf("暂不支持该部署服务：%s", account.Provider)
	}
	siteName := strings.TrimSpace(target.SiteName)
	if siteName == "" {
		return nil, fmt.Errorf("宝塔站点名称不能为空")
	}
	targetType, err := NormalizeBTPanelTargetType(target.ProductType)
	if err != nil {
		return nil, err
	}
	path := "/site"
	form := url.Values{}
	if targetType == BTPanelTargetDocker {
		// Docker 网站使用 Docker 模块自己的证书接口（官方文档未收录，参考 dnsmgr 的实现）
		path = "/mod/docker/com/set_ssl"
		form.Set("site_name", siteName)
	} else {
		form.Set("action", "SetSSL")
		form.Set("siteName", siteName)
	}
	form.Set("key", strings.TrimSpace(keyPEM))
	form.Set("csr", strings.TrimSpace(certPEM))
	var out map[string]any
	trace, err := btPanelRequest(ctx, account, client, path, form, &out, "request_token", "key", "csr")
	if err != nil {
		return trace, err
	}
	if !btPanelSuccess(out) {
		return trace, fmt.Errorf("宝塔面板返回失败：%s", trace.ResponseBody)
	}
	return trace, nil
}

func targetLabel(target Target) string {
	return firstString(target.ResourceName, target.SiteName, target.ResourceID)
}

func btPanelRequest(ctx context.Context, account model.DeployAccount, client *http.Client, path string, form url.Values, out any, sensitiveKeys ...string) (*Trace, error) {
	if strings.TrimSpace(account.AccessKey) == "" {
		return nil, fmt.Errorf("宝塔面板API密钥不能为空")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	token := md5Hex(timestamp + md5Hex(strings.TrimSpace(account.AccessKey)))
	form.Set("request_time", timestamp)
	form.Set("request_token", token)
	return btPanelPost(ctx, account, client, path, form, out, sensitiveKeys...)
}

func btPanelPost(ctx context.Context, account model.DeployAccount, client *http.Client, path string, form url.Values, out any, sensitiveKeys ...string) (*Trace, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(account.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("宝塔面板地址不能为空")
	}
	if strings.TrimSpace(account.AccessKey) == "" {
		return nil, fmt.Errorf("宝塔面板API密钥不能为空")
	}
	requestURL := endpoint + path
	trace := &Trace{
		RequestURL:    requestURL,
		RequestMethod: http.MethodPost,
		RequestHeaders: mustJSON(map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		}),
		RequestBody: maskFormBody(form, sensitiveKeys...),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return trace, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return trace, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	trace.ResponseBody = string(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return trace, fmt.Errorf("宝塔面板请求失败，HTTP状态码：%d，响应：%s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return trace, fmt.Errorf("解析宝塔面板响应失败：%w", err)
	}
	if message := btPanelErrorMessage(body); message != "" {
		return trace, errors.New(message)
	}
	return trace, nil
}

func btPanelErrorMessage(body []byte) string {
	var out struct {
		Status *bool  `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return ""
	}
	if out.Status != nil && !*out.Status {
		if strings.TrimSpace(out.Msg) != "" {
			return out.Msg
		}
		return "宝塔面板返回失败"
	}
	return ""
}

func btPanelSuccess(out map[string]any) bool {
	for _, key := range []string{"status", "success"} {
		value, ok := out[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case float64:
			return v == 1
		case string:
			text := strings.ToLower(strings.TrimSpace(v))
			return text == "true" || text == "1" || text == "success"
		}
	}
	if code, ok := out["code"].(float64); ok {
		return code == 0 || code == 200
	}
	return true
}

func maskFormBody(form url.Values, sensitiveKeys ...string) string {
	sensitive := map[string]bool{}
	for _, key := range sensitiveKeys {
		sensitive[key] = true
	}
	masked := url.Values{}
	for key, values := range form {
		for _, value := range values {
			if sensitive[key] && strings.TrimSpace(value) != "" {
				masked.Add(key, "***")
				continue
			}
			masked.Add(key, value)
		}
	}
	return masked.Encode()
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func btPanelSiteStatus(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "运行中"
		}
		return "已停止"
	case float64:
		if v == 1 {
			return "运行中"
		}
		if v == 0 {
			return "已停止"
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "running":
			return "运行中"
		case "0", "false", "stopped", "stop":
			return "已停止"
		default:
			return strings.TrimSpace(v)
		}
	default:
		return fmt.Sprint(value)
	}
}

func btPanelSiteItemsFromResponse(siteType, provider string, response map[string]any, keyword string) []SiteItem {
	rows := btPanelDataRows(response)
	items := make([]SiteItem, 0, len(rows))
	for _, row := range rows {
		item := SiteItem{
			ID:       firstMapValue(row, "id", "pid", "project_id"),
			Name:     firstStringMapValue(row, "name", "project_name", "site_name"),
			Type:     siteType,
			Path:     firstStringMapValue(row, "path", "project_path", "root_path"),
			Status:   btPanelSiteStatus(firstMapValue(row, "status", "run", "project_status")),
			Domains:  stringSliceMapValue(row, "domains", "domain"),
			Port:     firstMapValue(row, "port"),
			PS:       firstStringMapValue(row, "ps", "remark", "description"),
			SSL:      firstMapValue(row, "ssl"),
			Expire:   firstStringMapValue(row, "expire", "endtime"),
			AddTime:  firstStringMapValue(row, "addtime", "add_time", "create_time"),
			Provider: provider,
		}
		if item.Name == "" {
			item.Name = fmt.Sprint(item.ID)
		}
		if strings.TrimSpace(keyword) != "" && !siteItemMatchesKeyword(item, keyword) {
			continue
		}
		items = append(items, item)
	}
	return items
}

func btPanelDataRows(response map[string]any) []map[string]any {
	if rows := mapSlice(response["data"]); len(rows) > 0 {
		return rows
	}
	if data, ok := response["data"].(map[string]any); ok {
		for _, key := range []string{"data", "list", "items"} {
			if rows := mapSlice(data[key]); len(rows) > 0 {
				return rows
			}
		}
	}
	for _, key := range []string{"list", "items"} {
		if rows := mapSlice(response[key]); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func mapSlice(value any) []map[string]any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(values))
	for _, item := range values {
		if row, ok := item.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func firstMapValue(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func firstStringMapValue(row map[string]any, keys ...string) string {
	value := firstMapValue(row, keys...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringSliceMapValue(row map[string]any, keys ...string) []string {
	value := firstMapValue(row, keys...)
	switch v := value.(type) {
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				items = append(items, text)
			}
		}
		return items
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	default:
		return nil
	}
}

func siteItemMatchesKeyword(item SiteItem, keyword string) bool {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{item.Name, item.Path, item.PS, item.Type}, " "))
	if strings.Contains(haystack, needle) {
		return true
	}
	for _, domain := range item.Domains {
		if strings.Contains(strings.ToLower(domain), needle) {
			return true
		}
	}
	return false
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func traceValue(trace *Trace, field string) string {
	if trace == nil {
		return ""
	}
	if field == "RequestURL" {
		return trace.RequestURL
	}
	return ""
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
