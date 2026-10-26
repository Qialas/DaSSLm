package dns

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

type tencentProvider struct{}

func newTencentClient(account Account) (*dnspod.Client, error) {
	cred := common.NewCredential(account.AccessKey, account.SecretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"
	if strings.HasPrefix(account.ProxyURL, "http://") || strings.HasPrefix(account.ProxyURL, "https://") {
		cpf.HttpProfile.Proxy = account.ProxyURL
	}
	return dnspod.NewClient(cred, "", cpf)
}

func (tencentProvider) Test(ctx context.Context, account Account) (*TestResult, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	req := dnspod.NewDescribeDomainListRequest()
	if _, err := client.DescribeDomainListWithContext(ctx, req); err != nil {
		return nil, tencentErr(err)
	}
	return &TestResult{Provider: "tencentcloud", OK: true, Message: "腾讯云DNSPod账号连通性正常"}, nil
}

func (tencentProvider) ListDomains(ctx context.Context, account Account) ([]DomainItem, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	limit := int64(100)
	req := dnspod.NewDescribeDomainListRequest()
	req.Limit = &limit
	resp, err := client.DescribeDomainListWithContext(ctx, req)
	if err != nil {
		return nil, tencentErr(err)
	}
	items := make([]DomainItem, 0)
	for _, item := range resp.Response.DomainList {
		if item == nil {
			continue
		}
		items = append(items, DomainItem{
			ID:          strconv.FormatUint(uint64Value(item.DomainId), 10),
			Name:        stringValue(item.Name),
			Grade:       stringValue(item.Grade),
			RecordCount: int(uint64Value(item.RecordCount)),
			Status:      tencentStatus(stringValue(item.Status)),
		})
	}
	return items, nil
}

func (tencentProvider) ListRecords(ctx context.Context, account Account, domainName string) ([]RecordItem, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	limit := uint64(3000)
	no := "no"
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = &domainName
	req.Limit = &limit
	req.ErrorOnEmpty = &no
	resp, err := client.DescribeRecordListWithContext(ctx, req)
	if err != nil {
		return nil, tencentErr(err)
	}
	items := make([]RecordItem, 0)
	for _, item := range resp.Response.RecordList {
		if item == nil {
			continue
		}
		if strings.EqualFold(stringValue(item.Type), "NS") {
			continue
		}
		items = append(items, RecordItem{
			ID:       strconv.FormatUint(uint64Value(item.RecordId), 10),
			RR:       stringValue(item.Name),
			Type:     stringValue(item.Type),
			Value:    stringValue(item.Value),
			Line:     stringValue(item.Line),
			TTL:      int64(uint64Value(item.TTL)),
			Priority: int64(uint64Value(item.MX)),
			Remark:   stringValue(item.Remark),
			Status:   tencentRecordStatus(stringValue(item.Status)),
		})
	}
	return items, nil
}

func (tencentProvider) ListRecordLines(ctx context.Context, account Account, domainName, domainGrade string) ([]RecordLineItem, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	grade := strings.TrimSpace(domainGrade)
	if grade == "" {
		grade = lookupTencentDomainGrade(ctx, client, domainName)
	}
	if grade == "" {
		grade = "DP_FREE"
	}
	req := dnspod.NewDescribeRecordLineListRequest()
	req.Domain = &domainName
	req.DomainGrade = &grade
	resp, err := client.DescribeRecordLineListWithContext(ctx, req)
	if err != nil {
		return nil, tencentErr(err)
	}
	items := make([]RecordLineItem, 0)
	seen := make(map[string]bool)
	if resp.Response != nil {
		for _, item := range resp.Response.LineList {
			if item == nil {
				continue
			}
			name := stringValue(item.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			items = append(items, RecordLineItem{Name: name, LineID: stringValue(item.LineId)})
		}
	}
	if len(items) == 0 {
		return defaultRecordLines(), nil
	}
	return items, nil
}

func (tencentProvider) CreateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	ttl := uint64(input.TTL)
	mx := uint64(input.Priority)
	status := toTencentRecordStatus(input.Status)
	line := tencentLine(input.Line)
	req := dnspod.NewCreateRecordRequest()
	req.Domain = &input.DomainName
	req.SubDomain = &input.RR
	req.RecordType = &input.Type
	req.Value = &input.Value
	req.RecordLine = &line
	req.TTL = &ttl
	req.MX = &mx
	req.Status = &status
	req.Remark = &input.Remark
	resp, err := client.CreateRecordWithContext(ctx, req)
	if err != nil {
		return nil, tencentErr(err)
	}
	input.RecordID = strconv.FormatUint(uint64Value(resp.Response.RecordId), 10)
	return recordFromInput(input), nil
}

func (tencentProvider) UpdateRecord(ctx context.Context, account Account, input RecordInput) (*RecordItem, error) {
	client, err := newTencentClient(account)
	if err != nil {
		return nil, err
	}
	recordID, _ := strconv.ParseUint(input.RecordID, 10, 64)
	ttl := uint64(input.TTL)
	mx := uint64(input.Priority)
	status := toTencentRecordStatus(input.Status)
	line := tencentLine(input.Line)
	req := dnspod.NewModifyRecordRequest()
	req.Domain = &input.DomainName
	req.RecordId = &recordID
	req.SubDomain = &input.RR
	req.RecordType = &input.Type
	req.Value = &input.Value
	req.RecordLine = &line
	req.TTL = &ttl
	req.MX = &mx
	req.Status = &status
	req.Remark = &input.Remark
	if _, err := client.ModifyRecordWithContext(ctx, req); err != nil {
		return nil, tencentErr(err)
	}
	return recordFromInput(input), nil
}

func (tencentProvider) DeleteRecord(ctx context.Context, account Account, domainName, recordID string) error {
	client, err := newTencentClient(account)
	if err != nil {
		return err
	}
	id, _ := strconv.ParseUint(recordID, 10, 64)
	req := dnspod.NewDeleteRecordRequest()
	req.Domain = &domainName
	req.RecordId = &id
	if _, err := client.DeleteRecordWithContext(ctx, req); err != nil {
		return tencentErr(err)
	}
	return nil
}

func tencentStatus(status string) string {
	if strings.EqualFold(status, "ENABLE") {
		return "enabled"
	}
	return strings.ToLower(status)
}

func tencentRecordStatus(status string) string {
	if strings.EqualFold(status, "ENABLE") {
		return "enabled"
	}
	if strings.EqualFold(status, "DISABLE") {
		return "disabled"
	}
	return strings.ToLower(status)
}

func toTencentRecordStatus(status string) string {
	if status == "disabled" {
		return "DISABLE"
	}
	return "ENABLE"
}

func tencentLine(line string) string {
	if line == "" || line == "default" {
		return "默认"
	}
	return line
}

func lookupTencentDomainGrade(ctx context.Context, client *dnspod.Client, domainName string) string {
	limit := int64(100)
	req := dnspod.NewDescribeDomainListRequest()
	req.Limit = &limit
	resp, err := client.DescribeDomainListWithContext(ctx, req)
	if err != nil || resp.Response == nil {
		return ""
	}
	for _, item := range resp.Response.DomainList {
		if item == nil {
			continue
		}
		if strings.EqualFold(stringValue(item.Name), domainName) {
			return stringValue(item.Grade)
		}
	}
	return ""
}

func tencentErr(err error) error {
	if sdkErr, ok := err.(*tcerrors.TencentCloudSDKError); ok {
		return fmt.Errorf("%s：%s", sdkErr.Code, sdkErr.Message)
	}
	return err
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func uint64Value(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}
