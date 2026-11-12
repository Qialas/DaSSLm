package deploy

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
)

// 雨云 SSL 证书中心（我的证书）接口，见 https://api.rainyun.com 的「SSL证书」分组：
//
//	GET  /product/sslcenter/       分页查询我的证书列表（sslcenter:read）
//	POST /product/sslcenter/       上传证书和私钥到我的证书列表（sslcenter:create），只返回 ok，不返回证书ID
//	PUT  /product/sslcenter/{id}   替换证书及私钥，证书ID不变（sslcenter:write）
//
// 认证使用请求头 X-Api-Key。
const (
	RainyunProvider        = "rainyun"
	RainyunDefaultEndpoint = "https://api.v2.rainyun.com"

	rainyunCertificatePath = "/product/sslcenter/"
	rainyunListPageSize    = 100
	rainyunListMaxPages    = 50
	rainyunMaxResponseSize = 8 << 20
)

func IsRainyunProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), RainyunProvider)
}

// RainyunCertificate 雨云「我的证书」中的一张证书。
type RainyunCertificate struct {
	ID          int64      `json:"id"`
	Domain      string     `json:"domain"`
	Domains     []string   `json:"domains"`
	Issuer      string     `json:"issuer"`
	StartAt     *time.Time `json:"startAt"`
	ExpireAt    *time.Time `json:"expireAt"`
	UploadAt    *time.Time `json:"uploadAt"`
	BindDomains []string   `json:"bindDomains"`
	Expired     bool       `json:"expired"`
}

type rainyunCertificateRecord struct {
	ID          int64    `json:"ID"`
	Domain      string   `json:"Domain"`
	Issuer      string   `json:"Issuer"`
	StartDate   int64    `json:"StartDate"`
	ExpDate     int64    `json:"ExpDate"`
	UploadTime  int64    `json:"UploadTime"`
	BindDomains []string `json:"BindDomains"`
}

type rainyunCertificatePage struct {
	Records      []rainyunCertificateRecord `json:"Records"`
	TotalRecords int                        `json:"TotalRecords"`
}

type rainyunResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// NormalizeRainyunCertificateID 校验部署目标中的雨云证书ID：留空表示新建证书，否则必须是正整数。
func NormalizeRainyunCertificateID(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "#"))
	if value == "" {
		return "", nil
	}
	id, err := strconv.ParseUint(value, 10, 63)
	if err != nil || id == 0 {
		return "", fmt.Errorf("雨云证书ID应为数字：%s", value)
	}
	return strconv.FormatUint(id, 10), nil
}

// TestRainyun 读取一条证书列表，检测 API 密钥和证书读取权限，返回证书总数。
func TestRainyun(ctx context.Context, account model.DeployAccount, client *http.Client) (int, *Trace, error) {
	page, trace, err := listRainyunCertificatePage(ctx, account, client, 1, 1)
	if err != nil {
		return 0, trace, err
	}
	return page.TotalRecords, trace, nil
}

// ListRainyunCertificates 返回雨云「我的证书」列表，按证书ID倒序（最新上传的在前），keyword 匹配域名或证书ID。
func ListRainyunCertificates(ctx context.Context, account model.DeployAccount, client *http.Client, page, pageSize int, keyword string) ([]RainyunCertificate, int, *Trace, error) {
	records, trace, err := listAllRainyunCertificates(ctx, account, client)
	if err != nil {
		return nil, 0, trace, err
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	now := time.Now()
	items := make([]RainyunCertificate, 0, len(records))
	for _, record := range records {
		item := record.toCertificate(now)
		if keyword != "" && !rainyunCertificateMatchesKeyword(item, keyword) {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return []RainyunCertificate{}, total, trace, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return items[start:end], total, trace, nil
}

// deployRainyunCertificate 部署证书到雨云：
//   - 部署目标已有证书ID：原地替换这张证书（证书ID不变，已绑定它的雨云产品继续使用）；
//   - 没有证书ID：上传为新证书，再从证书列表中找出它的ID，通过 Result 返回给调用方回写，之后的部署都更新这张证书。
//
// 上传接口不返回证书ID，所以按证书有效期和域名在列表中匹配；上传前如果列表里已有同一张证书（例如上次上传后没能记录ID），
// 直接使用它，避免重复上传。
func deployRainyunCertificate(ctx context.Context, account model.DeployAccount, client *http.Client, target Target, certPEM, keyPEM string) (*Trace, Result, error) {
	certificateID, err := NormalizeRainyunCertificateID(target.ResourceID)
	if err != nil {
		return nil, Result{}, err
	}
	payload := map[string]string{"cert": strings.TrimSpace(certPEM), "key": strings.TrimSpace(keyPEM)}
	if certificateID != "" {
		trace, err := rainyunRequest(ctx, account, client, http.MethodPut, rainyunCertificatePath+certificateID, nil, payload, nil)
		if err != nil {
			return trace, Result{}, fmt.Errorf("更新雨云证书（ID：%s）失败：%w。如果这张证书已在雨云删除，请编辑部署目标重新选择证书或改为新建证书", certificateID, err)
		}
		return trace, Result{ResourceID: certificateID, Message: fmt.Sprintf("已更新雨云证书（ID：%s）", certificateID)}, nil
	}

	cert, err := firstCertificateFromPEM(certPEM)
	if err != nil {
		return nil, Result{}, err
	}
	var traces traceList
	before, trace, err := listAllRainyunCertificates(ctx, account, client)
	traces.add(trace)
	if err != nil {
		return traces.combined(), Result{}, fmt.Errorf("读取雨云证书列表失败：%w", err)
	}
	if existing := pickRainyunCertificate(before, cert, nil, true); existing != nil {
		return traces.combined(), rainyunResult(*existing, "雨云中已有这张证书，未重复上传，已记录证书ID：%d，之后续期会更新这张证书"), nil
	}
	known := make(map[int64]bool, len(before))
	for _, record := range before {
		known[record.ID] = true
	}
	trace, err = rainyunRequest(ctx, account, client, http.MethodPost, rainyunCertificatePath, nil, payload, nil)
	traces.add(trace)
	if err != nil {
		return traces.combined(), Result{}, fmt.Errorf("上传证书到雨云失败：%w", err)
	}
	after, trace, err := listAllRainyunCertificates(ctx, account, client)
	traces.add(trace)
	if err != nil {
		return traces.combined(), Result{}, fmt.Errorf("证书已上传到雨云，但读取证书列表失败：%w", err)
	}
	created := pickRainyunCertificate(after, cert, known, true)
	if created == nil {
		// 有效期对不上时（时间精度不同等），退而用“新出现且域名相同”的证书
		created = pickRainyunCertificate(after, cert, known, false)
	}
	if created == nil {
		return traces.combined(), Result{}, fmt.Errorf("证书已上传到雨云，但没能在证书列表中找到它，请在雨云控制台确认后，编辑部署目标选择这张证书")
	}
	return traces.combined(), rainyunResult(*created, "已上传到雨云，新证书ID：%d，之后续期会更新这张证书"), nil
}

func rainyunResult(record rainyunCertificateRecord, format string) Result {
	return Result{
		ResourceID:   strconv.FormatInt(record.ID, 10),
		ResourceName: strings.TrimSpace(record.Domain),
		Message:      fmt.Sprintf(format, record.ID),
	}
}

// pickRainyunCertificate 在列表中找刚上传的证书：跳过 exclude 中已有的证书；matchValidity 为 true 时要求生效/到期时间一致，
// 并且列表记录带域名时必须与证书域名有交集。多张符合时取ID最大（最新）的一张。
func pickRainyunCertificate(records []rainyunCertificateRecord, cert *x509.Certificate, exclude map[int64]bool, matchValidity bool) *rainyunCertificateRecord {
	names := certificateDNSNames(cert)
	var picked *rainyunCertificateRecord
	for i := range records {
		record := &records[i]
		if record.ID <= 0 || exclude[record.ID] {
			continue
		}
		domains := splitRainyunDomains(record.Domain)
		if matchValidity {
			if record.StartDate != cert.NotBefore.Unix() || record.ExpDate != cert.NotAfter.Unix() {
				continue
			}
			if len(domains) > 0 && !domainsOverlap(domains, names) {
				continue
			}
		} else if !domainsOverlap(domains, names) {
			continue
		}
		if picked == nil || record.ID > picked.ID {
			picked = record
		}
	}
	return picked
}

func (record rainyunCertificateRecord) toCertificate(now time.Time) RainyunCertificate {
	item := RainyunCertificate{
		ID:          record.ID,
		Domain:      strings.TrimSpace(record.Domain),
		Domains:     splitRainyunDomains(record.Domain),
		Issuer:      strings.TrimSpace(record.Issuer),
		StartAt:     unixTime(record.StartDate),
		ExpireAt:    unixTime(record.ExpDate),
		UploadAt:    unixTime(record.UploadTime),
		BindDomains: record.BindDomains,
	}
	if item.BindDomains == nil {
		item.BindDomains = []string{}
	}
	item.Expired = item.ExpireAt != nil && !now.Before(*item.ExpireAt)
	return item
}

func rainyunCertificateMatchesKeyword(item RainyunCertificate, keyword string) bool {
	if strconv.FormatInt(item.ID, 10) == strings.TrimPrefix(keyword, "#") {
		return true
	}
	if strings.Contains(strings.ToLower(item.Domain), keyword) {
		return true
	}
	for _, domain := range item.BindDomains {
		if strings.Contains(strings.ToLower(domain), keyword) {
			return true
		}
	}
	return false
}

func listAllRainyunCertificates(ctx context.Context, account model.DeployAccount, client *http.Client) ([]rainyunCertificateRecord, *Trace, error) {
	var records []rainyunCertificateRecord
	var trace *Trace
	for page := 1; page <= rainyunListMaxPages; page++ {
		data, pageTrace, err := listRainyunCertificatePage(ctx, account, client, page, rainyunListPageSize)
		trace = pageTrace
		if err != nil {
			return nil, trace, err
		}
		records = append(records, data.Records...)
		if len(data.Records) == 0 || len(records) >= data.TotalRecords {
			break
		}
	}
	return records, trace, nil
}

func listRainyunCertificatePage(ctx context.Context, account model.DeployAccount, client *http.Client, page, perPage int) (rainyunCertificatePage, *Trace, error) {
	options, _ := json.Marshal(map[string]any{
		"columnFilters": map[string]string{"Domain": ""},
		"sort":          []any{},
		"page":          page,
		"perPage":       perPage,
	})
	var data rainyunCertificatePage
	trace, err := rainyunRequest(ctx, account, client, http.MethodGet, rainyunCertificatePath, url.Values{"options": {string(options)}}, nil, &data)
	return data, trace, err
}

func rainyunEndpoint(account model.DeployAccount) string {
	return strings.TrimRight(firstNonEmptyDeploy(account.Endpoint, RainyunDefaultEndpoint), "/")
}

// rainyunRequest 调用雨云接口；响应 code 为 200 表示成功，否则返回接口的 message。
func rainyunRequest(ctx context.Context, account model.DeployAccount, client *http.Client, method, path string, query url.Values, payload any, out any) (*Trace, error) {
	apiKey := strings.TrimSpace(account.AccessKey)
	if apiKey == "" {
		return nil, fmt.Errorf("请先填写雨云API密钥")
	}
	requestURL := rainyunEndpoint(account) + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	var body io.Reader
	var raw []byte
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	headers := map[string]string{"X-Api-Key": maskSecret(apiKey)}
	if payload != nil {
		headers["Content-Type"] = "application/json"
	}
	trace := &Trace{
		RequestURL:     requestURL,
		RequestMethod:  method,
		RequestHeaders: mustJSON(headers),
		RequestBody:    maskRainyunPayload(raw),
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return trace, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return trace, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, rainyunMaxResponseSize))
	trace.ResponseBody = string(respBody)
	if err != nil {
		return trace, err
	}
	var envelope rainyunResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return trace, fmt.Errorf("雨云接口请求失败，HTTP状态码：%d", resp.StatusCode)
		}
		return trace, fmt.Errorf("解析雨云响应失败：%w", err)
	}
	if envelope.Code != 200 {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = fmt.Sprintf("HTTP状态码：%d，业务状态码：%d", resp.StatusCode, envelope.Code)
		}
		return trace, fmt.Errorf("雨云返回失败：%s", message)
	}
	if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return trace, fmt.Errorf("解析雨云响应失败：%w", err)
		}
	}
	return trace, nil
}

func maskRainyunPayload(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return string(raw)
	}
	for _, key := range []string{"cert", "key"} {
		if _, ok := payload[key]; ok {
			payload[key] = "***"
		}
	}
	return mustJSON(payload)
}

func maskSecret(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

func firstCertificateFromPEM(value string) (*x509.Certificate, error) {
	rest := []byte(strings.TrimSpace(value))
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析证书失败：%w", err)
		}
		return cert, nil
	}
	return nil, fmt.Errorf("证书内容中没有找到 PEM 证书")
}

func certificateDNSNames(cert *x509.Certificate) []string {
	names := make([]string, 0, len(cert.DNSNames)+1)
	if cert.Subject.CommonName != "" {
		names = append(names, cert.Subject.CommonName)
	}
	names = append(names, cert.DNSNames...)
	return names
}

// splitRainyunDomains 拆分雨云返回的域名，多个域名以逗号、空格等分隔。
func splitRainyunDomains(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '|' || r == ' ' || r == '\n' || r == '\t'
	})
	domains := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(field)), ".")
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	return domains
}

func domainsOverlap(left, right []string) bool {
	set := make(map[string]bool, len(left))
	for _, value := range left {
		set[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")] = true
	}
	for _, value := range right {
		if set[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")] {
			return true
		}
	}
	return false
}

func unixTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	t := time.Unix(value, 0)
	return &t
}

// traceList 把一次部署中的多个请求合并成一条部署日志。
type traceList []*Trace

func (t *traceList) add(trace *Trace) {
	if trace != nil {
		*t = append(*t, trace)
	}
}

func (t traceList) combined() *Trace {
	switch len(t) {
	case 0:
		return nil
	case 1:
		return t[0]
	}
	urls := make([]string, 0, len(t))
	methods := make([]string, 0, len(t))
	headers := make([]string, 0, len(t))
	requests := make([]map[string]string, 0, len(t))
	responses := make([]string, 0, len(t))
	for _, item := range t {
		base := item.RequestURL
		if index := strings.Index(base, "?"); index >= 0 {
			base = base[:index]
		}
		urls = append(urls, base)
		methods = append(methods, item.RequestMethod)
		headers = append(headers, item.RequestHeaders)
		requests = append(requests, map[string]string{"method": item.RequestMethod, "url": item.RequestURL, "body": item.RequestBody})
		responses = append(responses, item.ResponseBody)
	}
	return &Trace{
		RequestURL:     truncateRunes(strings.Join(urls, " + "), 500),
		RequestMethod:  truncateRunes(strings.Join(methods, "+"), 16),
		RequestHeaders: mustJSON(headers),
		RequestBody:    mustJSON(requests),
		ResponseBody:   mustJSON(responses),
	}
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
