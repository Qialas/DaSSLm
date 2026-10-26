package certificate

import (
	"context"
	core "dasslm/server/internal/handler"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
	sslservice "dasslm/server/internal/service/ssl"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SSLAccountHandler struct {
	*core.ResourceHandler[model.SSLAccount]
	db *gorm.DB
}

func NewSSLAccountHandler(db *gorm.DB) *SSLAccountHandler {
	return &SSLAccountHandler{ResourceHandler: core.NewResourceHandler[model.SSLAccount](db), db: db}
}

func (h *SSLAccountHandler) Certificates(c *gin.Context) {
	account, found := h.sslAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书获取证书资源")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	client, err := core.SSLHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx := context.WithValue(c.Request.Context(), sslHTTPClientContextKey{}, client)
	items, total, err := sslservice.ListTencentCertificates(ctx, account, uint64((page-1)*pageSize), uint64(pageSize), c.Query("keyword"))
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	core.Ok(c, gin.H{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

func (h *SSLAccountHandler) ImportCertificates(c *gin.Context) {
	account, found := h.sslAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书保存到本地")
		return
	}
	client, err := core.SSLHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx := context.WithValue(c.Request.Context(), sslHTTPClientContextKey{}, client)
	items, _, err := sslservice.ListTencentCertificates(ctx, account, 0, 100, "")
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	saved := 0
	for _, item := range items {
		row, ok, err := h.saveTencentCertificate(account, item)
		if err != nil {
			core.Fail(c, http.StatusInternalServerError, "保存证书资源失败")
			return
		}
		if !ok {
			continue
		}
		saved++
		h.logTencentCertificateImport(row, account)
	}
	core.Ok(c, gin.H{"saved": saved})
}

func (h *SSLAccountHandler) ImportCertificate(c *gin.Context) {
	account, found := h.sslAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书保存到本地")
		return
	}
	certificateID := strings.TrimSpace(c.Param("certificateId"))
	if certificateID == "" {
		core.Fail(c, http.StatusBadRequest, "证书ID不能为空")
		return
	}
	client, err := core.SSLHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx := context.WithValue(c.Request.Context(), sslHTTPClientContextKey{}, client)
	items, _, err := sslservice.ListTencentCertificates(ctx, account, 0, 100, certificateID)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	for _, item := range items {
		if item.CertificateID != certificateID {
			continue
		}
		row, saved, err := h.saveTencentCertificate(account, item)
		if err != nil {
			core.Fail(c, http.StatusInternalServerError, "保存证书资源失败")
			return
		}
		if !saved {
			core.Fail(c, http.StatusBadRequest, "证书资源信息不完整，无法导入")
			return
		}
		h.logTencentCertificateImport(row, account)
		core.Ok(c, gin.H{"item": row})
		return
	}
	core.Fail(c, http.StatusNotFound, "腾讯云证书资源不存在")
}

func (h *SSLAccountHandler) sslAccount(c *gin.Context) (model.SSLAccount, bool) {
	var account model.SSLAccount
	if err := h.db.First(&account, c.Param("id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "SSL账号不存在")
		return account, false
	}
	return account, true
}

func (h *SSLAccountHandler) findDomainID(domainName string) uint {
	name := normalizeImportDomainName(domainName)
	if name == "" {
		return 0
	}
	var domains []model.Domain
	if err := h.db.Find(&domains).Error; err != nil {
		return 0
	}
	sort.SliceStable(domains, func(i, j int) bool {
		return len(domains[i].Name) > len(domains[j].Name)
	})
	for _, domain := range domains {
		domainName := normalizeImportDomainName(domain.Name)
		if domainName == "" {
			continue
		}
		if name == domainName || strings.HasSuffix(name, "."+domainName) {
			return domain.ID
		}
	}
	return 0
}

func normalizeImportDomainName(value string) string {
	name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
	name = strings.TrimPrefix(name, "*.")
	name = strings.ReplaceAll(name, ",", ".")
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "https://")
	if slash := strings.Index(name, "/"); slash >= 0 {
		name = name[:slash]
	}
	return strings.TrimSpace(name)
}
