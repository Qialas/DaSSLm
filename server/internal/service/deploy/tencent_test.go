package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"dasslm/server/internal/model"
)

func TestTencentDeployInstanceFormats(t *testing.T) {
	cases := []struct {
		name                      string
		target                    Target
		product, instance, region string
		wantErr                   bool
	}{
		{name: "仅上传", target: Target{ProductType: "ssl", SiteName: "a.com"}, product: "ssl"},
		{name: "CDN 自动补 on", target: Target{ProductType: "cdn", ResourceID: "cdn.a.com"}, product: "cdn", instance: "cdn.a.com|on"},
		{name: "CDN 忽略地域", target: Target{ProductType: "cdn", ResourceID: "cdn.a.com|on", Region: "ap-guangzhou"}, product: "cdn", instance: "cdn.a.com|on"},
		{name: "旧 eo 映射为 teo", target: Target{ProductType: "eo", SiteName: "eo.a.com"}, product: "teo", instance: "eo.a.com"},
		{name: "WAF 需要地域", target: Target{ProductType: "waf", ResourceID: "waf.a.com"}, wantErr: true},
		{name: "WAF", target: Target{ProductType: "waf", ResourceID: "waf.a.com", Region: "ap-guangzhou"}, product: "waf", instance: "waf.a.com", region: "ap-guangzhou"},
		{name: "CLB 无 SNI", target: Target{ProductType: "clb", ResourceID: "lb-1|lbl-1", Region: "ap-shanghai"}, product: "clb", instance: "lb-1|lbl-1", region: "ap-shanghai"},
		{name: "CLB SNI", target: Target{ProductType: "clb", ResourceID: "lb-1|lbl-1|a.com", Region: "ap-shanghai"}, product: "clb", instance: "lb-1|lbl-1|a.com", region: "ap-shanghai"},
		{name: "CLB 缺监听器", target: Target{ProductType: "clb", ResourceID: "lb-1", Region: "ap-shanghai"}, wantErr: true},
		{name: "API 网关", target: Target{ProductType: "apigateway", ResourceID: "service-1|api.a.com", Region: "ap-beijing"}, product: "apigateway", instance: "service-1|api.a.com", region: "ap-beijing"},
		{name: "COS", target: Target{ProductType: "cos", ResourceID: "ap-hongkong|bucket-1250000000|cdn.a.com", Region: "ap-hongkong"}, product: "cos", instance: "ap-hongkong|bucket-1250000000|cdn.a.com", region: "ap-hongkong"},
		{name: "COS 地域不一致", target: Target{ProductType: "cos", ResourceID: "ap-hongkong|bucket|a.com", Region: "ap-beijing"}, wantErr: true},
		{name: "未知产品", target: Target{ProductType: "vpc", ResourceID: "x"}, wantErr: true},
	}
	for _, c := range cases {
		product, instance, region, err := TencentDeployInstance(c.target)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if product != c.product || instance != c.instance || region != c.region {
			t.Errorf("%s: got (%s, %s, %s), want (%s, %s, %s)", c.name, product, instance, region, c.product, c.instance, c.region)
		}
	}
}

type captureTransport struct {
	requests []*http.Request
	bodies   []string
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	c.requests = append(c.requests, r)
	c.bodies = append(c.bodies, string(body))
	resp := `{"Response":{"CertificateId":"cert-1","DeployRecordId":1,"DeployStatus":1,"RequestId":"r"}}`
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(resp)), Header: http.Header{}}, nil
}

// 地域按官方要求作为公共参数通过 X-TC-Region 请求头传递，不放在请求体里
func TestTencentDeploySendsRegionAsHeader(t *testing.T) {
	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	account := model.DeployAccount{Provider: "tencentcloud", AccessKey: "id", SecretKey: "key"}
	target := Target{SiteName: "a.com", ProductType: "clb", ResourceID: "lb-1|lbl-1", Region: "ap-shanghai"}
	if _, err := DeployCertificateToTarget(context.Background(), account, client, target, "CERT", "KEY"); err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("expected upload + deploy requests, got %d", len(transport.requests))
	}
	upload, deploy := transport.requests[0], transport.requests[1]
	if upload.Header.Get("X-TC-Region") != "" {
		t.Fatal("UploadCertificate should not carry a region")
	}
	if deploy.Header.Get("X-TC-Action") != "DeployCertificateInstance" || deploy.Header.Get("X-TC-Region") != "ap-shanghai" {
		t.Fatalf("unexpected deploy headers: %v", deploy.Header)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(transport.bodies[1]), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["Region"]; ok {
		t.Fatal("Region must not be sent in the request body")
	}
	if body["ResourceType"] != "clb" || body["InstanceIdList"].([]any)[0] != "lb-1|lbl-1" {
		t.Fatalf("unexpected deploy body: %v", body)
	}

	// 不需要地域的产品不带 X-TC-Region
	transport.requests, transport.bodies = nil, nil
	target = Target{SiteName: "a.com", ProductType: "cdn", ResourceID: "a.com", Region: "ap-shanghai"}
	if _, err := DeployCertificateToTarget(context.Background(), account, client, target, "CERT", "KEY"); err != nil {
		t.Fatal(err)
	}
	if transport.requests[1].Header.Get("X-TC-Region") != "" {
		t.Fatal("CDN deploy should not carry a region")
	}
}
