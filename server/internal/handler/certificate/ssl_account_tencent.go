package certificate

import (
	"strings"

	"dasslm/server/internal/model"
	sslservice "dasslm/server/internal/service/ssl"
)

func (h *SSLAccountHandler) saveTencentCertificate(account model.SSLAccount, item sslservice.TencentCertificateItem) (model.Certificate, bool, error) {
	if strings.TrimSpace(item.CertificateID) == "" || strings.TrimSpace(item.Domain) == "" {
		return model.Certificate{}, false, nil
	}
	row := sslservice.CertificateFromTencentItem(account.ID, item)
	if domainID := h.findDomainID(item.Domain); domainID > 0 {
		row.DomainID = domainID
	}
	err := h.db.Where("provider_certificate_id = ?", item.CertificateID).Assign(row).FirstOrCreate(&row).Error
	return row, true, err
}

func (h *SSLAccountHandler) logTencentCertificateImport(row model.Certificate, account model.SSLAccount) {
	_ = h.db.Create(&model.CertificateLog{
		CertificateID:         row.ID,
		ProviderCertificateID: row.ProviderCertificateID,
		CommonName:            row.CommonName,
		Action:                "import",
		Provider:              account.Provider,
		Status:                "success",
		Message:               "腾讯云证书资源已保存到本地",
	}).Error
}
