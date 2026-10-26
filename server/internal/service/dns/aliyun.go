package dns

import (
	"context"
	"net/url"
	"strings"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

type aliyunProvider struct{}

func newAliyunClient(account Account) (*alidns.Client, error) {
	cfg := &openapi.Config{
		AccessKeyId:     tea.String(account.AccessKey),
		AccessKeySecret: tea.String(account.SecretKey),
		Endpoint:        tea.String("alidns.cn-hangzhou.aliyuncs.com"),
	}
	applyAliyunProxy(cfg, account.ProxyURL)
	return alidns.NewClient(cfg)
}

func applyAliyunProxy(cfg *openapi.Config, proxyURL string) {
	if strings.TrimSpace(proxyURL) == "" {
		return
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return
	}
	switch parsed.Scheme {
	case "http":
		cfg.HttpProxy = tea.String(proxyURL)
		cfg.HttpsProxy = tea.String(proxyURL)
	case "https":
		cfg.HttpsProxy = tea.String(proxyURL)
	case "socks5", "socks5h":
		cfg.Socks5Proxy = tea.String(proxyURL)
		cfg.Socks5NetWork = tea.String("tcp")
	}
}

func (aliyunProvider) Test(ctx context.Context, account Account) (*TestResult, error) {
	client, err := newAliyunClient(account)
	if err != nil {
		return nil, err
	}
	req := &alidns.DescribeDomainsRequest{PageSize: tea.Int64(1)}
	if _, err := client.DescribeDomainsWithContext(ctx, req, &dara.RuntimeOptions{}); err != nil {
		return nil, err
	}
	return &TestResult{Provider: "aliyun", OK: true, Message: "阿里云DNS账号连通性正常"}, nil
}

func (aliyunProvider) ListDomains(ctx context.Context, account Account) ([]DomainItem, error) {
	client, err := newAliyunClient(account)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeDomainsWithContext(ctx, &alidns.DescribeDomainsRequest{PageSize: tea.Int64(100)}, &dara.RuntimeOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]DomainItem, 0)
	if resp.Body == nil || resp.Body.Domains == nil {
		return items, nil
	}
	for _, item := range resp.Body.Domains.Domain {
		if item == nil {
			continue
		}
		items = append(items, DomainItem{
			ID:          tea.StringValue(item.DomainId),
			Name:        tea.StringValue(item.DomainName),
			RecordCount: int(tea.Int64Value(item.RecordCount)),
			Status:      aliStatus(tea.StringValue(item.DomainLoggingSwitchStatus)),
		})
	}
	return items, nil
}

func (aliyunProvider) ListRecords(ctx context.Context, account Account, domainName string) ([]RecordItem, error) {
	client, err := newAliyunClient(account)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeDomainRecordsWithContext(ctx, &alidns.DescribeDomainRecordsRequest{
		DomainName: tea.String(domainName),
		PageSize:   tea.Int64(500),
	}, &dara.RuntimeOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]RecordItem, 0)
	if resp.Body == nil || resp.Body.DomainRecords == nil {
		return items, nil
	}
	for _, item := range resp.Body.DomainRecords.Record {
		if item == nil {
			continue
		}
		items = append(items, RecordItem{
			ID:       tea.StringValue(item.RecordId),
			RR:       tea.StringValue(item.RR),
			Type:     tea.StringValue(item.Type),
			Value:    tea.StringValue(item.Value),
			Line:     tea.StringValue(item.Line),
			TTL:      tea.Int64Value(item.TTL),
			Priority: tea.Int64Value(item.Priority),
			Remark:   tea.StringValue(item.Remark),
			Status:   aliRecordStatus(tea.StringValue(item.Status)),
		})
	}
	return items, nil
}

func (aliyunProvider) ListRecordLines(_ context.Context, _ Account, _, _ string) ([]RecordLineItem, error) {
	return defaultRecordLines(), nil
}

func (aliyunProvider) CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	client, err := newAliyunClient(account)
	if err != nil {
		return nil, err
	}
	resp, err := client.AddDomainRecordWithContext(ctx, &alidns.AddDomainRecordRequest{
		DomainName: tea.String(input.DomainName),
		RR:         tea.String(input.RR),
		Type:       tea.String(input.Type),
		Value:      tea.String(input.Value),
		Line:       tea.String(input.Line),
		TTL:        tea.Int64(input.TTL),
		Priority:   tea.Int64(input.Priority),
	}, &dara.RuntimeOptions{})
	if err != nil {
		return nil, err
	}
	input.RecordID = tea.StringValue(resp.Body.RecordId)
	return recordFromInput(input), nil
}

func (aliyunProvider) UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	client, err := newAliyunClient(account)
	if err != nil {
		return nil, err
	}
	if _, err := client.UpdateDomainRecordWithContext(ctx, &alidns.UpdateDomainRecordRequest{
		RecordId: tea.String(input.RecordID),
		RR:       tea.String(input.RR),
		Type:     tea.String(input.Type),
		Value:    tea.String(input.Value),
		Line:     tea.String(input.Line),
		TTL:      tea.Int64(input.TTL),
		Priority: tea.Int64(input.Priority),
	}, &dara.RuntimeOptions{}); err != nil {
		return nil, err
	}
	return recordFromInput(input), nil
}

func (aliyunProvider) DeleteRecord(ctx context.Context, account Account, _ string, recordID string) error {
	client, err := newAliyunClient(account)
	if err != nil {
		return err
	}
	_, err = client.DeleteDomainRecordWithContext(ctx, &alidns.DeleteDomainRecordRequest{RecordId: tea.String(recordID)}, &dara.RuntimeOptions{})
	return err
}

func aliStatus(status string) string {
	if status == "" {
		return "enabled"
	}
	return strings.ToLower(status)
}

func aliRecordStatus(status string) string {
	if strings.EqualFold(status, "ENABLE") {
		return "enabled"
	}
	if strings.EqualFold(status, "DISABLE") {
		return "disabled"
	}
	return strings.ToLower(status)
}
