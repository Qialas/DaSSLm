package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dasslm/server/internal/model"
	proxyservice "dasslm/server/internal/service/proxypool"
)

// 宝塔面板默认使用自签名证书：未开启时应校验失败，开启后应能正常访问
func TestSkipTLSVerifyAllowsSelfSignedPanel(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	strict := &http.Client{Timeout: 5 * time.Second}
	if _, err := strict.Get(server.URL); err == nil {
		t.Fatal("expected certificate verification error without skip")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	skipTLSVerify(client)
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("expected self-signed panel to be reachable, got %v", err)
	}
	resp.Body.Close()
}

// 走代理的账号开启后，仍保留代理配置，只额外关闭证书校验
func TestSkipTLSVerifyKeepsProxyTransport(t *testing.T) {
	client, err := proxyservice.Client(&model.ProxySetting{Name: "p", Protocol: "http", Host: "127.0.0.1", Port: 8080}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	skipTLSVerify(client)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("expected proxy transport to be kept")
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify to be enabled")
	}
}

// 宝塔账号自动忽略自签名证书，其他部署服务保持证书校验
func TestDeployHTTPClientSkipsVerifyOnlyForBTPanel(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	bt, err := DeployHTTPClient(nil, model.DeployAccount{Provider: "btpanel"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := bt.Get(server.URL)
	if err != nil {
		t.Fatalf("expected BT panel client to accept self-signed certificate, got %v", err)
	}
	resp.Body.Close()

	tencent, err := DeployHTTPClient(nil, model.DeployAccount{Provider: "tencentcloud"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tencent.Get(server.URL); err == nil {
		t.Fatal("expected tencentcloud client to keep certificate verification")
	}
}
