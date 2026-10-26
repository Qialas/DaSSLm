package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Account struct {
	Provider   string
	AccessKey  string
	SecretKey  string
	ProxyURL   string
	HTTPClient *http.Client
}

type DomainItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Grade       string `json:"grade"`
	RecordCount int    `json:"recordCount"`
	Status      string `json:"status"`
}

type RecordItem struct {
	ID       string `json:"id"`
	RR       string `json:"rr"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Line     string `json:"line"`
	TTL      int64  `json:"ttl"`
	Priority int64  `json:"priority"`
	Proxied  bool   `json:"proxied"`
	Remark   string `json:"remark"`
	Status   string `json:"status"`
}

type RecordLineItem struct {
	Name   string `json:"name"`
	LineID string `json:"lineId"`
}

type RecordInput struct {
	DomainName string
	RecordID   string
	RR         string `json:"rr"`
	Type       string `json:"type"`
	Value      string `json:"value"`
	Line       string `json:"line"`
	TTL        int64  `json:"ttl"`
	Priority   int64  `json:"priority"`
	Proxied    bool   `json:"proxied"`
	Remark     string `json:"remark"`
	Status     string `json:"status"`
}

type TestResult struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
}

type Provider interface {
	Test(ctx context.Context, account Account) (*TestResult, error)
	ListDomains(ctx context.Context, account Account) ([]DomainItem, error)
	ListRecords(ctx context.Context, account Account, domainName string) ([]RecordItem, error)
	ListRecordLines(ctx context.Context, account Account, domainName, domainGrade string) ([]RecordLineItem, error)
	CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error)
	UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error)
	DeleteRecord(ctx context.Context, account Account, domainName, recordID string) error
}

func NewProvider(name string) (Provider, error) {
	switch strings.ToLower(name) {
	case "aliyun":
		return aliyunProvider{}, nil
	case "dnspod", "tencentcloud":
		return tencentProvider{}, nil
	case "tencent_eo":
		return tencentEOProvider{}, nil
	case "cloudflare":
		return cloudflareProvider{}, nil
	default:
		return nil, fmt.Errorf("暂不支持该DNS渠道商：%s", name)
	}
}

func TestAccount(ctx context.Context, account Account) (*TestResult, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	return provider.Test(ctx, account)
}

func checkedProvider(account Account) (Provider, error) {
	provider, err := NewProvider(account.Provider)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(account.Provider) == "cloudflare" {
		if strings.TrimSpace(account.SecretKey) == "" {
			return nil, errors.New("请先填写Cloudflare API Token")
		}
	} else if strings.TrimSpace(account.AccessKey) == "" || strings.TrimSpace(account.SecretKey) == "" {
		return nil, errors.New("请先填写AccessKey和SecretKey")
	}
	return provider, nil
}

func ListDomains(ctx context.Context, account Account) ([]DomainItem, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	return provider.ListDomains(ctx, account)
}

func ListRecords(ctx context.Context, account Account, domainName string) ([]RecordItem, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	return provider.ListRecords(ctx, account, domainName)
}

func ListRecordLines(ctx context.Context, account Account, domainName, domainGrade string) ([]RecordLineItem, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	return provider.ListRecordLines(ctx, account, domainName, domainGrade)
}

func CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	normalizeRecordInput(&input)
	normalizeProviderRecordInput(account, &input)
	return provider.CreateRecord(ctx, account, input)
}

func UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	provider, err := checkedProvider(account)
	if err != nil {
		return nil, err
	}
	normalizeRecordInput(&input)
	normalizeProviderRecordInput(account, &input)
	return provider.UpdateRecord(ctx, account, input)
}

func DeleteRecord(ctx context.Context, account Account, domainName, recordID string) error {
	provider, err := checkedProvider(account)
	if err != nil {
		return err
	}
	return provider.DeleteRecord(ctx, account, domainName, recordID)
}

func normalizeRecordInput(input *RecordInput) {
	input.RR = strings.TrimSpace(input.RR)
	if input.RR == "" {
		input.RR = "@"
	}
	input.Type = strings.ToUpper(strings.TrimSpace(input.Type))
	input.Value = strings.TrimSpace(input.Value)
	if input.Line == "" {
		input.Line = "default"
	}
	if input.TTL <= 0 {
		input.TTL = 600
	}
	if input.Status == "" {
		input.Status = "enabled"
	}
}

func normalizeProviderRecordInput(account Account, input *RecordInput) {
	if strings.EqualFold(account.Provider, "cloudflare") {
		input.Line = ""
	}
	if strings.EqualFold(account.Provider, "dnspod") || strings.EqualFold(account.Provider, "tencentcloud") {
		input.Line = "default"
	}
	if strings.EqualFold(account.Provider, "tencent_eo") {
		input.Line = ""
	}
}

func recordFromInput(input RecordInput) *RecordItem {
	return &RecordItem{
		ID:       input.RecordID,
		RR:       input.RR,
		Type:     input.Type,
		Value:    input.Value,
		Line:     input.Line,
		TTL:      input.TTL,
		Priority: input.Priority,
		Proxied:  input.Proxied,
		Remark:   input.Remark,
		Status:   input.Status,
	}
}

func defaultRecordLines() []RecordLineItem {
	return []RecordLineItem{{Name: "默认"}}
}
