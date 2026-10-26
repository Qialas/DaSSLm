package handler

import (
	"crypto/tls"
	"dasslm/server/internal/model"
	proxyservice "dasslm/server/internal/service/proxypool"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"
)

func domainAccountProxy(db *gorm.DB, account model.DomainAccount) (*model.ProxySetting, error) {
	return DomainAccountProxy(db, account)
}

func DomainAccountProxy(db *gorm.DB, account model.DomainAccount) (*model.ProxySetting, error) {
	return proxyservice.Resolve(db, fmt.Sprintf("domain-account-%d", account.ID), account.UseProxy, account.ProxyMode, account.ProxyID)
}

func sslAccountProxy(db *gorm.DB, account model.SSLAccount) (*model.ProxySetting, error) {
	return SSLAccountProxy(db, account)
}

func SSLAccountProxy(db *gorm.DB, account model.SSLAccount) (*model.ProxySetting, error) {
	return proxyservice.Resolve(db, fmt.Sprintf("ssl-account-%d", account.ID), account.UseProxy, account.ProxyMode, account.ProxyID)
}

func deployAccountProxy(db *gorm.DB, account model.DeployAccount) (*model.ProxySetting, error) {
	return DeployAccountProxy(db, account)
}

func DeployAccountProxy(db *gorm.DB, account model.DeployAccount) (*model.ProxySetting, error) {
	return proxyservice.Resolve(db, fmt.Sprintf("deploy-account-%d", account.ID), account.UseProxy, account.ProxyMode, account.ProxyID)
}

func DeployHTTPClient(db *gorm.DB, account model.DeployAccount, timeout time.Duration) (*http.Client, error) {
	proxySetting, err := DeployAccountProxy(db, account)
	if err != nil {
		return nil, err
	}
	client, err := proxyservice.Client(proxySetting, timeout)
	if err != nil {
		return nil, err
	}
	// 宝塔面板默认使用自签名 HTTPS 证书，统一忽略证书校验
	if isBTPanelProvider(account.Provider) {
		skipTLSVerify(client)
	}
	return client, nil
}

func isBTPanelProvider(provider string) bool {
	return provider == "btpanel" || provider == "baota"
}

// skipTLSVerify 关闭 HTTPS 证书校验（宝塔面板默认使用自签名证书），保留原有的代理拨号配置
func skipTLSVerify(client *http.Client) {
	var transport *http.Transport
	if current, ok := client.Transport.(*http.Transport); ok && current != nil {
		transport = current.Clone()
	} else {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 -- 宝塔面板自签名证书
	client.Transport = transport
}

func sslHTTPClient(db *gorm.DB, account model.SSLAccount, timeout time.Duration) (*http.Client, error) {
	return SSLHTTPClient(db, account, timeout)
}

func SSLHTTPClient(db *gorm.DB, account model.SSLAccount, timeout time.Duration) (*http.Client, error) {
	proxySetting, err := sslAccountProxy(db, account)
	if err != nil {
		return nil, err
	}
	return proxyservice.Client(proxySetting, timeout)
}
