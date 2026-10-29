package certificate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dasslm/server/internal/model"
)

// 证书部署使用的客户端必须和“测试连接”一致：宝塔面板的自签名证书不能导致部署失败，
// 其他服务商仍然校验证书。
func TestDeployHTTPClientAcceptsBTPanelSelfSignedCert(t *testing.T) {
	panel := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer panel.Close()

	for provider, wantOK := range map[string]bool{"btpanel": true, "baota": true, "tencent_cdn": false} {
		client, err := deployHTTPClient(nil, model.DeployAccount{Provider: provider})
		if err != nil {
			t.Fatalf("%s: create client: %v", provider, err)
		}
		resp, err := client.Post(panel.URL+"/site", "application/x-www-form-urlencoded", strings.NewReader("a=1"))
		if resp != nil {
			resp.Body.Close()
		}
		if wantOK && err != nil {
			t.Fatalf("%s: self-signed panel should be accepted, got %v", provider, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("%s: self-signed certificate should still be rejected", provider)
		}
	}
}
