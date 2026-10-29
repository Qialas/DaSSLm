package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"dasslm/server/internal/model"
)

// 普通网站走 /site SetSSL（siteName），Docker 网站走 /mod/docker/com/set_ssl（site_name）
func TestBTPanelDeployRoutesByTargetType(t *testing.T) {
	var gotPath string
	var gotForm url.Values
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotPath, gotForm = r.URL.Path, r.PostForm
		_, _ = w.Write([]byte(`{"status":true,"msg":"证书已保存!"}`))
	}))
	defer panel.Close()
	account := model.DeployAccount{Provider: "btpanel", Endpoint: panel.URL, AccessKey: "key"}

	cases := []struct {
		productType, path, nameField string
	}{
		{"", "/site", "siteName"},
		{"site", "/site", "siteName"},
		{"docker", "/mod/docker/com/set_ssl", "site_name"},
	}
	for _, c := range cases {
		target := Target{SiteName: "app.example.com", ProductType: c.productType}
		if _, err := DeployCertificateToTarget(context.Background(), account, panel.Client(), target, "CERT", "KEY"); err != nil {
			t.Fatalf("%q: deploy failed: %v", c.productType, err)
		}
		if gotPath != c.path {
			t.Fatalf("%q: expected path %s, got %s", c.productType, c.path, gotPath)
		}
		if gotForm.Get(c.nameField) != "app.example.com" || gotForm.Get("key") != "KEY" || gotForm.Get("csr") != "CERT" {
			t.Fatalf("%q: unexpected form %v", c.productType, gotForm)
		}
		if gotForm.Get("request_token") == "" || gotForm.Get("request_time") == "" {
			t.Fatalf("%q: request must be signed", c.productType)
		}
		if c.path == "/site" && gotForm.Get("action") != "SetSSL" {
			t.Fatalf("%q: expected action=SetSSL", c.productType)
		}
	}

	if _, err := DeployCertificateToTarget(context.Background(), account, panel.Client(), Target{SiteName: "a", ProductType: "mail"}, "CERT", "KEY"); err == nil {
		t.Fatal("unknown BT target type should be rejected")
	}
}

func TestBTPanelDockerDeployReportsPanelError(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":false,"msg":"网站不存在"}`))
	}))
	defer panel.Close()
	account := model.DeployAccount{Provider: "btpanel", Endpoint: panel.URL, AccessKey: "key"}
	_, err := DeployCertificateToTarget(context.Background(), account, panel.Client(), Target{SiteName: "x", ProductType: "docker"}, "CERT", "KEY")
	if err == nil {
		t.Fatal("expected panel failure to surface as error")
	}
}
