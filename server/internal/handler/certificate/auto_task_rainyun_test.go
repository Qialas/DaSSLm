package certificate

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 雨云“新建证书”目标：首次部署上传后回写证书ID，之后的部署（续期）替换同一张证书，不再重复上传。
func TestAutoDeployRainyunTargetSavesNewCertificateIDAndUpdatesItNextTime(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AutoTask{}, &model.AutoTaskTarget{}, &model.DeployAccount{}, &model.Certificate{},
		&model.CertificateDeployLog{}, &model.SystemLog{}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var calls []string
	var records []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") != "rainyun-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"message":"API密钥无效"}`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"Records": records, "TotalRecords": len(records)}})
		case http.MethodPost:
			var body struct{ Cert string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			block, _ := pem.Decode([]byte(body.Cert))
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Errorf("uploaded certificate is invalid: %v", err)
			}
			records = append(records, map[string]any{
				"ID": 500 + len(records), "Domain": strings.Join(cert.DNSNames, ", "),
				"StartDate": cert.NotBefore.Unix(), "ExpDate": cert.NotAfter.Unix(),
			})
			_, _ = w.Write([]byte(`{"code":200,"data":"ok"}`))
		case http.MethodPut:
			_, _ = w.Write([]byte(`{"code":200,"data":"ok"}`))
		}
	}))
	defer server.Close()

	certPEM, keyPEM, _ := testCertificatePair(t, 301, time.Now().Add(60*24*time.Hour))
	cert := model.Certificate{CommonName: "renewal-test.example.com", CertPEM: certPEM, KeyPEM: keyPEM, Status: certStatusIssued}
	account := model.DeployAccount{Name: "雨云", Provider: "rainyun", Endpoint: server.URL, AccessKey: "rainyun-key", Status: model.StatusEnabled}
	if err := db.Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}

	h := NewAutoTaskHandler(db)
	if _, _, err := h.buildAutoDeployTask(autoTaskRequest{CertificateID: cert.ID, Targets: []autoTaskTargetRequest{
		{DeployAccountID: account.ID, SiteName: cert.CommonName, ResourceID: "abc"},
	}}, nil); err == nil {
		t.Fatal("non-numeric Rainyun certificate ID should be rejected")
	}
	task, targets, err := h.buildAutoDeployTask(autoTaskRequest{CertificateID: cert.ID, Targets: []autoTaskTargetRequest{
		{DeployAccountID: account.ID, SiteName: cert.CommonName, ProductType: "cdn", Region: "x", ResourceName: "leftover"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if targets[0].ResourceID != "" || targets[0].ResourceName != "" || targets[0].ProductType != "" || targets[0].Region != "" {
		t.Fatalf("new Rainyun target should be stored without resource fields, got %+v", targets[0])
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	targets[0].TaskID = task.ID
	if err := db.Create(&targets[0]).Error; err != nil {
		t.Fatal(err)
	}

	status, message := h.runAutoDeployTarget(context.Background(), task, targets[0], cert)
	if status != "success" || !strings.Contains(message, "500") {
		t.Fatalf("first deploy: status=%s message=%s", status, message)
	}
	var saved model.AutoTaskTarget
	if err := db.First(&saved, targets[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ResourceID != "500" || saved.ResourceName != "renewal-test.example.com" || saved.LastRunStatus != "success" {
		t.Fatalf("new certificate ID was not saved on the target: %+v", saved)
	}

	status, message = h.runAutoDeployTarget(context.Background(), task, saved, cert)
	if status != "success" {
		t.Fatalf("second deploy failed: %s", message)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /product/sslcenter/", "POST /product/sslcenter/", "GET /product/sslcenter/", "PUT /product/sslcenter/500"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected Rainyun calls:\n got %v\nwant %v", calls, want)
	}
	var logs []model.CertificateDeployLog
	db.Order("id").Find(&logs)
	if len(logs) != 2 || logs[0].Status != "success" || strings.Contains(logs[0].RequestBody, "PRIVATE KEY") {
		t.Fatalf("unexpected deploy logs: %+v", logs)
	}
}
