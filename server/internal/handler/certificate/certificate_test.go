package certificate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestACMEEABHmacKeyDecodesLiteSSLBase64URL(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	account := model.SSLAccount{
		Provider:   "litessl",
		EABHmacKey: base64.RawURLEncoding.EncodeToString(raw),
	}

	got, err := acmeEABHmacKey(account)
	if err != nil {
		t.Fatalf("acmeEABHmacKey returned error: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("acmeEABHmacKey = %q, want %q", got, raw)
	}
}

func TestACMEEABHmacKeyRejectsInvalidLiteSSLKey(t *testing.T) {
	_, err := acmeEABHmacKey(model.SSLAccount{
		Provider:   "litessl",
		EABHmacKey: "not a valid base64url value",
	})
	if err == nil {
		t.Fatal("acmeEABHmacKey returned nil error")
	}
}

func TestACMEEABHmacKeyKeepsCustomRawFallback(t *testing.T) {
	account := model.SSLAccount{
		Provider:   "custom_acme",
		EABHmacKey: "not a valid base64url value",
	}

	got, err := acmeEABHmacKey(account)
	if err != nil {
		t.Fatalf("acmeEABHmacKey returned error: %v", err)
	}
	if string(got) != account.EABHmacKey {
		t.Fatalf("acmeEABHmacKey = %q, want raw fallback %q", got, account.EABHmacKey)
	}
}

func TestFirstPEMCertificateDER(t *testing.T) {
	want := []byte{1, 2, 3, 4}
	value := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: want})) +
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{5, 6, 7, 8}}))
	got, err := firstPEMCertificateDER(value)
	if err != nil {
		t.Fatalf("firstPEMCertificateDER returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("firstPEMCertificateDER = %v, want %v", got, want)
	}
}

func TestFirstPEMCertificateDERRejectsInvalidValue(t *testing.T) {
	if _, err := firstPEMCertificateDER("not a certificate"); err == nil {
		t.Fatal("firstPEMCertificateDER returned nil error")
	}
}

func TestACMEAuthRecordFQDN(t *testing.T) {
	tests := []struct {
		name   string
		rr     string
		domain string
		want   string
	}{
		{name: "relative", rr: "_acme-challenge.private", domain: "ooiu.cn", want: "_acme-challenge.private.ooiu.cn"},
		{name: "absolute", rr: "_acme-challenge.private.ooiu.cn", domain: "ooiu.cn", want: "_acme-challenge.private.ooiu.cn"},
		{name: "root", rr: "@", domain: "ooiu.cn", want: "ooiu.cn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acmeAuthRecordFQDN(tt.rr, tt.domain); got != tt.want {
				t.Fatalf("acmeAuthRecordFQDN(%q, %q) = %q, want %q", tt.rr, tt.domain, got, tt.want)
			}
		})
	}
}

func TestContainsTXTValue(t *testing.T) {
	values := []string{"old-value", "EPFb8VyVlS-DzbCYcKSv74GH7Bhrm10QwBELZYLcmQI"}
	if !containsTXTValue(values, `"EPFb8VyVlS-DzbCYcKSv74GH7Bhrm10QwBELZYLcmQI"`) {
		t.Fatal("containsTXTValue did not match quoted expected value")
	}
	if containsTXTValue(values, "missing-value") {
		t.Fatal("containsTXTValue matched missing value")
	}
}

func TestCertificateContentNeedsRefreshDetectsRenewedMetadataWithOldPEM(t *testing.T) {
	certPEM, keyPEM, expiresAt := testCertificatePair(t, 101, time.Now().Add(24*time.Hour))
	renewedExpiresAt := expiresAt.Add(90 * 24 * time.Hour)
	row := model.Certificate{
		CertPEM:      certPEM,
		KeyPEM:       keyPEM,
		SerialNumber: "101",
		ExpiresAt:    &renewedExpiresAt,
	}

	if !certificateContentNeedsRefresh(row) {
		t.Fatal("certificateContentNeedsRefresh returned false for old PEM with renewed expiry metadata")
	}
}

func TestCertificateContentNeedsRefreshAcceptsMatchingCertificate(t *testing.T) {
	certPEM, keyPEM, expiresAt := testCertificatePair(t, 102, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{
		CertPEM:               certPEM,
		KeyPEM:                keyPEM,
		SerialNumber:          "102",
		ExpiresAt:             &expiresAt,
		ProviderCertificateID: "current-certificate-id",
		ContentCertificateID:  "current-certificate-id",
	}

	if certificateContentNeedsRefresh(row) {
		t.Fatal("certificateContentNeedsRefresh returned true for matching certificate content")
	}
}

func TestCertificateContentNeedsRefreshDetectsNewProviderCertificate(t *testing.T) {
	certPEM, keyPEM, expiresAt := testCertificatePair(t, 106, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{
		CertPEM:               certPEM,
		KeyPEM:                keyPEM,
		SerialNumber:          "106",
		ExpiresAt:             &expiresAt,
		ProviderCertificateID: "new-certificate-id",
		ContentCertificateID:  "old-certificate-id",
	}

	if !certificateContentNeedsRefresh(row) {
		t.Fatal("certificateContentNeedsRefresh returned false when provider certificate ID changed")
	}
}

func TestCertificateContentNeedsRefreshDetectsMismatchedPrivateKey(t *testing.T) {
	certPEM, _, expiresAt := testCertificatePair(t, 103, time.Now().Add(90*24*time.Hour))
	_, otherKeyPEM, _ := testCertificatePair(t, 104, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{CertPEM: certPEM, KeyPEM: otherKeyPEM, ExpiresAt: &expiresAt}

	if !certificateContentNeedsRefresh(row) {
		t.Fatal("certificateContentNeedsRefresh returned false for mismatched certificate and private key")
	}
}

func TestIsLocalACMECertificateIssuedRequiresCurrentCertificateURL(t *testing.T) {
	certPEM, keyPEM, expiresAt := testCertificatePair(t, 105, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{
		CertPEM:               certPEM,
		KeyPEM:                keyPEM,
		SerialNumber:          "105",
		ExpiresAt:             &expiresAt,
		ProviderCertificateID: "https://acme.example/old-certificate",
		ContentCertificateID:  "https://acme.example/old-certificate",
	}

	if isLocalACMECertificateIssued(row, "https://acme.example/new-certificate") {
		t.Fatal("old ACME certificate content was accepted for a new certificate URL")
	}
	if !isLocalACMECertificateIssued(row, row.ProviderCertificateID) {
		t.Fatal("matching ACME certificate URL and content were not recognized")
	}
}

func TestScanCertificateContentSyncBackfillsValidExistingCertificate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Certificate{}); err != nil {
		t.Fatalf("migrate certificates: %v", err)
	}
	certPEM, keyPEM, expiresAt := testCertificatePair(t, 107, time.Now().Add(90*24*time.Hour))
	row := model.Certificate{
		CommonName:            "renewal-test.example.com",
		CertPEM:               certPEM,
		KeyPEM:                keyPEM,
		SerialNumber:          "107",
		ExpiresAt:             &expiresAt,
		ProviderCertificateID: "existing-certificate-id",
		Status:                certStatusIssued,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	handler := &CertificateHandler{db: db, contentSyncInFlight: map[uint]*certificateWork{}}

	handler.scanCertificateContentSync()

	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatalf("reload certificate: %v", err)
	}
	if row.ContentCertificateID != row.ProviderCertificateID {
		t.Fatalf("content certificate ID = %q, want %q", row.ContentCertificateID, row.ProviderCertificateID)
	}
}

func TestCertificateIssueProgressStates(t *testing.T) {
	for _, status := range []string{certStatusApplying, certStatusDNSAdded, certStatusSubmitted} {
		if !isCertificateIssueInProgress(status) {
			t.Fatalf("status %q should be recoverable after restart", status)
		}
	}
	for _, status := range []string{certStatusQueued, certStatusIssued, certStatusFailed, certStatusExpired} {
		if isCertificateIssueInProgress(status) {
			t.Fatalf("status %q should not be handled by issue progress recovery", status)
		}
	}
}

func TestCertificateContentWorkClaimPreventsDuplicateWorkers(t *testing.T) {
	handler := &CertificateHandler{}
	if !handler.claimCertificateContentWork(1) {
		t.Fatal("first content work claim failed")
	}
	if handler.claimCertificateContentWork(1) {
		t.Fatal("duplicate content work claim succeeded")
	}
	handler.releaseCertificateContentWork(1)
	if !handler.claimCertificateContentWork(1) {
		t.Fatal("content work claim did not recover after release")
	}
}

func TestSSLAccountFindDomainIDMatchesSubdomain(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Domain{}); err != nil {
		t.Fatalf("migrate domains: %v", err)
	}
	domain := model.Domain{Name: "baidu.com", Status: model.StatusEnabled}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}
	handler := &SSLAccountHandler{db: db}
	for _, input := range []string{"baidu.com", "a.baidu.com", "*.a.baidu.com", "a.baidu,com"} {
		if got := handler.findDomainID(input); got != domain.ID {
			t.Fatalf("findDomainID(%q) = %d, want %d", input, got, domain.ID)
		}
	}
}

func testCertificatePair(t *testing.T, serial int64, notAfter time.Time) (string, string, time.Time) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	notAfter = notAfter.UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "renewal-test.example.com"},
		Issuer:       pkix.Name{CommonName: "DaSSLm Test CA"},
		NotBefore:    time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		NotAfter:     notAfter,
		DNSNames:     []string{"renewal-test.example.com"},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return certPEM, keyPEM, notAfter
}
