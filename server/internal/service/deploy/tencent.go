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
	trace, err := tencentSSLRequest(ctx, account, client, "DescribeCertificates", map[string]any{"Offset": 0, "Limit": 1, "CertificateType": "SVR"}, &out)
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
	trace, err := tencentSSLRequest(ctx, account, client, "UploadCertificate", payload, &out)
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

func deployTencentCertificateInstance(ctx context.Context, account model.DeployAccount, client *http.Client, certificateID string, target Target, uploadTrace *Trace) (*Trace, error) {
	productType := strings.ToLower(strings.TrimSpace(target.ProductType))
	if productType == "" || productType == "ssl" {
		if uploadTrace != nil {
			uploadTrace.ResponseBody = strings.TrimSpace(uploadTrace.ResponseBody)
			if uploadTrace.ResponseBody == "" {
				uploadTrace.ResponseBody = mustJSON(map[string]string{"CertificateId": certificateID})
			}
		}
		return uploadTrace, nil
	}
	instanceID := firstNonEmptyDeploy(target.ResourceID, target.SiteName)
	if instanceID == "" {
		return uploadTrace, fmt.Errorf("请选择或填写腾讯云%s部署资源", tencentProductName(productType))
	}
	payload := map[string]any{
		"CertificateId":  certificateID,
		"ResourceType":   productType,
		"InstanceIdList": []string{instanceID},
	}
	if strings.TrimSpace(target.Region) != "" {
		payload["Region"] = strings.TrimSpace(target.Region)
	}
	if strings.TrimSpace(target.ExtraConfig) != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(target.ExtraConfig), &extra); err != nil {
			return uploadTrace, fmt.Errorf("腾讯云部署扩展配置不是有效JSON：%w", err)
		}
		for key, value := range extra {
			payload[key] = value
		}
	}
	var out struct {
		Response struct {
			Error     *tencentCloudError `json:"Error"`
			DeployID  string             `json:"DeployId"`
			TaskID    string             `json:"TaskId"`
			RequestID string             `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, client, "DeployCertificateInstance", payload, &out)
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
	case "eo":
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

func tencentSSLRequest(ctx context.Context, account model.DeployAccount, client *http.Client, action string, payload any, out any) (*Trace, error) {
	if strings.TrimSpace(account.AccessKey) == "" || strings.TrimSpace(account.SecretKey) == "" {
		return nil, fmt.Errorf("请先填写腾讯云SecretId和SecretKey")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Unix()
	authorization, headers := tencentAuthorization(account, action, raw, timestamp)
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
