package certificate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dasslm/server/internal/model"

	"golang.org/x/crypto/acme"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFlowTestHandler(t *testing.T) *CertificateHandler {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Certificate{}, &model.SSLAccount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &CertificateHandler{db: db, contentSyncInFlight: map[uint]*certificateWork{}, progressLogAt: map[string]time.Time{}}
}

func TestWaitACMEAuthorizationsReturnsImmediatelyWhenAllAuthorizationsReused(t *testing.T) {
	handler := newFlowTestHandler(t)
	row := &model.Certificate{CommonName: "example.com", Status: certStatusSubmitted}
	done := make(chan error, 1)
	go func() {
		// CA 复用了已有效的授权时没有验证记录，不能再轮询等待
		done <- handler.waitACMEAuthorizations(context.Background(), row, &model.SSLAccount{Provider: "letsencrypt"}, nil)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitACMEAuthorizations returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitACMEAuthorizations blocked although there were no pending authorizations")
	}
}

func TestSaveACMECertificateChainSwapsPendingKeyTogetherWithCertificate(t *testing.T) {
	handler := newFlowTestHandler(t)
	oldCertPEM, oldKeyPEM, oldExpiresAt := testCertificatePair(t, 201, time.Now().Add(10*24*time.Hour))
	newCertPEM, newKeyPEM, newExpiresAt := testCertificatePair(t, 202, time.Now().Add(90*24*time.Hour))
	nextRetry := time.Now().Add(time.Hour)
	row := model.Certificate{
		CommonName:    "renewal-test.example.com",
		CertPEM:       oldCertPEM,
		KeyPEM:        oldKeyPEM,
		PendingKeyPEM: newKeyPEM,
		ExpiresAt:     &oldExpiresAt,
		Status:        certStatusSubmitted,
		RetryCount:    2,
		NextRetryAt:   &nextRetry,
	}
	if err := handler.db.Create(&row).Error; err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	der, err := firstPEMCertificateDER(newCertPEM)
	if err != nil {
		t.Fatalf("decode new certificate: %v", err)
	}
	if err := handler.saveACMECertificateChain(&row, "https://acme.test/cert/202", [][]byte{der}); err != nil {
		t.Fatalf("saveACMECertificateChain: %v", err)
	}
	if err := handler.db.First(&row, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Status != certStatusIssued || row.PendingKeyPEM != "" || row.RetryCount != 0 || row.NextRetryAt != nil {
		t.Fatalf("unexpected state after issue: status=%s pending=%q retry=%d next=%v", row.Status, row.PendingKeyPEM, row.RetryCount, row.NextRetryAt)
	}
	if _, err := parseCertificateWithMatchingKey(row.CertPEM, row.KeyPEM); err != nil {
		t.Fatalf("saved certificate and key do not match: %v", err)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.Equal(newExpiresAt) {
		t.Fatalf("expires_at = %v, want %v", row.ExpiresAt, newExpiresAt)
	}
}

func TestSaveACMECertificateChainRejectsCertificateWithoutMatchingKey(t *testing.T) {
	handler := newFlowTestHandler(t)
	oldCertPEM, oldKeyPEM, _ := testCertificatePair(t, 211, time.Now().Add(10*24*time.Hour))
	otherCertPEM, _, _ := testCertificatePair(t, 212, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{CommonName: "renewal-test.example.com", CertPEM: oldCertPEM, KeyPEM: oldKeyPEM, Status: certStatusSubmitted}
	if err := handler.db.Create(&row).Error; err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	der, _ := firstPEMCertificateDER(otherCertPEM)
	if err := handler.saveACMECertificateChain(&row, "https://acme.test/cert/212", [][]byte{der}); err == nil {
		t.Fatal("expected mismatch error")
	}
	_ = handler.db.First(&row, row.ID).Error
	if row.CertPEM != oldCertPEM || row.KeyPEM != oldKeyPEM {
		t.Fatal("old certificate content must stay untouched when the new certificate does not match any key")
	}
}

func TestMarkCertificateFailedKeepsOldCertificateAndSchedulesRetry(t *testing.T) {
	handler := newFlowTestHandler(t)
	certPEM, keyPEM, _ := testCertificatePair(t, 221, time.Now().Add(5*24*time.Hour))
	_, pendingKeyPEM, _ := testCertificatePair(t, 222, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{CommonName: "renewal-test.example.com", CertPEM: certPEM, KeyPEM: keyPEM, PendingKeyPEM: pendingKeyPEM, Status: certStatusSubmitted}
	if err := handler.db.Create(&row).Error; err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	before := time.Now()
	handler.markCertificateFailed(&row, model.SSLAccount{}, "ACME订单已失效", nil)
	if err := handler.db.First(&row, row.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Status != certStatusFailed || row.RetryCount != 1 || row.NextRetryAt == nil {
		t.Fatalf("status=%s retry=%d next=%v", row.Status, row.RetryCount, row.NextRetryAt)
	}
	if row.NextRetryAt.Before(before.Add(certRetryBaseDelay - time.Minute)) {
		t.Fatalf("next retry %v is earlier than the backoff", row.NextRetryAt)
	}
	if row.PendingKeyPEM != "" {
		t.Fatal("pending key should be discarded after failure")
	}
	if row.CertPEM != certPEM || row.KeyPEM != keyPEM {
		t.Fatal("old certificate content must be kept after a failed renewal")
	}
	if !certificateDeployable(row) {
		t.Fatal("still-valid old certificate should remain deployable after a failed renewal")
	}

	row.RetryCount = certRetryMaxCount
	handler.markCertificateFailed(&row, model.SSLAccount{}, "仍然失败", nil)
	_ = handler.db.First(&row, row.ID).Error
	if row.NextRetryAt != nil {
		t.Fatalf("no automatic retry expected after %d attempts, got %v", certRetryMaxCount, row.NextRetryAt)
	}
}

func TestRequeueCertificateApplicationClearsOrderButKeepsCertificate(t *testing.T) {
	handler := newFlowTestHandler(t)
	certPEM, keyPEM, _ := testCertificatePair(t, 231, time.Now().Add(5*24*time.Hour))
	next := time.Now().Add(time.Hour)
	row := model.Certificate{
		CommonName:      "renewal-test.example.com",
		CertPEM:         certPEM,
		KeyPEM:          keyPEM,
		PendingKeyPEM:   "pending",
		ProviderOrderID: "https://acme.test/order/1",
		ProviderStatus:  "pending",
		Status:          certStatusSubmitted,
		RetryCount:      3,
		NextRetryAt:     &next,
	}
	if err := handler.db.Create(&row).Error; err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	if err := handler.requeueCertificateApplication(context.Background(), &row, model.SSLAccount{Provider: "litessl"}, "重新申请", true); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	_ = handler.db.First(&row, row.ID).Error
	if row.Status != certStatusQueued || row.ProviderOrderID != "" || row.PendingKeyPEM != "" || row.RetryCount != 0 || row.NextRetryAt != nil {
		t.Fatalf("unexpected requeue state: %+v", row)
	}
	if row.CertPEM != certPEM || row.KeyPEM != keyPEM {
		t.Fatal("requeue must keep the current certificate content")
	}
}

func TestTakeoverCertificateWorkCancelsRunningWork(t *testing.T) {
	handler := newFlowTestHandler(t)
	ctx, ok := handler.claimCertificateWork(7)
	if !ok {
		t.Fatal("claim failed")
	}
	exited := make(chan struct{})
	go func() {
		<-ctx.Done()
		handler.releaseCertificateContentWork(7)
		close(exited)
	}()
	_, release, ok := handler.takeoverCertificateWork(7, 2*time.Second)
	if !ok {
		t.Fatal("takeover failed")
	}
	<-exited
	if handler.claimCertificateContentWork(7) {
		t.Fatal("work should be held by the takeover caller")
	}
	release()
	if !handler.claimCertificateContentWork(7) {
		t.Fatal("work should be claimable after release")
	}
}

func TestTakeoverCertificateWorkTimesOut(t *testing.T) {
	handler := newFlowTestHandler(t)
	if _, ok := handler.claimCertificateWork(8); !ok {
		t.Fatal("claim failed")
	}
	// 任务不响应取消时应超时返回，而不是无限等待
	if _, _, ok := handler.takeoverCertificateWork(8, 100*time.Millisecond); ok {
		t.Fatal("takeover should fail while the running work does not exit")
	}
}

func TestIsACMEOrderUnusable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&acme.Error{StatusCode: http.StatusNotFound, ProblemType: "urn:ietf:params:acme:error:malformed"}, true},
		{fmt.Errorf("wrapped: %w", &acme.Error{StatusCode: http.StatusNotFound}), true},
		{&acme.Error{StatusCode: http.StatusForbidden, ProblemType: "urn:ietf:params:acme:error:accountDoesNotExist"}, true},
		{&acme.Error{StatusCode: http.StatusServiceUnavailable}, false},
		{errors.New("connection reset"), false},
	}
	for _, item := range cases {
		if got := isACMEOrderUnusable(item.err); got != item.want {
			t.Fatalf("isACMEOrderUnusable(%v) = %v, want %v", item.err, got, item.want)
		}
	}
}

func TestACMEOrderExpired(t *testing.T) {
	if !acmeOrderExpired(&acme.Order{Status: acme.StatusPending, Expires: time.Now().Add(-time.Minute)}) {
		t.Fatal("pending order past its expiry should be expired")
	}
	if acmeOrderExpired(&acme.Order{Status: acme.StatusPending, Expires: time.Now().Add(time.Hour)}) {
		t.Fatal("order not yet expired")
	}
	if acmeOrderExpired(&acme.Order{Status: acme.StatusPending}) {
		t.Fatal("order without expiry should not be treated as expired")
	}
}

func TestValidateCertificateNames(t *testing.T) {
	ok := [][]string{
		{"example.com"},
		{"example.com", "*.example.com"},
		{"a.b.example.com", "*.b.example.com"},
	}
	for _, names := range ok {
		if err := validateCertificateNames("example.com", "letsencrypt", names); err != nil {
			t.Fatalf("validateCertificateNames(%v) = %v", names, err)
		}
	}
	bad := [][]string{
		{"example.com", "other.com"},
		{"badexample.com"},
		{"*.*.example.com"},
		{"a b.example.com"},
	}
	for _, names := range bad {
		if err := validateCertificateNames("example.com", "letsencrypt", names); err == nil {
			t.Fatalf("validateCertificateNames(%v) should fail", names)
		}
	}
	if err := validateCertificateNames("example.com", "tencent_free", []string{"*.example.com"}); err == nil {
		t.Fatal("tencent free certificate must reject wildcard")
	}
	if err := validateCertificateNames("example.com", "tencent_free", []string{"example.com", "www.example.com"}); err == nil {
		t.Fatal("tencent free certificate must reject multiple names")
	}
	if err := validateCertificateNames("example.com", "tencent_free", []string{"www.example.com"}); err != nil {
		t.Fatalf("tencent single name should pass: %v", err)
	}
}

func TestCertificateNamesKeyIgnoresOrderAndFormat(t *testing.T) {
	a := certificateNamesKey("example.com", `["*.example.com","www.example.com"]`)
	b := certificateNamesKey("Example.com.", "www.example.com, *.EXAMPLE.com")
	if a != b {
		t.Fatalf("names key mismatch: %q vs %q", a, b)
	}
}

func TestCertificateRetryDelay(t *testing.T) {
	want := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for i, expected := range want {
		if got := certificateRetryDelay(i + 1); got != expected {
			t.Fatalf("certificateRetryDelay(%d) = %v, want %v", i+1, got, expected)
		}
	}
}

func TestACMEAccountIdentityChanged(t *testing.T) {
	base := model.SSLAccount{Provider: "letsencrypt", Email: "a@example.com"}
	same := base
	same.Email = "b@example.com"
	if acmeAccountIdentityChanged(base, same) {
		t.Fatal("changing the contact email should keep the ACME account")
	}
	staging := base
	staging.DirectoryURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	if !acmeAccountIdentityChanged(base, staging) {
		t.Fatal("changing the directory URL must reset the ACME account")
	}
	defaultDirectory := base
	defaultDirectory.DirectoryURL = acme.LetsEncryptURL + "/"
	if acmeAccountIdentityChanged(base, defaultDirectory) {
		t.Fatal("explicit default directory URL is the same CA")
	}
	eab := base
	eab.EABKid = "kid"
	if !acmeAccountIdentityChanged(base, eab) {
		t.Fatal("changing EAB must reset the ACME account")
	}
}

func TestCertificateDeployable(t *testing.T) {
	certPEM, keyPEM, _ := testCertificatePair(t, 241, time.Now().Add(24*time.Hour))
	expiredCertPEM, expiredKeyPEM, _ := testCertificatePair(t, 242, time.Now().Add(-time.Hour))
	cases := []struct {
		row  model.Certificate
		want bool
	}{
		{model.Certificate{Status: certStatusIssued}, true},
		{model.Certificate{Status: certStatusQueued, CertPEM: certPEM, KeyPEM: keyPEM}, true},
		{model.Certificate{Status: certStatusFailed, CertPEM: expiredCertPEM, KeyPEM: expiredKeyPEM}, false},
		{model.Certificate{Status: certStatusRevoked, CertPEM: certPEM, KeyPEM: keyPEM}, false},
		{model.Certificate{Status: certStatusFailed}, false},
	}
	for i, item := range cases {
		if got := certificateDeployable(item.row); got != item.want {
			t.Fatalf("case %d: certificateDeployable = %v, want %v", i, got, item.want)
		}
	}
}

func TestPollCertificateFlowStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- pollCertificateFlow(ctx, func() (bool, error) {
			calls++
			return false, nil
		})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not stop after cancel")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestScanCertificateExpiryOnlyRenewsIssuedAndLegacyFailedCertificates(t *testing.T) {
	handler := newFlowTestHandler(t)
	sqlDB, err := handler.db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// 后台协程与测试共用同一个内存数据库连接
	sqlDB.SetMaxOpenConns(1)
	account := model.SSLAccount{Name: "le", Provider: "letsencrypt", Email: "a@example.com", Status: model.StatusEnabled}
	if err := handler.db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	soon := time.Now().Add(5 * 24 * time.Hour)
	later := time.Now().Add(80 * 24 * time.Hour)
	rows := map[string]*model.Certificate{
		"issued_soon":    {CommonName: "a.example.com", Status: certStatusIssued, ExpiresAt: &soon},
		"issued_later":   {CommonName: "b.example.com", Status: certStatusIssued, ExpiresAt: &later},
		"canceled_soon":  {CommonName: "c.example.com", Status: certStatusCanceled, ExpiresAt: &soon},
		"expired_soon":   {CommonName: "d.example.com", Status: certStatusExpired, ExpiresAt: &soon},
		"legacy_failed":  {CommonName: "e.example.com", Status: certStatusFailed, ExpiresAt: &soon},
		"retried_failed": {CommonName: "f.example.com", Status: certStatusFailed, ExpiresAt: &soon, RetryCount: 2},
	}
	for _, row := range rows {
		row.SSLAccountID = account.ID
		row.RenewBeforeDay = 30
		if err := handler.db.Create(row).Error; err != nil {
			t.Fatalf("create certificate: %v", err)
		}
	}

	handler.scanCertificateExpiry()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handler.contentSyncMu.Lock()
		running := len(handler.contentSyncInFlight)
		handler.contentSyncMu.Unlock()
		if running == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	reload := func(key string) model.Certificate {
		var row model.Certificate
		if err := handler.db.First(&row, rows[key].ID).Error; err != nil {
			t.Fatalf("reload %s: %v", key, err)
		}
		return row
	}
	// 续期被触发的证书会进入申请流程（测试环境没有域名表，申请会失败并安排退避重试）
	for _, key := range []string{"issued_soon", "legacy_failed"} {
		row := reload(key)
		if row.Status == rows[key].Status && row.RetryCount == rows[key].RetryCount {
			t.Fatalf("%s should have been renewed, status=%s retry=%d", key, row.Status, row.RetryCount)
		}
		if row.Status == certStatusFailed && row.NextRetryAt == nil {
			t.Fatalf("%s failed renewal should schedule a retry", key)
		}
	}
	for _, key := range []string{"issued_later", "canceled_soon", "expired_soon", "retried_failed"} {
		row := reload(key)
		if row.Status != rows[key].Status || row.RetryCount != rows[key].RetryCount {
			t.Fatalf("%s should not be renewed by the expiry scan, status=%s retry=%d", key, row.Status, row.RetryCount)
		}
	}
}
