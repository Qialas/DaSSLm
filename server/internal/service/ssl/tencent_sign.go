package ssl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
)

func TencentAuthorization(account model.SSLAccount, action string, rawPayload []byte, timestamp int64) (string, map[string]string) {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	hashedPayload := sha256Hex(rawPayload)
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + tencentSSLHost + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{"POST", "/", "", canonicalHeaders, signedHeaders, hashedPayload}, "\n")
	credentialScope := date + "/ssl/tc3_request"
	stringToSign := strings.Join([]string{"TC3-HMAC-SHA256", strconv.FormatInt(timestamp, 10), credentialScope, sha256Hex([]byte(canonicalRequest))}, "\n")
	secretDate := hmacSHA256([]byte("TC3"+account.SecretKey), date)
	secretService := hmacSHA256(secretDate, "ssl")
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", account.AccessKey, credentialScope, signedHeaders, signature)
	headers := map[string]string{
		"Authorization":  maskTencentAuthorization(authorization),
		"Content-Type":   "application/json; charset=utf-8",
		"Host":           tencentSSLHost,
		"X-TC-Action":    action,
		"X-TC-Timestamp": strconv.FormatInt(timestamp, 10),
		"X-TC-Version":   tencentSSLVersion,
	}
	return authorization, headers
}

func maskTencentAuthorization(value string) string {
	parts := strings.Split(value, "Signature=")
	if len(parts) != 2 || len(parts[1]) <= 8 {
		return value
	}
	signature := parts[1]
	return parts[0] + "Signature=" + signature[:4] + "***" + signature[len(signature)-4:]
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
