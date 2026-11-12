package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dasslm/server/internal/model"
)

const rainyunTestKey = "rainyun-test-api-key"

type fakeRainyunRecord struct {
	ID        int64
	Domain    string
	StartDate int64
	ExpDate   int64
	Cert      string
	Key       string
}

// fakeRainyun 模拟雨云「我的证书」接口：列表、上传、替换。
type fakeRainyun struct {
	mu      sync.Mutex
	records []fakeRainyunRecord
	nextID  int64
	calls   []string
	// afterUpload 在上传成功后调用，可模拟同一账号下的并发上传
	afterUpload func(f *fakeRainyun)
	// startShift 上传时给生效时间加的偏移，模拟雨云记录的时间与证书不完全一致
	startShift int64
}

func newFakeRainyun(t *testing.T) (*fakeRainyun, *httptest.Server) {
	t.Helper()
	fake := &fakeRainyun{nextID: 100}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeRainyun) add(domain string, start, exp int64) int64 {
	f.nextID++
	f.records = append(f.records, fakeRainyunRecord{ID: f.nextID, Domain: domain, StartDate: start, ExpDate: exp})
	return f.nextID
}

func (f *fakeRainyun) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	reply := func(status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	if r.Header.Get("X-Api-Key") != rainyunTestKey {
		reply(http.StatusUnauthorized, map[string]any{"code": 401, "message": "API密钥无效"})
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/product/sslcenter/":
		var options struct {
			Page    int `json:"page"`
			PerPage int `json:"perPage"`
		}
		if err := json.Unmarshal([]byte(r.URL.Query().Get("options")), &options); err != nil || options.Page < 1 || options.PerPage < 1 {
			reply(http.StatusBadRequest, map[string]any{"code": 400, "message": "options 参数错误"})
			return
		}
		records := []map[string]any{}
		for i := (options.Page - 1) * options.PerPage; i < len(f.records) && len(records) < options.PerPage; i++ {
			item := f.records[i]
			records = append(records, map[string]any{
				"ID": item.ID, "Domain": item.Domain, "Issuer": "Test CA", "StartDate": item.StartDate, "ExpDate": item.ExpDate,
				"UploadTime": item.StartDate, "BindDomains": []string{}, "UID": 1,
			})
		}
		reply(http.StatusOK, map[string]any{"code": 200, "data": map[string]any{"Records": records, "TotalRecords": len(f.records)}})
	case r.Method == http.MethodPost && r.URL.Path == "/product/sslcenter/":
		var body struct{ Cert, Key string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		cert, err := firstCertificateFromPEM(body.Cert)
		if err != nil || !strings.Contains(body.Key, "PRIVATE KEY") {
			reply(http.StatusBadRequest, map[string]any{"code": 400, "message": "证书或私钥格式错误"})
			return
		}
		id := f.add(strings.Join(cert.DNSNames, ", "), cert.NotBefore.Unix()+f.startShift, cert.NotAfter.Unix())
		f.records[len(f.records)-1].Cert, f.records[len(f.records)-1].Key = body.Cert, body.Key
		_ = id
		if f.afterUpload != nil {
			f.afterUpload(f)
		}
		reply(http.StatusOK, map[string]any{"code": 200, "data": "ok"})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/product/sslcenter/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/product/sslcenter/"), 10, 64)
		var body struct{ Cert, Key string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for i := range f.records {
			if f.records[i].ID == id {
				f.records[i].Cert, f.records[i].Key = body.Cert, body.Key
				reply(http.StatusOK, map[string]any{"code": 200, "data": "ok"})
				return
			}
		}
		reply(http.StatusBadRequest, map[string]any{"code": 400, "message": "证书不存在"})
	default:
		reply(http.StatusNotFound, map[string]any{"code": 404, "message": "not found"})
	}
}

func (f *fakeRainyun) callCount(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			count++
		}
	}
	return count
}

func rainyunAccount(server *httptest.Server) model.DeployAccount {
	return model.DeployAccount{Provider: RainyunProvider, Endpoint: server.URL + "/", AccessKey: rainyunTestKey}
}

func rainyunTestCertificate(t *testing.T, names ...string) (string, string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    time.Now().Add(-time.Hour).Truncate(time.Second),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second),
		DNSNames:     names,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, cert
}

func TestNormalizeRainyunCertificateID(t *testing.T) {
	cases := map[string]struct {
		want    string
		wantErr bool
	}{
		"":       {want: ""},
		"  ":     {want: ""},
		"123":    {want: "123"},
		" #45 ":  {want: "45"},
		"0":      {wantErr: true},
		"-1":     {wantErr: true},
		"abc":    {wantErr: true},
		"12;3":   {wantErr: true},
		"1=1":    {wantErr: true},
		"123abc": {wantErr: true},
	}
	for input, c := range cases {
		got, err := NormalizeRainyunCertificateID(input)
		if c.wantErr != (err != nil) || got != c.want {
			t.Errorf("NormalizeRainyunCertificateID(%q) = %q, %v; want %q, err=%v", input, got, err, c.want, c.wantErr)
		}
	}
}

func TestListRainyunCertificatesPaginatesFiltersAndSorts(t *testing.T) {
	fake, server := newFakeRainyun(t)
	now := time.Now().Unix()
	for i := 0; i < 150; i++ {
		fake.add(fmt.Sprintf("site%03d.example.com", i), now-3600, now+86400)
	}
	expiredID := fake.add("old.example.net, www.old.example.net", now-90*86400, now-86400)

	items, total, _, err := ListRainyunCertificates(context.Background(), rainyunAccount(server), server.Client(), 1, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 151 || len(items) != 20 {
		t.Fatalf("total=%d len=%d, want 151/20", total, len(items))
	}
	if items[0].ID != expiredID || !items[0].Expired || len(items[0].Domains) != 2 || items[0].Domains[1] != "www.old.example.net" {
		t.Fatalf("first item should be the newest (expired) certificate, got %+v", items[0])
	}
	if items[1].Expired || items[1].ExpireAt == nil || items[1].ExpireAt.Unix() != now+86400 {
		t.Fatalf("unexpected second item %+v", items[1])
	}
	if got := fake.callCount("GET /product/sslcenter/"); got != 2 {
		t.Fatalf("expected 2 list pages to be fetched, got %d", got)
	}

	items, total, _, err = ListRainyunCertificates(context.Background(), rainyunAccount(server), server.Client(), 1, 20, "SITE14")
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || items[0].Domain != "site149.example.com" {
		t.Fatalf("keyword filter: total=%d first=%v", total, items)
	}
	items, total, _, err = ListRainyunCertificates(context.Background(), rainyunAccount(server), server.Client(), 1, 20, strconv.FormatInt(expiredID, 10))
	if err != nil || total != 1 || items[0].ID != expiredID {
		t.Fatalf("ID filter: total=%d items=%v err=%v", total, items, err)
	}
}

func TestDeployRainyunCreatesCertificateAndReturnsNewID(t *testing.T) {
	fake, server := newFakeRainyun(t)
	now := time.Now().Unix()
	fake.add("other.example.org", now-3600, now+86400)
	// 上传后同一账号又出现一张更新的证书（例如别人同时上传），不能误认成我们的证书
	fake.afterUpload = func(f *fakeRainyun) { f.add("concurrent.example.org", now, now+86400) }
	certPEM, keyPEM, _ := rainyunTestCertificate(t, "example.com", "www.example.com")

	trace, result, err := DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{SiteName: "example.com"}, certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceID != "102" || result.ResourceName != "example.com, www.example.com" || !strings.Contains(result.Message, "102") {
		t.Fatalf("unexpected result %+v", result)
	}
	if got := fake.callCount("POST"); got != 1 {
		t.Fatalf("expected one upload, got %d", got)
	}
	if fake.records[1].Cert != strings.TrimSpace(certPEM) || fake.records[1].Key != strings.TrimSpace(keyPEM) {
		t.Fatal("uploaded certificate content mismatch")
	}
	if trace == nil || trace.RequestMethod != "GET+POST+GET" {
		t.Fatalf("expected combined trace, got %+v", trace)
	}
	for _, text := range []string{trace.RequestBody, trace.RequestHeaders} {
		if strings.Contains(text, "PRIVATE KEY") || strings.Contains(text, "BEGIN CERTIFICATE") || strings.Contains(text, rainyunTestKey) {
			t.Fatalf("trace leaks secrets: %s", text)
		}
	}
}

func TestDeployRainyunReusesIdenticalCertificateInsteadOfUploadingAgain(t *testing.T) {
	fake, server := newFakeRainyun(t)
	certPEM, keyPEM, cert := rainyunTestCertificate(t, "example.com")
	existing := fake.add("example.com", cert.NotBefore.Unix(), cert.NotAfter.Unix())
	fake.add("example.com", cert.NotBefore.Unix()-86400, cert.NotAfter.Unix()-86400) // 同域名的旧证书

	_, result, err := DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{}, certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceID != strconv.FormatInt(existing, 10) {
		t.Fatalf("expected to reuse certificate %d, got %+v", existing, result)
	}
	if got := fake.callCount("POST"); got != 0 {
		t.Fatalf("identical certificate should not be uploaded again, got %d uploads", got)
	}
}

func TestDeployRainyunFallsBackToNewCertificateWithSameDomain(t *testing.T) {
	fake, server := newFakeRainyun(t)
	fake.startShift = 8 * 3600 // 雨云记录的时间与证书不一致时，按新出现的同域名证书识别
	certPEM, keyPEM, _ := rainyunTestCertificate(t, "shift.example.com")
	_, result, err := DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{}, certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceID != "101" {
		t.Fatalf("expected new certificate 101, got %+v", result)
	}
}

func TestDeployRainyunUpdatesSelectedCertificate(t *testing.T) {
	fake, server := newFakeRainyun(t)
	id := fake.add("example.com", 1, 2)
	certPEM, keyPEM, _ := rainyunTestCertificate(t, "example.com")

	trace, result, err := DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{ResourceID: strconv.FormatInt(id, 10)}, certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceID != strconv.FormatInt(id, 10) {
		t.Fatalf("certificate ID should stay the same, got %+v", result)
	}
	if fake.calls[0] != fmt.Sprintf("PUT /product/sslcenter/%d", id) || len(fake.calls) != 1 {
		t.Fatalf("expected a single PUT, got %v", fake.calls)
	}
	if fake.records[0].Cert != strings.TrimSpace(certPEM) {
		t.Fatal("certificate was not replaced")
	}
	if trace.RequestMethod != http.MethodPut || strings.Contains(trace.RequestBody, "PRIVATE KEY") {
		t.Fatalf("unexpected trace %+v", trace)
	}
}

func TestDeployRainyunReportsMissingCertificateAndBadKey(t *testing.T) {
	_, server := newFakeRainyun(t)
	certPEM, keyPEM, _ := rainyunTestCertificate(t, "example.com")

	_, _, err := DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{ResourceID: "999"}, certPEM, keyPEM)
	if err == nil || !strings.Contains(err.Error(), "证书不存在") || !strings.Contains(err.Error(), "重新选择") {
		t.Fatalf("expected missing certificate error, got %v", err)
	}
	_, _, err = DeployCertificateWithResult(context.Background(), rainyunAccount(server), server.Client(), Target{ResourceID: "abc"}, certPEM, keyPEM)
	if err == nil || !strings.Contains(err.Error(), "应为数字") {
		t.Fatalf("expected invalid ID error, got %v", err)
	}

	account := rainyunAccount(server)
	account.AccessKey = "wrong-key"
	if _, _, err := TestRainyun(context.Background(), account, server.Client()); err == nil || !strings.Contains(err.Error(), "API密钥无效") {
		t.Fatalf("expected API key error, got %v", err)
	}
	account.AccessKey = ""
	if _, _, err := TestRainyun(context.Background(), account, server.Client()); err == nil {
		t.Fatal("expected error for empty API key")
	}
	if total, _, err := TestRainyun(context.Background(), rainyunAccount(server), server.Client()); err != nil || total != 0 {
		t.Fatalf("TestRainyun = %d, %v", total, err)
	}
}
