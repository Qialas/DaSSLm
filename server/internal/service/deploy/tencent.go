package deploy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
)

const (
	TencentCloudProvider = "tencentcloud"
	tencentSSLHost       = "ssl.tencentcloudapi.com"
	tencentSSLService    = "ssl"
	tencentSSLVersion    = "2019-12-05"
)

type tencentCloudError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func IsTencentCloudProvider(provider string) bool {
	return strings.EqualFold(provider, TencentCloudProvider)
}

func TestTencentCloud(ctx context.Context, account model.DeployAccount, client *http.Client) (*Trace, error) {
	var out struct {
		Response struct {
			Error      *tencentCloudError `json:"Error"`
			TotalCount uint64             `json:"TotalCount"`
			RequestID  string             `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, client, "", "DescribeCertificates", map[string]any{"Offset": 0, "Limit": 1, "CertificateType": "SVR"}, &out)
	if err != nil {
		return trace, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return trace, fmt.Errorf("%s：%s", out.Response.Error.Code, out.Response.Error.Message)
	}
	return trace, nil
}

func uploadTencentCertificate(ctx context.Context, account model.DeployAccount, client *http.Client, alias, certPEM, keyPEM string) (string, *Trace, error) {
	var out struct {
		Response struct {
			Error         *tencentCloudError `json:"Error"`
			CertificateID string             `json:"CertificateId"`
			RepeatCertID  string             `json:"RepeatCertId"`
			RequestID     string             `json:"RequestId"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"CertificatePublicKey":  strings.TrimSpace(certPEM),
		"CertificatePrivateKey": strings.TrimSpace(keyPEM),
		"CertificateType":       "SVR",
		"Alias":                 firstNonEmptyDeploy(alias, "DaSSLm上传证书"),
		"Repeatable":            false,
	}
	trace, err := tencentSSLRequest(ctx, account, client, "", "UploadCertificate", payload, &out)
	if err != nil {
		return "", trace, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return "", trace, fmt.Errorf("%s：%s", out.Response.Error.Code, out.Response.Error.Message)
	}
	certificateID := firstNonEmptyDeploy(out.Response.CertificateID, out.Response.RepeatCertID)
	if certificateID == "" {
		return "", trace, fmt.Errorf("腾讯云未返回证书ID")
	}
	return certificateID, trace, nil
}

// tencentRegionRequired 官方文档中要求传公共参数 Region 的产品（本项目支持的部分）。
var tencentRegionRequired = map[string]bool{"clb": true, "waf": true, "apigateway": true, "cos": true}

// NormalizeTencentProduct 规范腾讯云部署产品，取值与 DeployCertificateInstance 的 ResourceType 一致；
// ssl 表示只上传到证书管理。旧数据中的 eo 即 EdgeOne，官方取值为 teo。
func NormalizeTencentProduct(value string) (string, error) {
	product := strings.ToLower(strings.TrimSpace(value))
	switch product {
	case "", "ssl":
		return "ssl", nil
	case "eo", "teo":
		return "teo", nil
	case "cdn", "waf", "clb", "apigateway", "cos":
		return product, nil
	default:
		return "", fmt.Errorf("不支持的腾讯云部署产品：%s", value)
	}
}

// TencentDeployInstance 按官方 InstanceIdList 格式校验部署目标，返回规范后的产品、实例标识和地域。
// 地域只在官方要求的产品上保留，其他产品忽略。
func TencentDeployInstance(target Target) (product, instanceID, region string, err error) {
	product, err = NormalizeTencentProduct(target.ProductType)
	if err != nil || product == "ssl" {
		return product, "", "", err
	}
	name := tencentProductName(product)
	if tencentRegionRequired[product] {
		region = strings.TrimSpace(target.Region)
		if region == "" {
			return product, "", "", fmt.Errorf("腾讯云%s需要填写地域", name)
		}
	}
	raw := firstNonEmptyDeploy(target.ResourceID, target.SiteName)
	if raw == "" {
		return product, "", "", fmt.Errorf("请填写腾讯云%s的部署资源", name)
	}
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	filled := func(values []string) bool {
		for _, value := range values {
			if value == "" {
				return false
			}
		}
		return true
	}
	switch product {
	case "cdn":
		// 官方格式：域名|on，未带开关时默认 on
		if len(parts) == 1 && parts[0] != "" {
			parts = append(parts, "on")
		}
		if len(parts) != 2 || parts[0] == "" || (parts[1] != "on" && parts[1] != "off") {
			return product, "", "", fmt.Errorf("腾讯云CDN部署资源应为加速域名")
		}
	case "teo", "waf":
		if len(parts) != 1 || parts[0] == "" {
			return product, "", "", fmt.Errorf("腾讯云%s部署资源应为单个域名", name)
		}
	case "clb":
		// 官方格式：负载均衡ID|监听器ID，监听器开启 SNI 时再加 |域名
		if (len(parts) != 2 && len(parts) != 3) || !filled(parts) {
			return product, "", "", fmt.Errorf("腾讯云CLB需要填写负载均衡ID和监听器ID")
		}
	case "apigateway":
		if len(parts) != 2 || !filled(parts) {
			return product, "", "", fmt.Errorf("腾讯云API网关需要填写服务ID和自定义域名")
		}
	case "cos":
		if len(parts) != 3 || !filled(parts) {
			return product, "", "", fmt.Errorf("腾讯云COS需要填写存储桶和自定义域名")
		}
		if parts[0] != region {
			return product, "", "", fmt.Errorf("腾讯云COS存储桶地域（%s）与所选地域（%s）不一致", parts[0], region)
		}
	}
	return product, strings.Join(parts, "|"), region, nil
}

func deployTencentCertificateInstance(ctx context.Context, account model.DeployAccount, client *http.Client, certificateID string, target Target, uploadTrace *Trace) (*Trace, error) {
	product, instanceID, region, err := TencentDeployInstance(target)
	if err != nil {
		return uploadTrace, err
	}
	if product == "ssl" {
		if uploadTrace != nil {
			uploadTrace.ResponseBody = strings.TrimSpace(uploadTrace.ResponseBody)
			if uploadTrace.ResponseBody == "" {
				uploadTrace.ResponseBody = mustJSON(map[string]string{"CertificateId": certificateID})
			}
		}
		return uploadTrace, nil
	}
	payload := map[string]any{
		"CertificateId":  certificateID,
		"ResourceType":   product,
		"InstanceIdList": []string{instanceID},
	}
	var out struct {
		Response struct {
			Error          *tencentCloudError `json:"Error"`
			DeployRecordID json.Number        `json:"DeployRecordId"`
			DeployStatus   json.Number        `json:"DeployStatus"`
			RequestID      string             `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, client, region, "DeployCertificateInstance", payload, &out)
	combined := combineTencentDeployTrace(uploadTrace, trace)
	if err != nil {
		return combined, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return combined, fmt.Errorf("%s：%s", out.Response.Error.Code, out.Response.Error.Message)
	}
	return combined, nil
}

func combineTencentDeployTrace(uploadTrace, deployTrace *Trace) *Trace {
	if uploadTrace == nil {
		return deployTrace
	}
	if deployTrace == nil {
		return uploadTrace
	}
	return &Trace{
		RequestURL:     uploadTrace.RequestURL + " + " + deployTrace.RequestURL,
		RequestMethod:  http.MethodPost,
		RequestHeaders: mustJSON([]string{uploadTrace.RequestHeaders, deployTrace.RequestHeaders}),
		RequestBody:    mustJSON([]string{uploadTrace.RequestBody, deployTrace.RequestBody}),
		ResponseBody:   mustJSON([]string{uploadTrace.ResponseBody, deployTrace.ResponseBody}),
	}
}

func tencentProductName(productType string) string {
	switch strings.ToLower(strings.TrimSpace(productType)) {
	case "cdn":
		return "CDN"
	case "eo", "teo":
		return "EdgeOne"
	case "clb":
		return "CLB"
	case "waf":
		return "WAF"
	case "apigateway":
		return "API网关"
	case "cos":
		return "COS"
	default:
		return productType
	}
}

// tencentSSLRequest 调用腾讯云 SSL 接口；region 为公共参数 Region，非空时通过 X-TC-Region 请求头传递。
func tencentSSLRequest(ctx context.Context, account model.DeployAccount, client *http.Client, region, action string, payload any, out any) (*Trace, error) {
	if strings.TrimSpace(account.AccessKey) == "" || strings.TrimSpace(account.SecretKey) == "" {
		return nil, fmt.Errorf("请先填写腾讯云SecretId和SecretKey")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Unix()
	authorization, headers := tencentAuthorization(account, action, raw, timestamp)
	if region != "" {
		headers["X-TC-Region"] = region
	}
	trace := &Trace{
		RequestURL:     "https://" + tencentSSLHost,
		RequestMethod:  http.MethodPost,
		RequestHeaders: mustJSON(headers),
		RequestBody:    maskTencentPayload(action, raw),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+tencentSSLHost, bytes.NewReader(raw))
	if err != nil {
		return trace, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", tencentSSLHost)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", headers["X-TC-Timestamp"])
	req.Header.Set("X-TC-Version", tencentSSLVersion)
	if region != "" {
		req.Header.Set("X-TC-Region", region)
	}
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
		return trace, fmt.Errorf("腾讯云SSL请求失败，HTTP状态码：%d，响应：%s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return trace, fmt.Errorf("解析腾讯云SSL响应失败：%w", err)
	}
	return trace, nil
}

func tencentAuthorization(account model.DeployAccount, action string, rawPayload []byte, timestamp int64) (string, map[string]string) {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	hashedPayload := sha256HexDeploy(rawPayload)
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + tencentSSLHost + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{"POST", "/", "", canonicalHeaders, signedHeaders, hashedPayload}, "\n")
	credentialScope := date + "/" + tencentSSLService + "/tc3_request"
	stringToSign := strings.Join([]string{"TC3-HMAC-SHA256", strconv.FormatInt(timestamp, 10), credentialScope, sha256HexDeploy([]byte(canonicalRequest))}, "\n")
	secretDate := hmacSHA256Deploy([]byte("TC3"+account.SecretKey), date)
	secretService := hmacSHA256Deploy(secretDate, tencentSSLService)
	secretSigning := hmacSHA256Deploy(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256Deploy(secretSigning, stringToSign))
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", account.AccessKey, credentialScope, signedHeaders, signature)
	headers := map[string]string{
		"Authorization":  maskTencentAuthorizationDeploy(authorization),
		"Content-Type":   "application/json; charset=utf-8",
		"Host":           tencentSSLHost,
		"X-TC-Action":    action,
		"X-TC-Timestamp": strconv.FormatInt(timestamp, 10),
		"X-TC-Version":   tencentSSLVersion,
	}
	return authorization, headers
}

func maskTencentAuthorizationDeploy(value string) string {
	parts := strings.Split(value, "Signature=")
	if len(parts) != 2 || len(parts[1]) <= 8 {
		return value
	}
	signature := parts[1]
	return parts[0] + "Signature=" + signature[:4] + "***" + signature[len(signature)-4:]
}

func maskTencentPayload(action string, raw []byte) string {
	if !strings.EqualFold(action, "UploadCertificate") {
		return string(raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return string(raw)
	}
	if _, ok := payload["CertificatePublicKey"]; ok {
		payload["CertificatePublicKey"] = "***"
	}
	if _, ok := payload["CertificatePrivateKey"]; ok {
		payload["CertificatePrivateKey"] = "***"
	}
	masked, _ := json.Marshal(payload)
	return string(masked)
}

func sha256HexDeploy(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256Deploy(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func firstNonEmptyDeploy(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
