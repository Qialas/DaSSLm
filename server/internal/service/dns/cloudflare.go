package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type cloudflareProvider struct{}

func (cloudflareProvider) Test(ctx context.Context, account Account) (*TestResult, error) {
	if err := cfRequest(ctx, account, http.MethodGet, "/user/tokens/verify", nil, nil); err != nil {
		return nil, err
	}
	return &TestResult{Provider: "cloudflare", OK: true, Message: "Cloudflare账号连通性正常"}, nil
}

func (cloudflareProvider) ListDomains(ctx context.Context, account Account) ([]DomainItem, error) {
	var body struct {
		Result []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := cfRequest(ctx, account, http.MethodGet, "/zones?per_page=100", nil, &body); err != nil {
		return nil, err
	}
	items := make([]DomainItem, 0, len(body.Result))
	for _, item := range body.Result {
		items = append(items, DomainItem{ID: item.ID, Name: item.Name, Status: item.Status})
	}
	return items, nil
}

func (cloudflareProvider) ListRecords(ctx context.Context, account Account, domainName string) ([]RecordItem, error) {
	zoneID, err := cloudflareZoneID(ctx, account, domainName)
	if err != nil {
		return nil, err
	}
	var body struct {
		Result []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
			TTL     int64  `json:"ttl"`
			Comment string `json:"comment"`
			Proxied bool   `json:"proxied"`
		} `json:"result"`
	}
	if err := cfRequest(ctx, account, http.MethodGet, "/zones/"+zoneID+"/dns_records?per_page=500", nil, &body); err != nil {
		return nil, err
	}
	items := make([]RecordItem, 0, len(body.Result))
	for _, item := range body.Result {
		items = append(items, RecordItem{
			ID:      item.ID,
			RR:      cfRR(item.Name, domainName),
			Type:    item.Type,
			Value:   item.Content,
			TTL:     item.TTL,
			Proxied: item.Proxied,
			Remark:  item.Comment,
			Status:  "enabled",
		})
	}
	return items, nil
}

func (cloudflareProvider) ListRecordLines(_ context.Context, _ Account, _, _ string) ([]RecordLineItem, error) {
	return []RecordLineItem{}, nil
}

func (cloudflareProvider) CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	zoneID, err := cloudflareZoneID(ctx, account, input.DomainName)
	if err != nil {
		return nil, err
	}
	payload := cfRecordPayload(input)
	var body struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := cfRequest(ctx, account, http.MethodPost, "/zones/"+zoneID+"/dns_records", payload, &body); err != nil {
		return nil, err
	}
	input.RecordID = body.Result.ID
	return recordFromInput(input), nil
}

func (cloudflareProvider) UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	zoneID, err := cloudflareZoneID(ctx, account, input.DomainName)
	if err != nil {
		return nil, err
	}
	if err := cfRequest(ctx, account, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+input.RecordID, cfRecordPayload(input), nil); err != nil {
		return nil, err
	}
	return recordFromInput(input), nil
}

func (cloudflareProvider) DeleteRecord(ctx context.Context, account Account, domainName, recordID string) error {
	zoneID, err := cloudflareZoneID(ctx, account, domainName)
	if err != nil {
		return err
	}
	return cfRequest(ctx, account, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
}

func cfRequest(ctx context.Context, account Account, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+account.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	client := account.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare请求失败，HTTP状态码：%d，响应：%s", resp.StatusCode, truncateCloudflareResponse(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func truncateCloudflareResponse(raw []byte) string {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "-"
	}
	runes := []rune(body)
	if len(runes) <= 500 {
		return body
	}
	return string(runes[:500]) + "..."
}

func cloudflareZoneID(ctx context.Context, account Account, domainName string) (string, error) {
	var body struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := cfRequest(ctx, account, http.MethodGet, "/zones?name="+domainName, nil, &body); err != nil {
		return "", err
	}
	if len(body.Result) == 0 {
		return "", errors.New("Cloudflare未找到对应域名")
	}
	return body.Result[0].ID, nil
}

func cfRecordPayload(input RecordInput) map[string]any {
	name := input.RR
	if input.RR != "@" && !strings.HasSuffix(input.RR, "."+input.DomainName) {
		name = input.RR + "." + input.DomainName
	}
	if input.RR == "@" {
		name = input.DomainName
	}
	return map[string]any{"type": input.Type, "name": name, "content": input.Value, "ttl": input.TTL, "proxied": input.Proxied, "comment": input.Remark}
}

func cfRR(name, domainName string) string {
	if name == domainName {
		return "@"
	}
	return strings.TrimSuffix(name, "."+domainName)
}
