package ssl

import (
	"bytes"
	"context"
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
	tencentSSLHost    = "ssl.tencentcloudapi.com"
	tencentSSLVersion = "2019-12-05"
)

type HTTPClientContextKey struct{}

type Trace struct {
	RequestURL     string
	RequestMethod  string
	RequestHeaders string
	RequestBody    string
	ResponseBody   string
}

type TencentError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type TencentCertificateItem struct {
	CertificateID   string   `json:"certificateId"`
	Domain          string   `json:"domain"`
	Alias           string   `json:"alias"`
	ProductZhName   string   `json:"productZhName"`
	Status          *uint64  `json:"status"`
	StatusName      string   `json:"statusName"`
	StatusMsg       string   `json:"statusMsg"`
	VerifyType      string   `json:"verifyType"`
	CertBeginTime   string   `json:"certBeginTime"`
	CertEndTime     string   `json:"certEndTime"`
	InsertTime      string   `json:"insertTime"`
	PackageTypeName string   `json:"packageTypeName"`
	SubjectAltName  []string `json:"subjectAltName"`
}

func CertificateFromTencentItem(accountID uint, item TencentCertificateItem) model.Certificate {
	sans, _ := json.Marshal(item.SubjectAltName)
	row := model.Certificate{
		SSLAccountID:          accountID,
		CommonName:            item.Domain,
		SANs:                  string(sans),
		Issuer:                item.ProductZhName,
		ProviderCertificateID: item.CertificateID,
		ProviderStatusMsg:     firstNonEmpty(item.StatusName, item.StatusMsg),
		VerifyType:            item.VerifyType,
		RenewBeforeDay:        30,
		Status:                certStatusPending,
	}
	if item.Status != nil {
		row.ProviderStatus = strconv.FormatUint(*item.Status, 10)
		row.Status = TencentCertStatus(*item.Status, certStatusPending)
	}
	if expiresAt := ParseTencentTime(item.CertEndTime); expiresAt != nil {
		row.ExpiresAt = expiresAt
	}
	if row.CommonName == "" {
		row.CommonName = fmt.Sprintf("certificate-%s", item.CertificateID)
	}
	return row
}

func ListTencentCertificates(ctx context.Context, account model.SSLAccount, offset, limit uint64, searchKey string) ([]TencentCertificateItem, uint64, error) {
	var out struct {
		Response struct {
			Error        *TencentError `json:"Error"`
			TotalCount   uint64        `json:"TotalCount"`
			Certificates []struct {
				CertificateID   string   `json:"CertificateId"`
				Domain          string   `json:"Domain"`
				Alias           string   `json:"Alias"`
				ProductZhName   string   `json:"ProductZhName"`
				Status          *uint64  `json:"Status"`
				StatusName      string   `json:"StatusName"`
				StatusMsg       string   `json:"StatusMsg"`
				VerifyType      string   `json:"VerifyType"`
				CertBeginTime   string   `json:"CertBeginTime"`
				CertEndTime     string   `json:"CertEndTime"`
				InsertTime      string   `json:"InsertTime"`
				PackageTypeName string   `json:"PackageTypeName"`
				SubjectAltName  []string `json:"SubjectAltName"`
			} `json:"Certificates"`
			RequestID string `json:"RequestId"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"Offset":          offset,
		"Limit":           limit,
		"CertificateType": "SVR",
	}
	if strings.TrimSpace(searchKey) != "" {
		payload["SearchKey"] = strings.TrimSpace(searchKey)
	}
	trace, err := TencentRequest(ctx, account, "DescribeCertificates", payload, &out)
	if err != nil {
		return nil, 0, err
	}
	if err := TencentResponseErr(out.Response.Error, trace); err != nil {
		return nil, 0, err
	}
	items := make([]TencentCertificateItem, 0, len(out.Response.Certificates))
	for _, item := range out.Response.Certificates {
		items = append(items, TencentCertificateItem{
			CertificateID:   item.CertificateID,
			Domain:          item.Domain,
			Alias:           item.Alias,
			ProductZhName:   item.ProductZhName,
			Status:          item.Status,
			StatusName:      item.StatusName,
			StatusMsg:       item.StatusMsg,
			VerifyType:      item.VerifyType,
			CertBeginTime:   item.CertBeginTime,
			CertEndTime:     item.CertEndTime,
			InsertTime:      item.InsertTime,
			PackageTypeName: item.PackageTypeName,
			SubjectAltName:  item.SubjectAltName,
		})
	}
	return items, out.Response.TotalCount, nil
}

func TencentRequest(ctx context.Context, account model.SSLAccount, action string, payload any, out any) (*Trace, error) {
	if strings.TrimSpace(account.AccessKey) == "" || strings.TrimSpace(account.SecretKey) == "" {
		return nil, fmt.Errorf("请先填写腾讯云SecretId和SecretKey")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().Unix()
	authorization, headers := TencentAuthorization(account, action, raw, timestamp)
	trace := &Trace{
		RequestURL:     "https://" + tencentSSLHost,
		RequestMethod:  http.MethodPost,
		RequestBody:    string(raw),
		RequestHeaders: mustJSON(headers),
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

	client, ok := ctx.Value(HTTPClientContextKey{}).(*http.Client)
	if !ok || client == nil {
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

func TencentResponseErr(err *TencentError, trace *Trace) error {
	if err == nil || err.Code == "" {
		return nil
	}
	return &TracedError{Err: fmt.Errorf("%s：%s", err.Code, err.Message), Trace: trace}
}

type TracedError struct {
	Err   error
	Trace *Trace
}

func (e *TracedError) Error() string {
	return e.Err.Error()
}

func (e *TracedError) Unwrap() error {
	return e.Err
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TencentCertStatus(status uint64, fallback string) string {
	switch status {
	case 0, 8:
		return certStatusSubmitted
	case 1:
		return certStatusIssued
	case 2:
		return certStatusFailed
	case 3:
		return certStatusExpired
	case 4:
		return certStatusDNSAdded
	case 6, 7:
		return certStatusCanceled
	case 9:
		return certStatusRevoking
	case 10:
		return certStatusRevoked
	case 12:
		return certStatusRevoking
	default:
		if fallback != "" {
			return fallback
		}
		return certStatusPending
	}
}

func ParseTencentTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &t
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const (
	certStatusPending   = "pending"
	certStatusDNSAdded  = "dns_added"
	certStatusSubmitted = "submitted"
	certStatusIssued    = "issued"
	certStatusFailed    = "failed"
	certStatusExpired   = "expired"
	certStatusCanceled  = "canceled"
	certStatusRevoking  = "revoking"
	certStatusRevoked   = "revoked"
)
