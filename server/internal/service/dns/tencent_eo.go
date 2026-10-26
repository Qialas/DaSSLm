package dns

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
)

const (
	tencentEOHost    = "teo.tencentcloudapi.com"
	tencentEOService = "teo"
	tencentEOVersion = "2022-09-01"
)

type tencentEOProvider struct{}

type tencentEOError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func (tencentEOProvider) Test(ctx context.Context, account Account) (*TestResult, error) {
	var out struct {
		Response struct {
			Error *tencentEOError `json:"Error"`
		} `json:"Response"`
	}
	if err := tencentEORequest(ctx, account, "DescribeZones", map[string]any{"Limit": 1}, &out); err != nil {
		return nil, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return nil, tencentEOErr(out.Response.Error)
	}
	return &TestResult{Provider: "tencent_eo", OK: true, Message: "腾讯云EO账号连通性正常"}, nil
}

func (tencentEOProvider) ListDomains(ctx context.Context, account Account) ([]DomainItem, error) {
	items := make([]DomainItem, 0)
	offset := int64(0)
	limit := int64(100)
	for {
		var out struct {
			Response struct {
				Error      *tencentEOError `json:"Error"`
				TotalCount int64           `json:"TotalCount"`
				Zones      []struct {
					ZoneID       string `json:"ZoneId"`
					ZoneName     string `json:"ZoneName"`
					Type         string `json:"Type"`
					Status       string `json:"Status"`
					ActiveStatus string `json:"ActiveStatus"`
				} `json:"Zones"`
			} `json:"Response"`
		}
		if err := tencentEORequest(ctx, account, "DescribeZones", map[string]any{"Offset": offset, "Limit": limit}, &out); err != nil {
			return nil, err
		}
		if out.Response.Error != nil && out.Response.Error.Code != "" {
			return nil, tencentEOErr(out.Response.Error)
		}
		for _, zone := range out.Response.Zones {
			if !strings.EqualFold(zone.Type, "full") {
				continue
			}
			items = append(items, DomainItem{
				ID:     zone.ZoneID,
				Name:   zone.ZoneName,
				Grade:  zone.Type,
				Status: tencentEOStatus(firstNonEmptyString(zone.ActiveStatus, zone.Status)),
			})
		}
		offset += int64(len(out.Response.Zones))
		if len(out.Response.Zones) == 0 || offset >= out.Response.TotalCount {
			break
		}
	}
	return items, nil
}

func (tencentEOProvider) ListRecords(ctx context.Context, account Account, domainRef string) ([]RecordItem, error) {
	zoneID, zoneName, err := tencentEOZoneRef(ctx, account, domainRef)
	if err != nil {
		return nil, err
	}
	items := make([]RecordItem, 0)
	offset := int64(0)
	limit := int64(1000)
	for {
		var out struct {
			Response struct {
				Error      *tencentEOError `json:"Error"`
				TotalCount int64           `json:"TotalCount"`
				DnsRecords []struct {
					RecordID   string `json:"RecordId"`
					Name       string `json:"Name"`
					Type       string `json:"Type"`
					Content    string `json:"Content"`
					TTL        *int64 `json:"TTL"`
					Priority   *int64 `json:"Priority"`
					Status     string `json:"Status"`
					ModifiedOn string `json:"ModifiedOn"`
				} `json:"DnsRecords"`
			} `json:"Response"`
		}
		payload := map[string]any{"ZoneId": zoneID, "Offset": offset, "Limit": limit}
		if err := tencentEORequest(ctx, account, "DescribeDnsRecords", payload, &out); err != nil {
			return nil, err
		}
		if out.Response.Error != nil && out.Response.Error.Code != "" {
			return nil, tencentEOErr(out.Response.Error)
		}
		for _, record := range out.Response.DnsRecords {
			items = append(items, RecordItem{
				ID:       record.RecordID,
				RR:       tencentEORR(record.Name, zoneName),
				Type:     strings.ToUpper(record.Type),
				Value:    record.Content,
				Line:     "",
				TTL:      int64PtrValue(record.TTL),
				Priority: int64PtrValue(record.Priority),
				Status:   tencentEORecordStatus(record.Status),
			})
		}
		offset += int64(len(out.Response.DnsRecords))
		if len(out.Response.DnsRecords) == 0 || offset >= out.Response.TotalCount {
			break
		}
	}
	return items, nil
}

func (tencentEOProvider) ListRecordLines(_ context.Context, _ Account, _, _ string) ([]RecordLineItem, error) {
	return []RecordLineItem{}, nil
}

func (tencentEOProvider) CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	zoneID, zoneName, err := tencentEOZoneRef(ctx, account, input.DomainName)
	if err != nil {
		return nil, err
	}
	payload := tencentEORecordPayload(zoneID, zoneName, input)
	var out struct {
		Response struct {
			Error    *tencentEOError `json:"Error"`
			RecordID string          `json:"RecordId"`
		} `json:"Response"`
	}
	if err := tencentEORequest(ctx, account, "CreateDnsRecord", payload, &out); err != nil {
		return nil, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return nil, tencentEOErr(out.Response.Error)
	}
	input.RecordID = out.Response.RecordID
	if input.Status == "disabled" {
		if err := tencentEOSetRecordStatus(ctx, account, zoneID, input.RecordID, input.Status); err != nil {
			return nil, err
		}
	}
	return recordFromInput(input), nil
}

func (tencentEOProvider) UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	zoneID, zoneName, err := tencentEOZoneRef(ctx, account, input.DomainName)
	if err != nil {
		return nil, err
	}
	record := map[string]any{
		"RecordId": input.RecordID,
		"Name":     tencentEORecordName(input.RR, zoneName),
		"Type":     strings.ToUpper(input.Type),
		"Content":  input.Value,
		"TTL":      input.TTL,
	}
	if strings.EqualFold(input.Type, "MX") {
		record["Priority"] = input.Priority
	}
	payload := map[string]any{"ZoneId": zoneID, "DnsRecords": []map[string]any{record}}
	var out struct {
		Response struct {
			Error *tencentEOError `json:"Error"`
		} `json:"Response"`
	}
	if err := tencentEORequest(ctx, account, "ModifyDnsRecords", payload, &out); err != nil {
		return nil, err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return nil, tencentEOErr(out.Response.Error)
	}
	if err := tencentEOSetRecordStatus(ctx, account, zoneID, input.RecordID, input.Status); err != nil {
		return nil, err
	}
	return recordFromInput(input), nil
}

func (tencentEOProvider) DeleteRecord(ctx context.Context, account Account, domainRef, recordID string) error {
	zoneID, _, err := tencentEOZoneRef(ctx, account, domainRef)
	if err != nil {
		return err
	}
	var out struct {
		Response struct {
			Error *tencentEOError `json:"Error"`
		} `json:"Response"`
	}
	payload := map[string]any{"ZoneId": zoneID, "RecordIds": []string{recordID}}
	if err := tencentEORequest(ctx, account, "DeleteDnsRecords", payload, &out); err != nil {
		return err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return tencentEOErr(out.Response.Error)
	}
	return nil
}

func tencentEOSetRecordStatus(ctx context.Context, account Account, zoneID, recordID, status string) error {
	if strings.TrimSpace(recordID) == "" {
		return nil
	}
	payload := map[string]any{"ZoneId": zoneID}
	if status == "disabled" {
		payload["RecordsToDisable"] = []string{recordID}
	} else {
		payload["RecordsToEnable"] = []string{recordID}
	}
	var out struct {
		Response struct {
			Error *tencentEOError `json:"Error"`
		} `json:"Response"`
	}
	if err := tencentEORequest(ctx, account, "ModifyDnsRecordsStatus", payload, &out); err != nil {
		return err
	}
	if out.Response.Error != nil && out.Response.Error.Code != "" {
		return tencentEOErr(out.Response.Error)
	}
	return nil
}

func tencentEORecordPayload(zoneID, zoneName string, input RecordInput) map[string]any {
	payload := map[string]any{
		"ZoneId":  zoneID,
		"Name":    tencentEORecordName(input.RR, zoneName),
		"Type":    strings.ToUpper(input.Type),
		"Content": input.Value,
		"TTL":     input.TTL,
	}
	if strings.EqualFold(input.Type, "MX") {
		payload["Priority"] = input.Priority
	}
	return payload
}

func tencentEOZoneRef(ctx context.Context, account Account, domainRef string) (string, string, error) {
	zoneID, zoneName := splitTencentEORef(domainRef)
	if zoneID != "" {
		return zoneID, zoneName, nil
	}
	if zoneName == "" {
		return "", "", fmt.Errorf("腾讯云EO站点ID为空")
	}
	zones, err := tencentEOProvider{}.ListDomains(ctx, account)
	if err != nil {
		return "", "", err
	}
	for _, item := range zones {
		if strings.EqualFold(item.Name, zoneName) {
			return item.ID, item.Name, nil
		}
	}
	return "", "", fmt.Errorf("腾讯云EO未找到NS接入站点：%s", zoneName)
}

func splitTencentEORef(value string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(value), "|", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "zone-") {
		return value, ""
	}
	return "", value
}

func tencentEORecordName(rr, zoneName string) string {
	rr = strings.TrimSpace(rr)
	zoneName = strings.Trim(strings.TrimSpace(zoneName), ".")
	if rr == "" || rr == "@" {
		return zoneName
	}
	rr = strings.Trim(rr, ".")
	if zoneName != "" && (strings.EqualFold(rr, zoneName) || strings.HasSuffix(strings.ToLower(rr), "."+strings.ToLower(zoneName))) {
		return rr
	}
	if zoneName == "" {
		return rr
	}
	return rr + "." + zoneName
}

func tencentEORR(name, zoneName string) string {
	name = strings.Trim(strings.TrimSpace(name), ".")
	zoneName = strings.Trim(strings.TrimSpace(zoneName), ".")
	if zoneName == "" {
		return name
	}
	if strings.EqualFold(name, zoneName) {
		return "@"
	}
	suffix := "." + strings.ToLower(zoneName)
	if strings.HasSuffix(strings.ToLower(name), suffix) {
		return strings.TrimSuffix(name[:len(name)-len(suffix)], ".")
	}
	return name
}

func tencentEOStatus(status string) string {
	if strings.EqualFold(status, "active") {
		return "enabled"
	}
	if strings.EqualFold(status, "inactive") || strings.EqualFold(status, "paused") {
		return "disabled"
	}
	return strings.ToLower(status)
}

func tencentEORecordStatus(status string) string {
	if strings.EqualFold(status, "enable") {
		return "enabled"
	}
	if strings.EqualFold(status, "disable") {
		return "disabled"
	}
	return strings.ToLower(status)
}

func tencentEORequest(ctx context.Context, account Account, action string, payload any, out any) error {
	if strings.TrimSpace(account.AccessKey) == "" || strings.TrimSpace(account.SecretKey) == "" {
		return fmt.Errorf("请先填写腾讯云SecretId和SecretKey")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	timestamp := time.Now().Unix()
	authorization := tencentEOAuthorization(account, raw, timestamp)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+tencentEOHost, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", tencentEOHost)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", tencentEOVersion)
	client := account.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("腾讯云EO请求失败，HTTP状态码：%d，响应：%s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析腾讯云EO响应失败：%w", err)
	}
	return nil
}

func tencentEOAuthorization(account Account, rawPayload []byte, timestamp int64) string {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	hashedPayload := sha256HexDNS(rawPayload)
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + tencentEOHost + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{"POST", "/", "", canonicalHeaders, signedHeaders, hashedPayload}, "\n")
	credentialScope := date + "/" + tencentEOService + "/tc3_request"
	stringToSign := strings.Join([]string{"TC3-HMAC-SHA256", strconv.FormatInt(timestamp, 10), credentialScope, sha256HexDNS([]byte(canonicalRequest))}, "\n")
	secretDate := hmacSHA256DNS([]byte("TC3"+account.SecretKey), date)
	secretService := hmacSHA256DNS(secretDate, tencentEOService)
	secretSigning := hmacSHA256DNS(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256DNS(secretSigning, stringToSign))
	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", account.AccessKey, credentialScope, signedHeaders, signature)
}

func tencentEOErr(err *tencentEOError) error {
	return fmt.Errorf("%s：%s", err.Code, err.Message)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func int64PtrValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func sha256HexDNS(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256DNS(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
