package certificate

import (
	"context"
	core "dasslm/server/internal/handler"
	"fmt"
	"net/http"
	"strings"

	"dasslm/server/internal/model"
	deployservice "dasslm/server/internal/service/deploy"
	"dasslm/server/internal/service/notification"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type certificateDeployRequest struct {
	DeployAccountID uint   `json:"deployAccountId" binding:"required"`
	SiteName        string `json:"siteName"`
}

func (h *CertificateHandler) Deploy(c *gin.Context) {
	row, _, found := h.certificateAccount(c)
	if !found {
		return
	}
	var req certificateDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	var account model.DeployAccount
	if err := h.db.First(&account, req.DeployAccountID).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "部署账号不存在")
		return
	}
	if account.Status == model.StatusDisabled {
		core.Fail(c, http.StatusBadRequest, "部署账号已停用")
		return
	}
	if !certificateDeployable(row) {
		core.Fail(c, http.StatusBadRequest, "证书未签发，无法部署")
		return
	}
	targetName := firstNonEmpty(req.SiteName, row.CommonName)
	trace, err := deployIssuedCertificate(c.Request.Context(), h.db, row, account, targetName)
	status := "success"
	message := "证书已部署到目标服务"
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	h.logCertificateDeployment(row, account, targetName, status, message, trace)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, "部署证书失败："+err.Error())
		return
	}
	notification.DispatchAsync(h.db, notification.EventDeploySuccess, notification.Message{
		Title:   "SSL证书部署成功",
		Content: fmt.Sprintf("证书 %s 已部署到 %s。", row.CommonName, targetName),
		Level:   "info",
		Variables: map[string]string{
			"domain": row.CommonName,
			"target": targetName,
		},
	})
	core.Ok(c, gin.H{"deployed": true, "targetName": targetName})
}

func deployIssuedCertificate(ctx context.Context, db *gorm.DB, row model.Certificate, account model.DeployAccount, targetName string) (*deployRequestTrace, error) {
	return deployIssuedCertificateToTarget(ctx, db, row, account, model.AutoTaskTarget{SiteName: targetName})
}

func deployIssuedCertificateToTarget(ctx context.Context, db *gorm.DB, row model.Certificate, account model.DeployAccount, target model.AutoTaskTarget) (*deployRequestTrace, error) {
	if !certificateDeployable(row) {
		return nil, fmt.Errorf("证书未签发，无法部署")
	}
	certPEM := strings.TrimSpace(row.CertPEM)
	if chainPEM := strings.TrimSpace(row.ChainPEM); chainPEM != "" {
		certPEM = strings.TrimSpace(certPEM + "\n" + chainPEM)
	}
	if certPEM == "" || strings.TrimSpace(row.KeyPEM) == "" {
		return nil, fmt.Errorf("证书内容不完整，请先在详情中下载证书内容")
	}
	client, err := deployHTTPClient(db, account)
	if err != nil {
		return nil, err
	}
	return deployCertificateToTarget(ctx, account, client, deployservice.Target{
		SiteName:     firstNonEmpty(target.SiteName, row.CommonName),
		ProductType:  target.ProductType,
		Region:       target.Region,
		ResourceID:   target.ResourceID,
		ResourceName: target.ResourceName,
		ExtraConfig:  target.ExtraConfig,
	}, certPEM, row.KeyPEM)
}

func deployCertificate(ctx context.Context, account model.DeployAccount, client *http.Client, siteName, certPEM, keyPEM string) (*deployRequestTrace, error) {
	return deployservice.DeployCertificate(ctx, account, client, siteName, certPEM, keyPEM)
}

func deployCertificateToTarget(ctx context.Context, account model.DeployAccount, client *http.Client, target deployservice.Target, certPEM, keyPEM string) (*deployRequestTrace, error) {
	return deployservice.DeployCertificateToTarget(ctx, account, client, target, certPEM, keyPEM)
}
