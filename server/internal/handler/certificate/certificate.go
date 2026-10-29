package certificate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	core "dasslm/server/internal/handler"
	domainhandler "dasslm/server/internal/handler/domain"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dasslm/server/internal/model"
	dnsservice "dasslm/server/internal/service/dns"
	"dasslm/server/internal/service/notification"
	sslservice "dasslm/server/internal/service/ssl"
	"dasslm/server/internal/service/systemtask"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/acme"
	"gorm.io/gorm"
)

const (
	certStatusQueued               = "queued"
	certStatusPending              = "pending"
	certStatusApplying             = "applying"
	certStatusDNSAdded             = "dns_added"
	certStatusSubmitted            = "submitted"
	certStatusIssued               = "issued"
	certStatusFailed               = "failed"
	certStatusExpired              = "expired"
	certStatusCanceled             = "canceled"
	certStatusRevoking             = "revoking"
	certStatusRevoked              = "revoked"
	certFlowInterval               = 10 * time.Second
	certificateContentSyncInterval = 10 * time.Minute
)

type CertificateHandler struct {
	*core.ResourceHandler[model.Certificate]
	db *gorm.DB
	// contentSyncInFlight 记录每张证书正在执行的后台任务（申请、签发、同步、吊销），
	// 同一张证书同一时间只允许一个任务，重新申请/删除时可取消正在执行的任务。
	contentSyncMu       sync.Mutex
	contentSyncInFlight map[uint]*certificateWork
	progressLogMu       sync.Mutex
	progressLogAt       map[string]time.Time
}

func NewCertificateHandler(db *gorm.DB) *CertificateHandler {
	h := &CertificateHandler{
		ResourceHandler:     core.NewResourceHandler[model.Certificate](db),
		db:                  db,
		contentSyncInFlight: map[uint]*certificateWork{},
		progressLogAt:       map[string]time.Time{},
	}
	h.startCertificateApplyWorker()
	h.startCertificateExpiryWorker()
	h.startCertificateContentSyncWorker()
	return h
}

type certificateRequest struct {
	DomainID       uint   `json:"domainId" binding:"required"`
	SSLAccountID   uint   `json:"sslAccountId" binding:"required"`
	CommonName     string `json:"commonName"`
	SANs           string `json:"sans"`
	RenewBeforeDay int    `json:"renewBeforeDay"`
}

type certificateUpdateRequest struct {
	DomainID       uint   `json:"domainId"`
	SSLAccountID   uint   `json:"sslAccountId"`
	CommonName     string `json:"commonName"`
	SANs           string `json:"sans"`
	RenewBeforeDay int    `json:"renewBeforeDay"`
}

func (h *CertificateHandler) Create(c *gin.Context) {
	var req certificateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	domain, account, commonName, sans, valid := h.validateRequest(c, req)
	if !valid {
		return
	}
	renewBeforeDay := req.RenewBeforeDay
	if renewBeforeDay <= 0 {
		renewBeforeDay = 30
	}

	row := model.Certificate{
		DomainID:          domain.ID,
		SSLAccountID:      account.ID,
		CommonName:        commonName,
		SANs:              sans,
		RenewBeforeDay:    renewBeforeDay,
		Status:            certStatusPending,
		ProviderStatusMsg: "已创建证书任务，等待后台自动检查或手动开始申请",
	}
	if existing, exists, err := h.activeCertificateTask(account.ID, commonName, row.SANs); err == nil && exists {
		core.Ok(c, existing)
		return
	} else if err != nil {
		row.ProviderStatusMsg = "已创建证书任务，查重检查失败：" + truncateMessage(err.Error(), 180)
	}
	if err := h.db.Create(&row).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "创建证书申请失败")
		return
	}
	logMessage := "证书申请任务已创建，等待后台自动检查或手动开始申请"
	if strings.Contains(row.ProviderStatusMsg, "查重检查失败") {
		logMessage = row.ProviderStatusMsg
	}
	h.logCertificate(row, account, "apply", "success", logMessage, nil)
	core.Ok(c, row)
}

func (h *CertificateHandler) Update(c *gin.Context) {
	var row model.Certificate
	if err := h.db.First(&row, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "证书不存在")
		return
	}

	var req certificateUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	renewBeforeDay := req.RenewBeforeDay
	if renewBeforeDay <= 0 {
		renewBeforeDay = row.RenewBeforeDay
	}
	if renewBeforeDay <= 0 {
		renewBeforeDay = 30
	}

	updates := map[string]any{
		"renew_before_day": renewBeforeDay,
	}
	if req.DomainID > 0 && req.SSLAccountID > 0 {
		domain, account, commonName, sans, valid := h.validateRequest(c, certificateRequest{
			DomainID:       req.DomainID,
			SSLAccountID:   req.SSLAccountID,
			CommonName:     req.CommonName,
			SANs:           req.SANs,
			RenewBeforeDay: renewBeforeDay,
		})
		if !valid {
			return
		}
		changed := domain.ID != row.DomainID ||
			account.ID != row.SSLAccountID ||
			commonName != row.CommonName ||
			certificateNamesKey(commonName, sans) != certificateNamesKey(row.CommonName, row.SANs)
		// 申请进行中修改域名或账号会导致订单与CSR/账号不一致，签发必然失败
		if changed && (isCertificateApplicationActive(row.Status) || row.Status == certStatusRevoking) {
			core.Fail(c, http.StatusBadRequest, "证书正在申请或吊销中，暂不能修改域名和SSL账号，请等待完成或使用“重新申请”后再修改")
			return
		}
		if changed {
			updates["domain_id"] = domain.ID
			updates["ssl_account_id"] = account.ID
			updates["common_name"] = commonName
			updates["sans"] = sans
			// 未完成的旧订单属于旧的域名/账号，修改后不能继续使用
			if row.Status != certStatusIssued && row.Status != certStatusRevoked {
				updates["provider_order_id"] = ""
				updates["provider_certificate_id"] = ""
				updates["provider_status"] = ""
				updates["pending_key_pem"] = ""
			}
		}
	}
	if err := h.db.Model(&row).Updates(updates).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存证书申请失败")
		return
	}
	h.db.First(&row, row.ID)
	core.Ok(c, row)
}

func (h *CertificateHandler) Submit(c *gin.Context) {
	row, account, found := h.certificateAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书和ACME证书提交")
		return
	}
	if strings.EqualFold(account.Provider, "tencent_free") && row.Status == certStatusRevoking {
		if h.certificateWorkRunning(row.ID) {
			core.Ok(c, row)
			return
		}
		ctx, err := h.contextWithSSLHTTPClient(c.Request.Context(), account)
		if err != nil {
			core.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		trace, err := h.ensureTencentRevokeAuthRecordsFromDetail(ctx, &row, account)
		if err != nil {
			h.logCertificate(row, account, "revoke_verify", "failed", "确认吊销DNS验证记录失败："+err.Error(), trace)
			core.Fail(c, http.StatusBadRequest, "确认吊销DNS验证记录失败："+err.Error())
			return
		}
		_ = h.db.Model(&row).Updates(map[string]any{"status": certStatusRevoking, "provider_status_msg": "已确认吊销DNS验证记录，等待域名验证"})
		h.db.First(&row, row.ID)
		h.logCertificate(row, account, "revoke", "success", "已确认吊销DNS验证记录，继续提交验证", trace)
		h.startTencentRevokeFlow(row.ID, account.ID)
		core.Ok(c, row)
		return
	}

	// 失败、过期、已取消：清理旧订单与验证记录后重新申请
	if canRequeueCertificateApplication(row.Status) {
		h.requeueFromRequest(c, row, account, "证书已重新进入后台申请队列")
		return
	}

	if needsCertificateOrder(row, account) {
		switch row.Status {
		case certStatusQueued, certStatusPending:
			if row.Status == certStatusPending {
				if err := h.db.Model(&row).Updates(map[string]any{
					"status":              certStatusQueued,
					"provider_status_msg": "已进入后台申请队列",
				}).Error; err != nil {
					core.Fail(c, http.StatusInternalServerError, "加入后台申请队列失败")
					return
				}
				h.db.First(&row, row.ID)
				h.logCertificate(row, account, "apply", "success", "证书申请任务已进入后台队列", nil)
			}
			h.enqueueCertificateApply(row.ID)
			core.Ok(c, row)
			return
		case certStatusApplying:
			core.Ok(c, row)
			return
		default:
			// 处于申请中间状态却没有订单，说明数据已不一致，直接重新申请
			h.requeueFromRequest(c, row, account, "证书申请单缺失，已重新进入后台申请队列")
			return
		}
	}

	if row.Status == certStatusIssued || row.Status == certStatusRevoked {
		h.refreshCertificateExclusive(c.Request.Context(), &row, account)
		h.db.First(&row, row.ID)
		core.Ok(c, row)
		return
	}

	if h.certificateWorkRunning(row.ID) {
		// 后台正在验证签发，避免重复提交
		core.Ok(c, row)
		return
	}

	if isACMEProvider(account.Provider) {
		if row.Status == certStatusApplying {
			core.Ok(c, row)
			return
		}
		// 先确认旧订单仍然可用，订单失效（过期/被CA删除）时直接重新申请，避免一直卡在验证中
		ctx, err := h.contextWithSSLHTTPClient(c.Request.Context(), account)
		if err != nil {
			core.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		usable, reason, err := h.checkACMEOrderUsable(ctx, &row, &account)
		if err != nil {
			core.Fail(c, http.StatusBadRequest, "检查ACME订单状态失败："+err.Error())
			return
		}
		if !usable {
			h.logCertificate(row, account, "apply", "failed", reason+"，已自动重新申请", nil)
			h.requeueFromRequest(c, row, account, reason+"，已重新进入后台申请队列")
			return
		}
		h.logCertificate(row, account, "submit", "success", "已开始后台验证签发", nil)
		h.startCertificateIssueFlow(row.ID, account.ID)
		core.Ok(c, row)
		return
	}

	if row.AuthRecordID == "" && row.Status != certStatusSubmitted {
		if _, err := h.ensureTencentAuthRecord(c.Request.Context(), &row, account); err != nil {
			h.logCertificate(row, account, "apply", "failed", err.Error(), traceFromError(err))
			core.Fail(c, http.StatusBadRequest, "补全DNS验证记录失败："+err.Error())
			return
		}
		h.db.First(&row, row.ID)
	}

	h.db.Model(&row).Updates(map[string]any{"status": certStatusSubmitted, "provider_status_msg": "DNS验证记录已添加，等待CA验证"})
	h.db.First(&row, row.ID)
	h.logCertificate(row, account, "submit", "success", "DNS验证记录已添加，等待CA验证", nil)
	h.startCertificateIssueFlow(row.ID, account.ID)
	core.Ok(c, row)
}

// Reset 重新申请：取消正在执行的后台任务，清理旧订单与DNS验证记录后重新进入申请队列。
// 已签发的证书执行一次立即续期，新证书签发前旧证书内容保持不变。
func (h *CertificateHandler) Reset(c *gin.Context) {
	row, account, found := h.certificateAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书和ACME证书重新申请")
		return
	}
	if account.Status == model.StatusDisabled {
		core.Fail(c, http.StatusBadRequest, "SSL账号已停用")
		return
	}
	if row.Status == certStatusRevoking {
		core.Fail(c, http.StatusBadRequest, "证书正在吊销中，暂不能重新申请")
		return
	}
	message := "已手动重新申请，已进入后台申请队列"
	if row.Status == certStatusIssued {
		message = "已手动发起续期申请，新证书签发前旧证书保持不变"
	}
	h.requeueFromRequest(c, row, account, message)
}

// requeueFromRequest 接管证书的后台任务后重新入队，并返回最新的证书数据。
func (h *CertificateHandler) requeueFromRequest(c *gin.Context, row model.Certificate, account model.SSLAccount, message string) {
	workCtx, release, ok := h.takeoverCertificateWork(row.ID, certWorkCancelWait)
	if !ok {
		core.Fail(c, http.StatusConflict, "后台任务正在处理该证书，请稍后再试")
		return
	}
	_ = h.db.First(&row, row.ID).Error
	if err := h.requeueCertificateApplication(c.Request.Context(), &row, account, message, true); err != nil {
		release()
		h.logCertificate(row, account, "apply", "failed", "重新加入申请队列失败："+err.Error(), nil)
		core.Fail(c, http.StatusInternalServerError, "重新加入申请队列失败："+err.Error())
		return
	}
	h.db.First(&row, row.ID)
	h.logCertificate(row, account, "apply", "success", message, nil)
	// 继续持有后台任务直接开始申请，避免释放后被其他流程抢占
	h.startCertificateApplyClaimed(workCtx, row.ID)
	core.Ok(c, row)
}

func (h *CertificateHandler) Revoke(c *gin.Context) {
	row, account, found := h.certificateAccount(c)
	if !found {
		return
	}
	if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
		core.Fail(c, http.StatusBadRequest, "当前只支持腾讯云免费证书和ACME证书吊销")
		return
	}
	if isACMEProvider(account.Provider) {
		if row.Status != certStatusIssued && row.Status != certStatusExpired {
			// ACME没有“取消订单”接口：停用验证授权并清理DNS记录，相当于取消本次申请
			h.cancelLocalApplication(c, row, account)
			return
		}
		trace, err := h.revokeACMECertificate(c.Request.Context(), &row, &account)
		if err != nil {
			h.logCertificate(row, account, "revoke", "failed", err.Error(), trace)
			core.Fail(c, http.StatusBadRequest, "吊销ACME证书失败："+err.Error())
			return
		}
		if err := h.db.Model(&row).Updates(map[string]any{
			"status":              certStatusRevoked,
			"provider_status":     "revoked",
			"provider_status_msg": "ACME证书已吊销",
			"next_retry_at":       nil,
		}).Error; err != nil {
			core.Fail(c, http.StatusInternalServerError, "保存证书状态失败")
			return
		}
		h.db.First(&row, row.ID)
		h.logCertificate(row, account, "revoke", "success", "ACME证书已吊销", trace)
		core.Ok(c, row)
		return
	}
	if strings.TrimSpace(row.ProviderCertificateID) == "" {
		if row.Status == certStatusIssued || row.Status == certStatusRevoking {
			core.Fail(c, http.StatusBadRequest, "证书缺少腾讯云证书ID")
			return
		}
		// 还没有在腾讯云创建订单：直接取消本地申请
		h.cancelLocalApplication(c, row, account)
		return
	}
	ctx, err := h.contextWithSSLHTTPClient(c.Request.Context(), account)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if row.Status == certStatusIssued || row.Status == certStatusRevoking {
		var auths []tencentRevokeAuth
		var trace *tencentRequestTrace
		if row.Status == certStatusRevoking {
			trace, err = h.ensureTencentRevokeAuthRecordsFromDetail(ctx, &row, account)
		} else {
			auths, trace, err = tencentRevokeCertificate(ctx, account, row.ProviderCertificateID, "revoke")
		}
		if err != nil {
			h.logCertificate(row, account, "revoke", "failed", err.Error(), traceFromError(err))
			core.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		if row.Status != certStatusRevoking {
			err = h.createTencentRevokeAuthRecords(ctx, &row, auths)
		}
		if err != nil {
			h.logCertificate(row, account, "revoke_verify", "failed", "添加吊销DNS验证记录失败："+err.Error(), trace)
			core.Fail(c, http.StatusBadRequest, "添加吊销DNS验证记录失败："+err.Error())
			return
		}
		h.db.Model(&row).Updates(map[string]any{"status": certStatusRevoking, "provider_status_msg": "已提交吊销请求，等待域名验证", "next_retry_at": nil})
		h.logCertificate(row, account, "revoke", "success", "已提交吊销请求，DNS验证记录已确认", trace)
		h.startTencentRevokeFlow(row.ID, account.ID)
	} else {
		_, release, ok := h.takeoverCertificateWork(row.ID, certWorkCancelWait)
		if !ok {
			core.Fail(c, http.StatusConflict, "后台任务正在处理该证书，请稍后再试")
			return
		}
		defer release()
		_ = h.db.First(&row, row.ID).Error
		trace, err := tencentSimpleCertificateAction(ctx, account, "CancelCertificateOrder", row.ProviderCertificateID)
		if err != nil {
			h.logCertificate(row, account, "revoke", "failed", err.Error(), traceFromError(err))
			core.Fail(c, http.StatusBadRequest, err.Error())
			return
		}
		if _, cleanupErr := h.cleanupAuthRecordWithMessage(ctx, &row, "certificate_cancel_cleanup", "证书订单已取消，已删除SSL验证记录", "已取消证书订单"); cleanupErr != nil {
			h.logCertificate(row, account, "cleanup", "failed", "删除DNS验证记录失败："+cleanupErr.Error(), nil)
		}
		h.db.Model(&row).Updates(map[string]any{"status": certStatusCanceled, "provider_status_msg": "已取消证书订单", "next_retry_at": nil, "pending_key_pem": ""})
		h.logCertificate(row, account, "revoke", "success", "已取消证书订单", trace)
	}
	h.db.First(&row, row.ID)
	core.Ok(c, row)
}

func (h *CertificateHandler) Detail(c *gin.Context) {
	row, account, found := h.certificateAccount(c)
	if !found {
		return
	}
	if shouldRefreshCertificate(row, account) && isTruthyQuery(c.Query("refresh")) {
		if isCertificateIssueInProgress(row.Status) {
			// 申请进行中：由后台任务负责验证与签发，这里只在没有后台任务时补启动一次，
			// 避免前端轮询时在请求里同步等待DNS生效或重复提交验证。
			if row.Status != certStatusApplying && !h.certificateWorkRunning(row.ID) {
				h.startCertificateIssueFlow(row.ID, account.ID)
			}
		} else {
			h.refreshCertificateExclusive(c.Request.Context(), &row, account)
		}
		h.db.First(&row, row.ID)
	}
	core.Ok(c, row)
}

// refreshCertificateExclusive 在没有其他后台任务处理该证书时刷新服务商状态并同步证书内容，
// 防止与后台签发流程并发执行导致重复通知、重复自动部署。
func (h *CertificateHandler) refreshCertificateExclusive(ctx context.Context, row *model.Certificate, account model.SSLAccount) {
	if _, ok := h.claimCertificateWork(row.ID); !ok {
		return
	}
	defer h.releaseCertificateContentWork(row.ID)
	_ = h.db.First(row, row.ID).Error
	trace, err := h.refreshCertificate(ctx, row, account)
	if err != nil {
		if h.allowProgressLog(row.ID, "refresh", err.Error()) {
			h.logCertificate(*row, account, "verify", "failed", "刷新证书状态失败："+err.Error(), traceFromError(err))
		}
	} else if trace != nil && h.allowProgressLog(row.ID, "refresh_success", row.Status) {
		h.logCertificate(*row, account, "verify", "success", verifyLogMessage(account), trace)
	}
	_ = h.db.First(row, row.ID).Error
	if row.Status == certStatusIssued && certificateContentNeedsRefresh(*row) {
		contentRefreshed := false
		if err := h.downloadCertificateContent(ctx, row, account); err != nil {
			h.logCertificate(*row, account, "detail", "failed", "下载证书内容失败："+err.Error(), traceFromError(err))
		} else {
			contentRefreshed = true
		}
		_ = h.db.First(row, row.ID).Error
		if contentRefreshed && !certificateContentNeedsRefresh(*row) {
			triggerCertificateAutoDeployments(h.db, *row)
		}
	}
}

func (h *CertificateHandler) startCertificateFlow(ctx context.Context, row *model.Certificate, domain model.Domain, account *model.SSLAccount) (*tencentRequestTrace, error) {
	var err error
	ctx, err = h.contextWithSSLHTTPClient(ctx, *account)
	if err != nil {
		return nil, err
	}
	if isACMEProvider(account.Provider) {
		return h.startACMECertificateFlow(ctx, row, domain, account)
	}
	if !strings.EqualFold(account.Provider, "tencent_free") {
		return nil, fmt.Errorf("当前只支持腾讯云免费证书自动申请")
	}
	if strings.TrimSpace(account.Email) == "" {
		return nil, fmt.Errorf("腾讯云SSL账号需要填写联系人邮箱")
	}

	certificateID, trace, err := tencentApplyCertificate(ctx, *account, row.CommonName)
	if err != nil {
		return trace, err
	}
	row.ProviderCertificateID = certificateID
	h.db.Model(row).Updates(map[string]any{
		"provider_certificate_id": certificateID,
		"verify_type":             "DNS",
		"status":                  certStatusApplying,
	})

	detail, detailTrace, err := h.waitTencentDVDetail(ctx, *account, certificateID)
	if detailTrace != nil {
		trace = detailTrace
	}
	if err != nil {
		return trace, err
	}
	if detail != nil {
		h.applyTencentDetail(row, detail)
	}
	if detail == nil || detail.DvAuthDetail == nil {
		return trace, fmt.Errorf("腾讯云未返回DNS验证信息")
	}
	authName, authValue := pickTencentAuthRecord(detail.DvAuthDetail)
	if authName == "" || authValue == "" {
		return trace, fmt.Errorf("腾讯云DNS验证信息不完整")
	}
	recordID, rr, err := h.createAuthRecord(ctx, domain, authName, authValue)
	if err != nil {
		return trace, err
	}
	status := certStatusDNSAdded
	if detail.Status != nil {
		status = tencentCertStatus(*detail.Status, status)
	}
	h.db.Model(row).Updates(map[string]any{
		"auth_record_id":      recordID,
		"auth_record_name":    rr,
		"auth_record_value":   authValue,
		"auth_cleanup_status": "pending",
		"provider_status_msg": "DNS验证记录已添加",
		"status":              status,
	})
	return trace, nil
}

func (h *CertificateHandler) createAuthRecord(ctx context.Context, domain model.Domain, authName, authValue string) (string, string, error) {
	var domainAccount model.DomainAccount
	if err := h.db.First(&domainAccount, domain.DomainAccountID).Error; err != nil {
		return "", "", fmt.Errorf("域名账号不存在")
	}
	rr := normalizeAuthRR(authName, domain.Name)
	record := dnsservice.RecordInput{
		DomainName: domain.Name,
		RR:         rr,
		Type:       "TXT",
		Value:      authValue,
		Line:       "default",
		TTL:        600,
		Status:     "enabled",
		Remark:     "DaSSLm SSL验证",
	}
	dnsAccount, err := domainhandler.DNSAccountWithProxy(h.db, domainAccount)
	if err != nil {
		return "", "", err
	}
	if existing, findErr := findExistingAuthRecord(ctx, dnsAccount, domain.Name, record); findErr == nil && existing != nil {
		domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "create_record", "success", "SSL验证记录已存在，继续证书流程", domainhandler.DNSTrace(domainAccount, "ListRecords", record, existing, nil))
		return existing.ID, rr, nil
	}
	item, err := dnsservice.CreateRecord(ctx, dnsAccount, record)
	trace := domainhandler.DNSTrace(domainAccount, "CreateRecord", record, item, err)
	if err != nil {
		existing, findErr := findExistingAuthRecord(ctx, dnsAccount, domain.Name, record)
		if findErr == nil && existing != nil {
			domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "create_record", "success", "SSL验证记录已存在，继续证书流程", domainhandler.DNSTrace(domainAccount, "ListRecords", record, existing, nil))
			return existing.ID, rr, nil
		}
		domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "create_record", "failed", "添加SSL验证记录失败："+err.Error(), trace)
		return "", "", fmt.Errorf("添加DNS验证记录失败：%w", err)
	}
	domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "create_record", "success", "已添加SSL验证记录", trace)
	return item.ID, rr, nil
}

func findExistingAuthRecord(ctx context.Context, account dnsservice.Account, domainName string, target dnsservice.RecordInput) (*dnsservice.RecordItem, error) {
	records, err := dnsservice.ListRecords(ctx, account, domainName)
	if err != nil {
		return nil, err
	}
	targetRR := strings.TrimSpace(target.RR)
	targetType := strings.ToUpper(strings.TrimSpace(target.Type))
	targetValue := strings.TrimSpace(target.Value)
	for _, record := range records {
		if strings.TrimSpace(record.RR) == targetRR &&
			strings.ToUpper(strings.TrimSpace(record.Type)) == targetType &&
			strings.TrimSpace(record.Value) == targetValue {
			return &record, nil
		}
	}
	return nil, nil
}

type acmeAuthRecord struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Value            string `json:"value"`
	Domain           string `json:"domain"`
	AuthorizationURL string `json:"authorizationUrl"`
	ChallengeURL     string `json:"challengeUrl"`
}

func (h *CertificateHandler) startACMECertificateFlow(ctx context.Context, row *model.Certificate, domain model.Domain, account *model.SSLAccount) (*tencentRequestTrace, error) {
	if strings.TrimSpace(account.Email) == "" {
		return nil, fmt.Errorf("ACME账号需要填写注册邮箱")
	}
	client, trace, err := h.acmeClient(ctx, account)
	if err != nil {
		return trace, err
	}
	names := certificateNames(row, domain.Name)
	if len(names) == 0 {
		return trace, fmt.Errorf("证书域名不能为空")
	}
	if err := validateCertificateNames(domain.Name, account.Provider, names); err != nil {
		return trace, err
	}
	// 新私钥只写入 pending_key_pem，签发成功后才替换 key_pem；
	// 续期失败时旧证书与旧私钥保持可用。
	_, keyPEM, err := generateRSAPrivateKeyPEM()
	if err != nil {
		return trace, err
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(names...))
	trace = acmeTrace(account.Provider, "AuthorizeOrder", gin.H{"identifiers": names}, order, err)
	if err != nil {
		return trace, err
	}
	// 订单一创建就落库，进程中途退出时也能找到订单与已创建的验证记录并清理
	if err := h.db.Model(row).Updates(map[string]any{
		"pending_key_pem":     keyPEM,
		"provider_order_id":   order.URI,
		"provider_status":     order.Status,
		"provider_status_msg": "ACME订单已创建，正在添加DNS验证记录",
		"verify_type":         "DNS",
	}).Error; err != nil {
		return trace, err
	}
	records := make([]acmeAuthRecord, 0, len(order.AuthzURLs))
	saveRecords := func() error {
		rawRecords, _ := json.Marshal(records)
		updates := map[string]any{"auth_records": string(rawRecords)}
		if len(records) > 0 {
			updates["auth_record_id"] = records[0].ID
			updates["auth_record_name"] = records[0].Name
			updates["auth_record_value"] = records[0].Value
			updates["auth_cleanup_status"] = "pending"
		}
		return h.db.Model(row).Updates(updates).Error
	}
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		stepTrace := acmeTrace(account.Provider, "GetAuthorization", gin.H{"authorizationUrl": authzURL}, authz, err)
		if err != nil {
			return stepTrace, err
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		challenge := pickDNS01Challenge(authz)
		if challenge == nil {
			return stepTrace, fmt.Errorf("ACME未返回DNS-01验证信息：%s", authz.Identifier.Value)
		}
		value, err := client.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return stepTrace, err
		}
		authName := "_acme-challenge." + strings.TrimPrefix(authz.Identifier.Value, "*.")
		recordID, rr, err := h.createAuthRecord(ctx, domain, authName, value)
		if err != nil {
			return stepTrace, err
		}
		records = append(records, acmeAuthRecord{
			ID:               recordID,
			Name:             rr,
			Value:            value,
			Domain:           authz.Identifier.Value,
			AuthorizationURL: authzURL,
			ChallengeURL:     challenge.URI,
		})
		if err := saveRecords(); err != nil {
			return stepTrace, err
		}
	}
	if err := saveRecords(); err != nil {
		return trace, err
	}
	updates := map[string]any{
		"provider_status_msg": "DNS验证记录已添加",
		"status":              certStatusDNSAdded,
	}
	if len(records) == 0 {
		updates["provider_status_msg"] = "域名验证已有效，等待提交签发"
	}
	if err := h.db.Model(row).Updates(updates).Error; err != nil {
		return trace, err
	}
	return trace, nil
}

func (h *CertificateHandler) ensureTencentAuthRecord(ctx context.Context, row *model.Certificate, account model.SSLAccount) (*tencentRequestTrace, error) {
	var err error
	ctx, err = h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return nil, err
	}
	domain, err := h.resolveCertificateDomain(row, row.CommonName)
	if err != nil {
		return nil, err
	}
	detail, trace, err := h.waitTencentDVDetail(ctx, account, row.ProviderCertificateID)
	if err != nil {
		return trace, err
	}
	if detail == nil || detail.DvAuthDetail == nil {
		return trace, fmt.Errorf("腾讯云未返回DNS验证信息")
	}
	h.applyTencentDetail(row, detail)
	authName, authValue := pickTencentAuthRecord(detail.DvAuthDetail)
	if authName == "" || authValue == "" {
		return trace, fmt.Errorf("腾讯云DNS验证信息不完整")
	}
	recordID, rr, err := h.createAuthRecord(ctx, domain, authName, authValue)
	if err != nil {
		return trace, err
	}
	status := certStatusDNSAdded
	if detail.Status != nil {
		status = tencentCertStatus(*detail.Status, status)
	}
	h.db.Model(row).Updates(map[string]any{
		"auth_record_id":      recordID,
		"auth_record_name":    rr,
		"auth_record_value":   authValue,
		"auth_cleanup_status": "pending",
		"provider_status_msg": "DNS验证记录已添加",
		"status":              status,
	})
	return trace, nil
}

func (h *CertificateHandler) waitTencentDVDetail(ctx context.Context, account model.SSLAccount, certificateID string) (*tencentDetail, *tencentRequestTrace, error) {
	var lastErr error
	var lastTrace *tencentRequestTrace
	for i := 0; i < 3; i++ {
		detail, trace, err := tencentDescribeCertificate(ctx, account, certificateID)
		if trace != nil {
			lastTrace = trace
		}
		if err == nil && detail != nil && detail.DvAuthDetail != nil {
			return detail, trace, nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	if lastErr != nil {
		return nil, lastTrace, lastErr
	}
	return tencentDescribeCertificate(ctx, account, certificateID)
}

func (h *CertificateHandler) startCertificateIssueFlow(rowID uint, accountID uint) {
	ctx, ok := h.claimCertificateWork(rowID)
	if !ok {
		return
	}
	go func() {
		defer h.releaseCertificateContentWork(rowID)
		h.runCertificateIssueTask(ctx, rowID, accountID)
	}()
}

// runCertificateIssueTask 执行后台验证签发并记录任务日志，调用方需已持有该证书的后台任务。
func (h *CertificateHandler) runCertificateIssueTask(ctx context.Context, rowID uint, accountID uint) {
	var row model.Certificate
	var account model.SSLAccount
	var taskLog *model.TaskLog
	if h.db.First(&row, rowID).Error == nil {
		taskLog = h.startTaskLog("后台证书验证签发", "certificate_issue", row)
	}
	if err := h.runCertificateIssueFlow(ctx, rowID, accountID); err != nil {
		if ctx.Err() != nil {
			// 被“重新申请/删除/取消”主动中断，不记为失败
			if taskLog != nil {
				h.finishTaskLog(taskLog, "success", "后台验证签发已被中断", taskLogDetail(row, nil, "issue_canceled"))
			}
			return
		}
		_ = h.db.First(&row, rowID).Error
		if h.db.First(&account, accountID).Error == nil && h.allowProgressLog(rowID, "issue_flow", err.Error()) {
			h.logCertificate(row, account, "verify", "failed", "证书自动验证流程未完成："+err.Error(), nil)
		}
		if taskLog != nil {
			h.finishTaskLog(taskLog, "failed", err.Error(), taskLogDetail(row, &account, "issue_failed"))
		}
		return
	}
	if taskLog != nil {
		_ = h.db.First(&row, rowID).Error
		if account.ID == 0 {
			_ = h.db.First(&account, accountID).Error
		}
		h.finishTaskLog(taskLog, "success", "证书后台验证签发流程已完成", taskLogDetail(row, &account, "issue_success"))
	}
}

func (h *CertificateHandler) startCertificateApplyWorker() {
	go func() {
		time.Sleep(2 * time.Second)
		interval := systemtask.Interval(h.db, systemtask.CertificateApplyQueueTaskKey)
		core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_apply_worker_start", "success", "证书申请后台监控已启动", gin.H{"intervalSeconds": int(interval.Seconds())})
		for {
			h.recoverStaleApplying()
			h.scanCertificateRetry()
			h.scanCertificateApplyQueue()
			h.scanCertificateIssueProgress()
			time.Sleep(systemtask.Interval(h.db, systemtask.CertificateApplyQueueTaskKey))
		}
	}()
}

func (h *CertificateHandler) startCertificateExpiryWorker() {
	go func() {
		time.Sleep(2 * time.Second)
		delay := systemtask.Interval(h.db, systemtask.CertificateExpiryMonitorKey)
		core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_expiry_worker_start", "success", "证书到期后台监控已启动", gin.H{"nextDelaySeconds": int(delay.Seconds())})
		for {
			time.Sleep(systemtask.Interval(h.db, systemtask.CertificateExpiryMonitorKey))
			h.scanCertificateExpiry()
		}
	}()
}

func (h *CertificateHandler) startCertificateContentSyncWorker() {
	go func() {
		time.Sleep(3 * time.Second)
		core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_content_sync_worker_start", "success", "证书本地内容同步监控已启动", gin.H{"intervalSeconds": int(certificateContentSyncInterval.Seconds())})
		for {
			h.scanCertificateContentSync()
			time.Sleep(certificateContentSyncInterval)
		}
	}()
}

func (h *CertificateHandler) scanCertificateContentSync() {
	var rows []model.Certificate
	if err := h.db.
		Where("status = ? AND provider_certificate_id <> ?", certStatusIssued, "").
		Order("id ASC").
		Find(&rows).Error; err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_content_sync_scan", "failed", "扫描证书本地内容同步任务失败", gin.H{"error": err.Error()})
		return
	}
	for _, row := range rows {
		if strings.TrimSpace(row.ContentCertificateID) == "" && !certificateMaterialNeedsRefresh(row) {
			_ = h.db.Model(&model.Certificate{}).
				Where("id = ? AND content_certificate_id = ?", row.ID, "").
				Update("content_certificate_id", strings.TrimSpace(row.ProviderCertificateID)).Error
			continue
		}
		if !certificateContentNeedsRefresh(row) {
			continue
		}
		var account model.SSLAccount
		if err := h.db.First(&account, row.SSLAccountID).Error; err != nil || account.Status == model.StatusDisabled {
			continue
		}
		if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
			continue
		}
		h.enqueueCertificateContentSync(row.ID, account.ID)
	}
}

func (h *CertificateHandler) enqueueCertificateContentSync(rowID, accountID uint) {
	ctx, ok := h.claimCertificateWork(rowID)
	if !ok {
		return
	}
	go func() {
		defer h.releaseCertificateContentWork(rowID)
		var row model.Certificate
		if err := h.db.First(&row, rowID).Error; err != nil || row.Status != certStatusIssued || !certificateContentNeedsRefresh(row) {
			return
		}
		var account model.SSLAccount
		if err := h.db.First(&account, accountID).Error; err != nil {
			return
		}
		if err := h.downloadCertificateContent(ctx, &row, account); err != nil {
			if ctx.Err() == nil && h.allowProgressLog(rowID, "content_sync", err.Error()) {
				core.LogSystem(h.db, "worker", "certificate", "error", "certificate_content_sync", "failed", "同步证书本地内容失败", gin.H{"certificateId": row.ID, "error": err.Error()})
			}
			return
		}
		if err := h.db.First(&row, row.ID).Error; err != nil {
			return
		}
		if certificateContentNeedsRefresh(row) {
			core.LogSystem(h.db, "worker", "certificate", "error", "certificate_content_sync", "failed", "同步后的证书本地内容校验失败", gin.H{"certificateId": row.ID})
			return
		}
		triggerCertificateAutoDeployments(h.db, row)
		core.LogSystem(h.db, "worker", "certificate", "info", "certificate_content_sync", "success", "证书本地内容已同步并触发自动部署", gin.H{"certificateId": row.ID, "providerCertificateId": row.ProviderCertificateID})
	}()
}

func (h *CertificateHandler) scanCertificateApplyQueue() {
	var rows []model.Certificate
	err := h.db.
		Where("status IN ? AND provider_order_id = ? AND provider_certificate_id = ?", []string{certStatusQueued, certStatusPending}, "", "").
		Order("id ASC").
		Limit(10).
		Find(&rows).Error
	if err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_apply_queue_scan", "failed", "扫描证书申请任务失败", gin.H{"error": err.Error()})
		return
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if h.certificateWorkRunning(row.ID) {
			continue
		}
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_apply_queue_scan", "success", "发现待申请证书任务", gin.H{"count": len(ids), "certificateIds": ids})
	}
	for _, id := range ids {
		h.enqueueCertificateApply(id)
	}
}

func (h *CertificateHandler) scanCertificateIssueProgress() {
	var rows []model.Certificate
	if err := h.db.
		Where("status IN ? AND (provider_order_id <> ? OR provider_certificate_id <> ?)", []string{certStatusApplying, certStatusDNSAdded, certStatusSubmitted}, "", "").
		Order("id ASC").
		Find(&rows).Error; err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_issue_progress_scan", "failed", "扫描证书签发恢复任务失败", gin.H{"error": err.Error()})
		return
	}
	for _, row := range rows {
		if h.certificateWorkRunning(row.ID) {
			continue
		}
		var account model.SSLAccount
		if err := h.db.First(&account, row.SSLAccountID).Error; err != nil || account.Status == model.StatusDisabled {
			continue
		}
		if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
			continue
		}
		// ACME 的 applying 表示订单创建/添加DNS记录途中，由 recoverStaleApplying 处理中断
		if isACMEProvider(account.Provider) && row.Status == certStatusApplying {
			continue
		}
		if row.ApplyStartedAt == nil {
			now := time.Now()
			_ = h.db.Model(&model.Certificate{}).Where("id = ? AND apply_started_at IS NULL", row.ID).Update("apply_started_at", &now).Error
		} else if time.Since(*row.ApplyStartedAt) > certIssueDeadline {
			h.failTimedOutCertificate(row, account)
			continue
		}
		h.enqueueCertificateIssueProgress(row.ID, account.ID)
	}
}

func (h *CertificateHandler) enqueueCertificateIssueProgress(rowID, accountID uint) {
	ctx, ok := h.claimCertificateWork(rowID)
	if !ok {
		return
	}
	go func() {
		defer h.releaseCertificateContentWork(rowID)
		var row model.Certificate
		if err := h.db.First(&row, rowID).Error; err != nil || !isCertificateIssueInProgress(row.Status) {
			return
		}
		var account model.SSLAccount
		if err := h.db.First(&account, accountID).Error; err != nil {
			return
		}
		logErr := func(message string, err error) {
			if ctx.Err() != nil || !h.allowProgressLog(rowID, "issue_progress", err.Error()) {
				return
			}
			core.LogSystem(h.db, "worker", "certificate", "error", "certificate_issue_progress", "failed", message, gin.H{"certificateId": row.ID, "error": err.Error()})
		}
		if strings.EqualFold(account.Provider, "tencent_free") {
			sslCtx, err := h.contextWithSSLHTTPClient(ctx, account)
			if err != nil {
				logErr("恢复腾讯云证书签发流程失败", err)
				return
			}
			if row.Status == certStatusApplying && strings.TrimSpace(row.AuthRecordID) == "" {
				if _, err := h.ensureTencentAuthRecord(sslCtx, &row, account); err != nil {
					logErr("恢复腾讯云证书DNS验证记录失败", err)
					return
				}
				_ = h.db.First(&row, row.ID).Error
			}
			if _, err := h.refreshTencentCertificate(sslCtx, &row, account); err != nil {
				logErr("刷新腾讯云证书签发进度失败", err)
				return
			}
			_ = h.db.First(&row, row.ID).Error
			if row.Status == certStatusFailed {
				h.markCertificateFailed(&row, account, firstNonEmpty(row.ProviderStatusMsg, "腾讯云证书申请失败"), nil)
				return
			}
			if row.Status == certStatusDNSAdded || row.Status == certStatusApplying {
				if err := h.runTencentIssueFlow(sslCtx, &row, account); err != nil {
					logErr("恢复腾讯云证书签发流程未完成", err)
				}
			}
			return
		}
		sslCtx, err := h.contextWithSSLHTTPClient(ctx, account)
		if err != nil {
			logErr("恢复ACME证书签发流程失败", err)
			return
		}
		if _, err := h.refreshACMECertificate(sslCtx, &row, &account); err != nil {
			logErr("恢复ACME证书签发流程未完成", err)
		}
	}()
}

func isCertificateIssueInProgress(status string) bool {
	return status == certStatusApplying || status == certStatusDNSAdded || status == certStatusSubmitted
}

// isCertificateApplicationActive 申请流程是否正在进行（含排队）
func isCertificateApplicationActive(status string) bool {
	return status == certStatusQueued || isCertificateIssueInProgress(status)
}

func (h *CertificateHandler) scanCertificateExpiry() {
	var rows []model.Certificate
	// 只对已签发的证书自动续期：
	// - 失败的申请由 scanCertificateRetry 按退避时间重试，不在这里每轮重复下单；
	//   旧版本遗留的失败证书（从未安排过重试）在这里补一次续期，之后同样走退避重试；
	// - 已取消/已过期（多为从服务商导入的历史证书）不自动重新申请，需要时手动“重新申请”。
	err := h.db.
		Where(renewableCertificateCondition+" AND expires_at IS NOT NULL", certStatusIssued, certStatusFailed).
		Order("expires_at ASC").
		Find(&rows).Error
	if err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_expiry_scan", "failed", "扫描证书到期任务失败", gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	renewIDs := make([]uint, 0)
	for _, row := range rows {
		if row.ExpiresAt == nil {
			continue
		}
		renewBeforeDay := row.RenewBeforeDay
		if renewBeforeDay <= 0 {
			renewBeforeDay = 30
		}
		if row.ExpiresAt.After(now.AddDate(0, 0, renewBeforeDay)) {
			continue
		}
		var account model.SSLAccount
		if err := h.db.First(&account, row.SSLAccountID).Error; err != nil {
			core.LogSystem(h.db, "monitor", "certificate", "warning", "certificate_expiry_skip", "failed", "证书续期跳过：SSL账号不存在", gin.H{"certificateId": row.ID, "error": err.Error()})
			continue
		}
		if account.Status == model.StatusDisabled {
			core.LogSystem(h.db, "monitor", "certificate", "warning", "certificate_expiry_skip", "failed", "证书续期跳过：SSL账号已停用", gin.H{"certificateId": row.ID, "sslAccountId": account.ID})
			continue
		}
		if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
			core.LogSystem(h.db, "monitor", "certificate", "warning", "certificate_expiry_skip", "failed", "证书续期跳过：证书服务暂不支持自动申请", gin.H{"certificateId": row.ID, "provider": account.Provider})
			continue
		}
		if h.enqueueCertificateRenewal(row, account, renewBeforeDay) {
			renewIDs = append(renewIDs, row.ID)
		}
	}
	if len(renewIDs) > 0 {
		core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_expiry_scan", "success", "发现即将到期证书，已加入续期队列", gin.H{"count": len(renewIDs), "certificateIds": renewIDs})
	}
}

// renewableCertificateCondition 可自动续期：已签发，或旧版本遗留的、从未安排过重试的失败证书。
const renewableCertificateCondition = "(status = ? OR (status = ? AND retry_count = 0 AND next_retry_at IS NULL))"

func (h *CertificateHandler) enqueueCertificateRenewal(row model.Certificate, account model.SSLAccount, renewBeforeDay int) bool {
	if _, ok := h.claimCertificateWork(row.ID); !ok {
		return false
	}
	message := "证书即将到期，已进入自动续期申请队列"
	updates := certificateRequeueUpdates(message)
	updates["retry_count"] = 0
	// 只清理订单信息，cert_pem/key_pem 保留旧证书，续期签发成功前旧证书仍可部署
	result := h.db.Model(&model.Certificate{}).
		Where("id = ? AND "+renewableCertificateCondition, row.ID, certStatusIssued, certStatusFailed).
		Updates(updates)
	h.releaseCertificateContentWork(row.ID)
	if result.Error != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_expiry_enqueue", "failed", "证书续期加入队列失败", gin.H{"certificateId": row.ID, "error": result.Error.Error()})
		return false
	}
	if result.RowsAffected == 0 {
		return false
	}
	row.Status = certStatusQueued
	row.ProviderStatusMsg = message
	h.logCertificate(row, account, "apply", "success", fmt.Sprintf("证书即将到期，已自动加入续期申请队列（提前 %d 天）", renewBeforeDay), nil)
	h.enqueueCertificateApply(row.ID)
	return true
}

func (h *CertificateHandler) enqueueCertificateApply(rowID uint) {
	ctx, ok := h.claimCertificateWork(rowID)
	if !ok {
		core.LogSystem(h.db, "monitor", "certificate", "warning", "certificate_apply_enqueue_skip", "success", "证书已有后台任务在执行，跳过重复入队", gin.H{"certificateId": rowID})
		return
	}
	h.startCertificateApplyClaimed(ctx, rowID)
}

// startCertificateApplyClaimed 在调用方已占用的后台任务中执行申请与签发，结束后释放占用。
func (h *CertificateHandler) startCertificateApplyClaimed(ctx context.Context, rowID uint) {
	core.LogSystem(h.db, "monitor", "certificate", "info", "certificate_apply_enqueue", "success", "证书申请任务已进入后台执行队列", gin.H{"certificateId": rowID})

	go func() {
		defer h.releaseCertificateContentWork(rowID)
		started, accountID, err := h.runCertificateApplyFlow(ctx, rowID)
		if err != nil {
			if ctx.Err() == nil {
				core.LogSystem(h.db, "worker", "certificate", "error", "certificate_apply_execute", "failed", "证书后台申请执行失败", gin.H{"certificateId": rowID, "error": err.Error()})
			}
			return
		}
		core.LogSystem(h.db, "worker", "certificate", "info", "certificate_apply_execute", "success", "证书后台申请执行完成", gin.H{"certificateId": rowID})
		if started {
			// 继续持有同一个后台任务执行验证签发，避免与扫描任务并发处理同一张证书
			h.runCertificateIssueTask(ctx, rowID, accountID)
		}
	}()
}

// runCertificateApplyFlow 创建证书订单并添加DNS验证记录。调用方需已持有该证书的后台任务。
// 返回值 started 表示订单已创建、需要继续执行验证签发。
func (h *CertificateHandler) runCertificateApplyFlow(ctx context.Context, rowID uint) (bool, uint, error) {
	var row model.Certificate
	if err := h.db.First(&row, rowID).Error; err != nil {
		return false, 0, err
	}
	taskLog := h.startTaskLog("后台证书申请", "certificate_apply", row)
	if strings.TrimSpace(row.ProviderOrderID) != "" || strings.TrimSpace(row.ProviderCertificateID) != "" {
		h.finishTaskLog(taskLog, "success", "证书申请单已存在，跳过重复申请", taskLogDetail(row, nil, "skip_existing_order"))
		return false, 0, nil
	}
	if row.Status != certStatusQueued && row.Status != certStatusPending {
		h.finishTaskLog(taskLog, "success", "证书状态无需后台申请，已跳过", taskLogDetail(row, nil, "skip_status"))
		return false, 0, nil
	}
	now := time.Now()
	claimed := h.db.Model(&model.Certificate{}).
		Where("id = ? AND status IN ? AND provider_order_id = ? AND provider_certificate_id = ?", rowID, []string{certStatusQueued, certStatusPending}, "", "").
		Updates(map[string]any{
			"status":              certStatusApplying,
			"provider_status_msg": "后台正在创建证书申请单",
			"apply_started_at":    &now,
			"next_retry_at":       nil,
		})
	if claimed.Error != nil {
		h.finishTaskLog(taskLog, "failed", claimed.Error.Error(), taskLogDetail(row, nil, "claim_failed"))
		return false, 0, claimed.Error
	}
	if claimed.RowsAffected == 0 {
		h.finishTaskLog(taskLog, "success", "任务已被其他后台流程接管", taskLogDetail(row, nil, "already_claimed"))
		return false, 0, nil
	}
	if err := h.db.First(&row, rowID).Error; err != nil {
		h.finishTaskLog(taskLog, "failed", err.Error(), taskLogDetail(row, nil, "reload_failed"))
		return false, 0, err
	}

	var account model.SSLAccount
	if err := h.db.First(&account, row.SSLAccountID).Error; err != nil {
		h.failCertificateApply(&row, account, "SSL账号不存在："+err.Error(), nil)
		h.finishTaskLog(taskLog, "failed", "SSL账号不存在："+err.Error(), taskLogDetail(row, nil, "account_not_found"))
		return false, 0, err
	}
	if account.Status == model.StatusDisabled {
		err := fmt.Errorf("SSL账号已停用")
		h.failCertificateApply(&row, account, err.Error(), nil)
		h.finishTaskLog(taskLog, "failed", err.Error(), taskLogDetail(row, &account, "account_disabled"))
		return false, 0, err
	}
	domain, err := h.resolveCertificateDomain(&row, row.CommonName)
	if err != nil {
		h.failCertificateApply(&row, account, "域名不存在："+err.Error(), nil)
		h.finishTaskLog(taskLog, "failed", "域名不存在："+err.Error(), taskLogDetail(row, &account, "domain_not_found"))
		return false, 0, err
	}
	trace, err := h.startCertificateFlow(ctx, &row, domain, &account)
	if err != nil {
		if ctx.Err() != nil {
			// 被重新申请/删除中断：接管方会负责清理与重置状态
			h.finishTaskLog(taskLog, "success", "证书申请已被中断", taskLogDetail(row, &account, "apply_canceled"))
			return false, 0, ctx.Err()
		}
		_ = h.db.First(&row, row.ID).Error
		h.failCertificateApply(&row, account, err.Error(), trace)
		h.finishTaskLog(taskLog, "failed", err.Error(), taskLogDetail(row, &account, "apply_failed"))
		return false, 0, err
	}
	h.db.First(&row, row.ID)
	h.logCertificate(row, account, "apply", "success", "证书申请已创建，DNS验证记录已添加", trace)
	h.finishTaskLog(taskLog, "success", "证书申请已创建，DNS验证记录已添加", taskLogDetail(row, &account, "apply_success"))
	return true, account.ID, nil
}

func (h *CertificateHandler) failCertificateApply(row *model.Certificate, account model.SSLAccount, message string, trace *tencentRequestTrace) {
	h.markCertificateFailed(row, account, message, trace)
}

func (h *CertificateHandler) runCertificateIssueFlow(ctx context.Context, rowID uint, accountID uint) error {
	var row model.Certificate
	if err := h.db.First(&row, rowID).Error; err != nil {
		return err
	}
	var account model.SSLAccount
	if err := h.db.First(&account, accountID).Error; err != nil {
		return err
	}
	if !isCertificateIssueInProgress(row.Status) {
		return nil
	}
	ctx, err := h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return err
	}
	if isACMEProvider(account.Provider) {
		return h.runACMEIssueFlow(ctx, &row, &account)
	}
	if strings.EqualFold(account.Provider, "tencent_free") {
		return h.runTencentIssueFlow(ctx, &row, account)
	}
	return fmt.Errorf("暂不支持该证书服务：%s", account.Provider)
}

func (h *CertificateHandler) runTencentIssueFlow(ctx context.Context, row *model.Certificate, account model.SSLAccount) error {
	if strings.TrimSpace(row.ProviderCertificateID) == "" {
		return fmt.Errorf("证书缺少腾讯云证书ID")
	}
	trace, err := tencentCompleteCertificate(ctx, account, row.ProviderCertificateID)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h.logCertificate(*row, account, "verify", "failed", "主动触发域名验证失败："+err.Error(), traceFromError(err))
		return err
	}
	h.logCertificate(*row, account, "verify", "success", "已主动触发域名验证", trace)

	throttle := newFlowLogThrottle()
	verified := false
	err = pollCertificateFlow(ctx, func() (bool, error) {
		results, trace, err := tencentCheckCertificateDomainVerification(ctx, account, row.ProviderCertificateID)
		if err != nil {
			if ctx.Err() == nil && throttle.allow("check_failed:"+err.Error()) {
				h.logCertificate(*row, account, "verify", "failed", "检查域名验证失败："+err.Error(), traceFromError(err))
			}
			return false, nil
		}
		if tencentVerificationPassed(results) {
			h.logCertificate(*row, account, "verify", "success", "域名验证已通过", trace)
			verified = true
			return true, nil
		}
		if throttle.allow("waiting") {
			h.logCertificate(*row, account, "verify", "success", "域名验证暂未生效，继续等待", trace)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if !verified {
		_ = h.db.Model(row).Updates(map[string]any{"provider_status_msg": "域名验证暂未生效，后台将继续检查"}).Error
		return fmt.Errorf("域名验证暂未生效")
	}

	var issueErr error
	done := false
	err = pollCertificateFlow(ctx, func() (bool, error) {
		detail, trace, err := tencentDescribeCertificate(ctx, account, row.ProviderCertificateID)
		if err != nil {
			if ctx.Err() == nil && throttle.allow("status_failed:"+err.Error()) {
				h.logCertificate(*row, account, "verify", "failed", "检查签发状态失败："+err.Error(), traceFromError(err))
			}
			return false, nil
		}
		previousStatus := row.Status
		if detail != nil {
			h.applyTencentDetail(row, detail)
			_ = h.db.First(row, row.ID).Error
		}
		if row.Status != previousStatus || throttle.allow("status_checked") {
			h.logCertificate(*row, account, "verify", "success", "已检查签发状态", trace)
		}
		switch row.Status {
		case certStatusIssued:
			done = true
			if err := h.downloadTencentCertificate(ctx, row, account); err != nil {
				h.logCertificate(*row, account, "detail", "failed", "下载新证书内容失败："+err.Error(), traceFromError(err))
				issueErr = fmt.Errorf("下载新证书内容失败：%w", err)
				return true, nil
			}
			if err := h.db.First(row, row.ID).Error; err != nil {
				issueErr = err
				return true, nil
			}
			if certificateContentNeedsRefresh(*row) {
				issueErr = fmt.Errorf("新证书内容校验失败，已停止自动部署")
				return true, nil
			}
			cleaned, err := h.cleanupAuthRecord(ctx, row)
			if err != nil {
				h.logCertificate(*row, account, "cleanup", "failed", "删除DNS验证记录失败："+err.Error(), nil)
			} else if cleaned {
				h.logCertificate(*row, account, "cleanup", "success", "证书已签发，DNS验证记录已删除", nil)
			}
			h.notifyCertificateIssued(*row)
			triggerCertificateAutoDeployments(h.db, *row)
			return true, nil
		case certStatusFailed, certStatusExpired, certStatusCanceled, certStatusRevoked:
			done = true
			h.markCertificateFailed(row, account, "证书签发进入终止状态："+firstNonEmpty(row.ProviderStatusMsg, row.Status), nil)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if issueErr != nil {
		return issueErr
	}
	if !done {
		return fmt.Errorf("签发状态仍在处理中，后台将继续检查")
	}
	return nil
}

func (h *CertificateHandler) runACMEIssueFlow(ctx context.Context, row *model.Certificate, account *model.SSLAccount) error {
	client, trace, err := h.acmeClient(ctx, account)
	if err != nil {
		return err
	}
	// 订单可能已过期或被CA删除，先确认订单可用，否则直接标记失败并按退避重试，避免一直卡在验证中
	usable, reason, err := h.checkACMEOrderUsable(ctx, row, account)
	if err != nil {
		return err
	}
	if !usable {
		h.markCertificateFailed(row, *account, reason, nil)
		return nil
	}
	submitTrace, accepted, err := h.submitACMECertificate(ctx, row, account)
	if submitTrace != nil {
		trace = submitTrace
	}
	if err != nil {
		if errors.Is(err, errACMEChallengeInvalid) {
			h.markCertificateFailed(row, *account, err.Error(), trace)
			return nil
		}
		return err
	}
	if accepted {
		h.logCertificate(*row, *account, "submit", "success", "已提交ACME DNS验证，等待生效", trace)
	}
	if err := h.waitACMEAuthorizations(ctx, row, account, client); err != nil {
		if errors.Is(err, errACMEAuthorizationInvalid) {
			h.markCertificateFailed(row, *account, err.Error(), nil)
			return nil
		}
		return err
	}
	throttle := newFlowLogThrottle()
	var issueErr error
	done := false
	err = pollCertificateFlow(ctx, func() (bool, error) {
		order, err := client.GetOrder(ctx, row.ProviderOrderID)
		trace = acmeTrace(account.Provider, "GetOrder", gin.H{"orderUrl": row.ProviderOrderID}, order, err)
		if err != nil {
			if isACMEOrderUnusable(err) {
				done = true
				h.markCertificateFailed(row, *account, "ACME订单已失效或不存在："+err.Error(), trace)
				return true, nil
			}
			if ctx.Err() == nil && throttle.allow("get_order:"+err.Error()) {
				h.logCertificate(*row, *account, "verify", "failed", "检查ACME签发状态失败："+err.Error(), trace)
			}
			return false, nil
		}
		if throttle.allow("order:" + order.Status) {
			h.logCertificate(*row, *account, "verify", "success", "已检查ACME签发状态："+order.Status, trace)
		}
		if order.Status == acme.StatusReady {
			if err := h.finalizeACMECertificate(ctx, row, account, client, order); err != nil {
				issueErr = err
				return true, nil
			}
			_ = h.db.First(row, row.ID).Error
		}
		if order.Status == acme.StatusValid || row.Status == certStatusIssued {
			done = true
			certURL := firstNonEmpty(order.CertURL, row.ProviderCertificateID)
			if certURL != "" && (order.Status == acme.StatusValid || certificateContentNeedsRefresh(*row)) {
				if err := h.fetchACMECertificate(ctx, row, account, client, certURL); err != nil {
					issueErr = err
					return true, nil
				}
				_ = h.db.First(row, row.ID).Error
			}
			if certificateContentNeedsRefresh(*row) {
				issueErr = fmt.Errorf("新证书内容校验失败，已停止自动部署")
				return true, nil
			}
			cleaned, err := h.cleanupAuthRecord(ctx, row)
			if err != nil {
				h.logCertificate(*row, *account, "cleanup", "failed", "删除DNS验证记录失败："+err.Error(), nil)
			} else if cleaned {
				h.logCertificate(*row, *account, "cleanup", "success", "证书已签发，DNS验证记录已删除", nil)
			}
			h.notifyCertificateIssued(*row)
			triggerCertificateAutoDeployments(h.db, *row)
			return true, nil
		}
		if order.Status == acme.StatusInvalid {
			done = true
			h.markCertificateFailed(row, *account, "ACME订单无效"+acmeOrderErrorDetail(order), trace)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if issueErr != nil {
		return issueErr
	}
	if !done {
		return fmt.Errorf("ACME签发状态仍在处理中，后台将继续检查")
	}
	return nil
}

var (
	errACMEAuthorizationInvalid = errors.New("ACME域名验证失败")
	errACMEChallengeInvalid     = errors.New("ACME DNS验证已失效")
)

func (h *CertificateHandler) waitACMEAuthorizations(ctx context.Context, row *model.Certificate, account *model.SSLAccount, client *acme.Client) error {
	records := parseACMEAuthRecords(row.AuthRecords)
	if len(records) == 0 {
		// 所有授权在下单时已有效（CA复用了近期的验证结果），无需等待，直接检查订单
		return nil
	}
	throttle := newFlowLogThrottle()
	var invalidErr error
	allValid := false
	err := pollCertificateFlow(ctx, func() (bool, error) {
		valid := true
		for _, record := range records {
			if strings.TrimSpace(record.AuthorizationURL) == "" {
				continue
			}
			authz, err := client.GetAuthorization(ctx, record.AuthorizationURL)
			trace := acmeTrace(account.Provider, "GetAuthorization", gin.H{"authorizationUrl": record.AuthorizationURL}, authz, err)
			if err != nil {
				if ctx.Err() == nil && throttle.allow("authz_failed:"+err.Error()) {
					h.logCertificate(*row, *account, "verify", "failed", "检查ACME域名验证失败："+err.Error(), trace)
				}
				valid = false
				continue
			}
			if authz.Status == acme.StatusInvalid || authz.Status == acme.StatusDeactivated || authz.Status == acme.StatusExpired || authz.Status == acme.StatusRevoked {
				invalidErr = fmt.Errorf("%w：%s%s", errACMEAuthorizationInvalid, record.Domain, acmeAuthorizationErrorDetail(authz))
				h.logCertificate(*row, *account, "verify", "failed", invalidErr.Error(), trace)
				return true, nil
			}
			if authz.Status != acme.StatusValid {
				valid = false
			}
		}
		if valid {
			allValid = true
			return true, nil
		}
		if throttle.allow("waiting") {
			h.logCertificate(*row, *account, "verify", "success", "ACME域名验证进行中，继续等待", nil)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if invalidErr != nil {
		return invalidErr
	}
	if !allValid {
		return fmt.Errorf("ACME域名验证暂未生效，后台将继续检查")
	}
	_ = h.db.Model(row).Updates(map[string]any{"provider_status_msg": "ACME域名验证已生效"}).Error
	return nil
}

func (h *CertificateHandler) startTencentRevokeFlow(rowID uint, accountID uint) {
	ctx, ok := h.claimCertificateWork(rowID)
	if !ok {
		return
	}
	go func() {
		defer h.releaseCertificateContentWork(rowID)
		if err := h.runTencentRevokeFlow(ctx, rowID, accountID); err != nil && ctx.Err() == nil {
			var row model.Certificate
			var account model.SSLAccount
			if h.db.First(&row, rowID).Error == nil && h.db.First(&account, accountID).Error == nil {
				h.logCertificate(row, account, "revoke", "failed", "吊销自动验证流程失败："+err.Error(), nil)
			}
		}
	}()
}

func (h *CertificateHandler) runTencentRevokeFlow(ctx context.Context, rowID uint, accountID uint) error {
	var row model.Certificate
	if err := h.db.First(&row, rowID).Error; err != nil {
		return err
	}
	var account model.SSLAccount
	if err := h.db.First(&account, accountID).Error; err != nil {
		return err
	}
	ctx, err := h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ProviderCertificateID) == "" {
		return fmt.Errorf("证书缺少腾讯云证书ID")
	}
	if trace, err := h.ensureTencentRevokeAuthRecordsFromDetail(ctx, &row, account); err != nil {
		h.logCertificate(row, account, "revoke_verify", "failed", "确认吊销DNS验证记录失败："+err.Error(), trace)
		return err
	}
	trace, err := tencentCompleteCertificate(ctx, account, row.ProviderCertificateID)
	if err != nil {
		h.logCertificate(row, account, "revoke_verify", "failed", "主动触发吊销域名验证失败："+err.Error(), traceFromError(err))
		return err
	}
	h.logCertificate(row, account, "revoke_verify", "success", "已主动触发吊销域名验证", trace)

	throttle := newFlowLogThrottle()
	verified := false
	err = pollCertificateFlow(ctx, func() (bool, error) {
		results, trace, err := tencentCheckCertificateDomainVerification(ctx, account, row.ProviderCertificateID)
		if err != nil {
			if ctx.Err() == nil && throttle.allow("check_failed:"+err.Error()) {
				h.logCertificate(row, account, "revoke_check", "failed", "检查吊销域名验证失败："+err.Error(), traceFromError(err))
			}
			return false, nil
		}
		if tencentVerificationPassed(results) {
			h.logCertificate(row, account, "revoke_check", "success", "吊销域名验证已通过", trace)
			verified = true
			return true, nil
		}
		if throttle.allow("waiting") {
			h.logCertificate(row, account, "revoke_check", "success", "吊销域名验证暂未通过", trace)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if !verified {
		_ = h.db.Model(&row).Updates(map[string]any{"provider_status_msg": "吊销域名验证暂未通过，请稍后点击“验证”继续"}).Error
		return fmt.Errorf("吊销域名验证暂未通过")
	}

	revoked := false
	err = pollCertificateFlow(ctx, func() (bool, error) {
		detail, trace, err := tencentDescribeCertificate(ctx, account, row.ProviderCertificateID)
		if err != nil {
			if ctx.Err() == nil && throttle.allow("status_failed:"+err.Error()) {
				h.logCertificate(row, account, "revoke_status", "failed", "检查吊销状态失败："+err.Error(), traceFromError(err))
			}
			return false, nil
		}
		previousStatus := row.Status
		if detail != nil {
			h.applyTencentDetail(&row, detail)
			_ = h.db.First(&row, row.ID).Error
		}
		if row.Status != previousStatus || throttle.allow("status_checked") {
			h.logCertificate(row, account, "revoke_status", "success", "已检查吊销状态", trace)
		}
		if row.Status == certStatusRevoked {
			revoked = true
			cleaned, err := h.cleanupAuthRecordWithMessage(ctx, &row, "certificate_revoke_cleanup", "证书吊销完成，已删除SSL验证记录", "证书已吊销，DNS验证记录已删除")
			if err != nil {
				h.logCertificate(row, account, "cleanup", "failed", "删除吊销DNS验证记录失败："+err.Error(), nil)
			} else if cleaned {
				h.logCertificate(row, account, "cleanup", "success", "证书已吊销，DNS验证记录已删除", nil)
			}
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if !revoked {
		return fmt.Errorf("吊销状态仍在处理中")
	}
	return nil
}

func (h *CertificateHandler) createTencentRevokeAuthRecords(ctx context.Context, row *model.Certificate, auths []tencentRevokeAuth) error {
	if len(auths) == 0 {
		if len(parseACMEAuthRecords(row.AuthRecords)) > 0 {
			return nil
		}
		return fmt.Errorf("腾讯云未返回吊销DNS验证信息")
	}
	candidates := []string{row.CommonName}
	for _, auth := range auths {
		candidates = append(candidates, auth.Domain, auth.Key, auth.Path)
	}
	domain, err := h.resolveCertificateDomain(row, candidates...)
	if err != nil {
		return err
	}
	records := make([]acmeAuthRecord, 0, len(auths))
	for _, auth := range auths {
		authName := tencentRevokeAuthRecordName(auth)
		authValue := strings.TrimSpace(auth.Value)
		if authName == "" || authValue == "" {
			continue
		}
		recordID, rr, err := h.createAuthRecord(ctx, domain, authName, authValue)
		if err != nil {
			return err
		}
		records = append(records, acmeAuthRecord{
			ID:     recordID,
			Name:   rr,
			Value:  authValue,
			Domain: firstNonEmpty(auth.Domain, row.CommonName),
		})
	}
	if len(records) == 0 {
		if len(parseACMEAuthRecords(row.AuthRecords)) > 0 {
			return nil
		}
		return fmt.Errorf("腾讯云吊销DNS验证信息不完整")
	}
	rawRecords, _ := json.Marshal(records)
	updates := map[string]any{
		"auth_records":        string(rawRecords),
		"provider_status_msg": "吊销DNS验证记录已添加",
		"status":              certStatusRevoking,
	}
	if len(records) > 0 {
		updates["auth_record_id"] = records[0].ID
		updates["auth_record_name"] = records[0].Name
		updates["auth_record_value"] = records[0].Value
		updates["auth_cleanup_status"] = "pending"
	}
	h.db.Model(row).Updates(map[string]any{
		"auth_record_id":      updates["auth_record_id"],
		"auth_record_name":    updates["auth_record_name"],
		"auth_record_value":   updates["auth_record_value"],
		"auth_records":        updates["auth_records"],
		"auth_cleanup_status": updates["auth_cleanup_status"],
		"provider_status_msg": updates["provider_status_msg"],
		"status":              updates["status"],
	})
	return nil
}

func (h *CertificateHandler) ensureTencentRevokeAuthRecordsFromDetail(ctx context.Context, row *model.Certificate, account model.SSLAccount) (*tencentRequestTrace, error) {
	detail, trace, err := tencentDescribeCertificate(ctx, account, row.ProviderCertificateID)
	if err != nil {
		return trace, err
	}
	if detail != nil {
		h.applyTencentDetail(row, detail)
	}
	if detail == nil || len(detail.DvRevokeAuthDetails) == 0 {
		if len(parseACMEAuthRecords(row.AuthRecords)) > 0 {
			return trace, nil
		}
		return trace, fmt.Errorf("腾讯云未返回吊销DNS验证信息")
	}
	if err := h.createTencentRevokeAuthRecords(ctx, row, detail.DvRevokeAuthDetails); err != nil {
		return trace, err
	}
	return trace, nil
}

// submitACMECertificate 等待DNS验证记录生效后通知CA开始验证。accepted 表示本次确实提交了新的验证。
func (h *CertificateHandler) submitACMECertificate(ctx context.Context, row *model.Certificate, account *model.SSLAccount) (*tencentRequestTrace, bool, error) {
	var proxyErr error
	ctx, proxyErr = h.contextWithSSLHTTPClient(ctx, *account)
	if proxyErr != nil {
		return nil, false, proxyErr
	}
	client, trace, err := h.acmeClient(ctx, account)
	if err != nil {
		return trace, false, err
	}
	records := parseACMEAuthRecords(row.AuthRecords)
	if len(records) == 0 && strings.TrimSpace(row.AuthRecordID) != "" {
		records = append(records, acmeAuthRecord{
			ID:     row.AuthRecordID,
			Name:   row.AuthRecordName,
			Value:  row.AuthRecordValue,
			Domain: row.CommonName,
		})
	}
	pending := make([]acmeAuthRecord, 0, len(records))
	challenges := map[string]*acme.Challenge{}
	for _, record := range records {
		if strings.TrimSpace(record.ChallengeURL) == "" {
			continue
		}
		challenge, err := client.GetChallenge(ctx, record.ChallengeURL)
		trace = acmeTrace(account.Provider, "GetChallenge", gin.H{"challengeUrl": record.ChallengeURL}, challenge, err)
		if err != nil {
			if isACMEOrderUnusable(err) {
				return trace, false, fmt.Errorf("%w：%v", errACMEChallengeInvalid, err)
			}
			return trace, false, err
		}
		if challenge.Status == acme.StatusInvalid {
			return trace, false, fmt.Errorf("%w：%s%s", errACMEChallengeInvalid, record.Domain, acmeProblemDetail(challenge.Error))
		}
		if challenge.Status == acme.StatusPending {
			pending = append(pending, record)
			challenges[record.ChallengeURL] = challenge
		}
	}
	accepted := false
	if len(pending) > 0 {
		// 只在需要提交时检查DNS生效，避免CA查询到旧缓存导致验证失败
		if err := h.waitACMEDNSPropagation(ctx, row, pending); err != nil {
			return acmeTrace(account.Provider, "WaitDNS01", gin.H{"records": pending}, nil, err), false, err
		}
		for _, record := range pending {
			acceptedChallenge, err := client.Accept(ctx, challenges[record.ChallengeURL])
			trace = acmeTrace(account.Provider, "AcceptChallenge", gin.H{"challengeUrl": record.ChallengeURL}, acceptedChallenge, err)
			if err != nil {
				return trace, false, err
			}
			accepted = true
		}
	}
	if err := h.db.Model(row).Updates(map[string]any{
		"status":              certStatusSubmitted,
		"provider_status_msg": "DNS验证记录已提交，等待CA验证",
	}).Error; err != nil {
		return trace, accepted, err
	}
	return trace, accepted, nil
}

func (h *CertificateHandler) waitACMEDNSPropagation(ctx context.Context, row *model.Certificate, records []acmeAuthRecord) error {
	if len(records) == 0 {
		return nil
	}
	candidates := []string{row.CommonName}
	for _, record := range records {
		candidates = append(candidates, record.Domain, record.Name)
	}
	domain, err := h.resolveCertificateDomain(row, candidates...)
	if err != nil {
		return fmt.Errorf("无法定位证书关联域名，无法检查DNS验证记录：%w", err)
	}
	var missing []string
	for i := 0; i < acmeDNSPropagationChecks; i++ {
		if i > 0 {
			if err := sleepContext(ctx, certFlowInterval); err != nil {
				return err
			}
		}
		missing = missing[:0]
		for _, record := range records {
			name := acmeAuthRecordFQDN(record.Name, domain.Name)
			if name == "" || strings.TrimSpace(record.Value) == "" {
				continue
			}
			// 直接查询权威DNS，避免递归DNS的否定缓存导致长时间误判为未生效
			if !txtRecordPropagated(ctx, domain.Name, name, record.Value) {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			return nil
		}
	}
	return fmt.Errorf("DNS TXT验证记录暂未生效：%s", strings.Join(missing, "、"))
}

func (h *CertificateHandler) refreshCertificate(ctx context.Context, row *model.Certificate, account model.SSLAccount) (*tencentRequestTrace, error) {
	var err error
	ctx, err = h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return nil, err
	}
	if isACMEProvider(account.Provider) {
		return h.refreshACMECertificate(ctx, row, &account)
	}
	return h.refreshTencentCertificate(ctx, row, account)
}

func (h *CertificateHandler) refreshACMECertificate(ctx context.Context, row *model.Certificate, account *model.SSLAccount) (*tencentRequestTrace, error) {
	client, trace, err := h.acmeClient(ctx, account)
	if err != nil {
		return trace, err
	}
	if strings.TrimSpace(row.ProviderOrderID) == "" {
		return trace, fmt.Errorf("证书缺少ACME订单URL")
	}
	inProgress := isCertificateIssueInProgress(row.Status)
	order, err := client.GetOrder(ctx, row.ProviderOrderID)
	trace = acmeTrace(account.Provider, "GetOrder", gin.H{"orderUrl": row.ProviderOrderID}, order, err)
	if err != nil {
		if inProgress && isACMEOrderUnusable(err) {
			// 订单已过期或被CA删除，无法继续签发：标记失败并按退避自动重新申请
			h.markCertificateFailed(row, *account, "ACME订单已失效或不存在（可能已过期）："+err.Error(), trace)
			return trace, nil
		}
		return trace, err
	}
	if inProgress {
		if order.Status == acme.StatusInvalid {
			h.markCertificateFailed(row, *account, "ACME订单无效"+acmeOrderErrorDetail(order), trace)
			return trace, nil
		}
		if acmeOrderExpired(order) {
			h.markCertificateFailed(row, *account, "ACME订单已过期", trace)
			return trace, nil
		}
	}
	if order.Status == acme.StatusPending && hasACMEChallengeRecords(row) {
		submitTrace, accepted, err := h.submitACMECertificate(ctx, row, account)
		if submitTrace != nil {
			trace = submitTrace
		}
		if err != nil {
			if errors.Is(err, errACMEChallengeInvalid) {
				h.markCertificateFailed(row, *account, err.Error(), trace)
				return trace, nil
			}
			return trace, err
		}
		if accepted {
			h.logCertificate(*row, *account, "submit", "success", "已提交ACME DNS验证", trace)
		}
		order, err = client.GetOrder(ctx, row.ProviderOrderID)
		trace = acmeTrace(account.Provider, "GetOrder", gin.H{"orderUrl": row.ProviderOrderID}, order, err)
		if err != nil {
			return trace, err
		}
	}
	previousStatus := row.Status
	previousCertificateID := strings.TrimSpace(row.ProviderCertificateID)
	localIssued := isLocalACMECertificateIssued(*row, order.CertURL)
	status := acmeOrderStatus(order.Status, row.Status)
	statusMsg := "ACME订单状态：" + order.Status
	if localIssued {
		status = certStatusIssued
		statusMsg = "证书已签发，本地证书内容已保存"
	}
	if !inProgress && status == certStatusFailed {
		// 已签发/已吊销证书的历史订单不影响当前证书状态
		status = row.Status
	}
	if status == certStatusFailed {
		h.markCertificateFailed(row, *account, "ACME订单无效"+acmeOrderErrorDetail(order), trace)
		return trace, nil
	}
	updates := map[string]any{
		"provider_status":     order.Status,
		"provider_status_msg": statusMsg,
		"status":              status,
	}
	if order.CertURL != "" {
		updates["provider_certificate_id"] = order.CertURL
	}
	if err := h.db.Model(row).Updates(updates).Error; err != nil {
		return trace, err
	}
	h.db.First(row, row.ID)
	contentRefreshed := false
	if order.Status == acme.StatusReady {
		if err := h.finalizeACMECertificate(ctx, row, account, client, order); err != nil {
			return trace, err
		}
		h.db.First(row, row.ID)
		contentRefreshed = true
	}
	if order.Status == acme.StatusValid || row.Status == certStatusIssued {
		certURL := firstNonEmpty(order.CertURL, row.ProviderCertificateID)
		if certURL != "" && !contentRefreshed && (previousCertificateID != strings.TrimSpace(certURL) || certificateContentNeedsRefresh(*row)) {
			if err := h.fetchACMECertificate(ctx, row, account, client, certURL); err != nil {
				return trace, err
			}
			h.db.First(row, row.ID)
			contentRefreshed = true
		}
		if row.Status == certStatusIssued {
			if certificateContentNeedsRefresh(*row) {
				return trace, fmt.Errorf("新证书内容校验失败，已停止自动部署")
			}
			cleaned, err := h.cleanupAuthRecord(ctx, row)
			if err != nil {
				h.logCertificate(*row, *account, "cleanup", "failed", "删除DNS验证记录失败："+err.Error(), nil)
			} else if cleaned {
				h.logCertificate(*row, *account, "cleanup", "success", "证书已签发，DNS验证记录已删除", nil)
			}
			if previousStatus != certStatusIssued {
				h.notifyCertificateIssued(*row)
			}
			if previousStatus != certStatusIssued || contentRefreshed {
				triggerCertificateAutoDeployments(h.db, *row)
			}
		}
	}
	return trace, nil
}

func (h *CertificateHandler) finalizeACMECertificate(ctx context.Context, row *model.Certificate, account *model.SSLAccount, client *acme.Client, order *acme.Order) error {
	// 新订单使用 pending_key_pem；兼容旧版本直接写入 key_pem 的进行中订单
	key, err := parseRSAPrivateKeyPEM(firstNonEmpty(row.PendingKeyPEM, row.KeyPEM))
	if err != nil {
		return err
	}
	names := certificateNames(row, row.CommonName)
	csrDER, err := createCSRDER(key, names)
	if err != nil {
		return err
	}
	chain, certURL, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	trace := acmeTrace(account.Provider, "CreateOrderCert", gin.H{"finalizeUrl": order.FinalizeURL}, gin.H{"certUrl": certURL, "chainLength": len(chain)}, err)
	if err != nil {
		h.logCertificate(*row, *account, "verify", "failed", "ACME证书签发失败："+err.Error(), trace)
		return err
	}
	if err := h.saveACMECertificateChain(row, certURL, chain); err != nil {
		return err
	}
	h.logCertificate(*row, *account, "verify", "success", "ACME证书已签发并保存到本地", trace)
	return nil
}

func (h *CertificateHandler) fetchACMECertificate(ctx context.Context, row *model.Certificate, account *model.SSLAccount, client *acme.Client, certURL string) error {
	chain, err := client.FetchCert(ctx, certURL, true)
	trace := acmeTrace(account.Provider, "FetchCert", gin.H{"certUrl": certURL}, gin.H{"chainLength": len(chain)}, err)
	if err != nil {
		h.logCertificate(*row, *account, "detail", "failed", "拉取ACME证书失败："+err.Error(), trace)
		return err
	}
	if err := h.saveACMECertificateChain(row, certURL, chain); err != nil {
		return err
	}
	h.logCertificate(*row, *account, "detail", "success", "已拉取ACME证书内容到本地", trace)
	return nil
}

func (h *CertificateHandler) revokeACMECertificate(ctx context.Context, row *model.Certificate, account *model.SSLAccount) (*tencentRequestTrace, error) {
	if row.Status != certStatusIssued && row.Status != certStatusExpired {
		err := fmt.Errorf("只有已签发或已过期的ACME证书支持吊销")
		return acmeTrace(account.Provider, "RevokeCert", gin.H{"certificateId": row.ID}, nil, err), err
	}
	certDER, err := firstPEMCertificateDER(row.CertPEM)
	if err != nil {
		return acmeTrace(account.Provider, "RevokeCert", gin.H{"certificateId": row.ID}, nil, err), err
	}
	ctx, err = h.contextWithSSLHTTPClient(ctx, *account)
	if err != nil {
		return acmeTrace(account.Provider, "RevokeCert", gin.H{"certificateId": row.ID}, nil, err), err
	}
	client, trace, err := h.acmeClient(ctx, account)
	if err != nil {
		return trace, err
	}
	request := gin.H{
		"certificateId": row.ID,
		"commonName":    row.CommonName,
		"reason":        int(acme.CRLReasonUnspecified),
	}
	err = client.RevokeCert(ctx, nil, certDER, acme.CRLReasonUnspecified)
	trace = acmeTrace(account.Provider, "RevokeCert", request, gin.H{"method": "account", "revoked": err == nil}, err)
	if err == nil {
		return trace, nil
	}
	if strings.TrimSpace(row.KeyPEM) == "" {
		return trace, err
	}
	certKey, keyErr := parseRSAPrivateKeyPEM(row.KeyPEM)
	if keyErr != nil {
		return trace, err
	}
	err = client.RevokeCert(ctx, certKey, certDER, acme.CRLReasonUnspecified)
	return acmeTrace(account.Provider, "RevokeCert", request, gin.H{"method": "certificate_key", "revoked": err == nil}, err), err
}

func (h *CertificateHandler) downloadCertificateContent(ctx context.Context, row *model.Certificate, account model.SSLAccount) error {
	var err error
	ctx, err = h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return err
	}
	if isACMEProvider(account.Provider) {
		certURL := strings.TrimSpace(row.ProviderCertificateID)
		if certURL == "" {
			return fmt.Errorf("证书缺少ACME证书URL")
		}
		client, _, err := h.acmeClient(ctx, &account)
		if err != nil {
			return err
		}
		return h.fetchACMECertificate(ctx, row, &account, client, certURL)
	}
	return h.downloadTencentCertificate(ctx, row, account)
}

func (h *CertificateHandler) saveACMECertificateChain(row *model.Certificate, certURL string, chain [][]byte) error {
	if len(chain) == 0 {
		return fmt.Errorf("ACME未返回证书内容")
	}
	certPEM := strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chain[0]})))
	chainPEMs := make([]string, 0, len(chain)-1)
	for _, der := range chain[1:] {
		chainPEMs = append(chainPEMs, strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))))
	}
	// 证书与私钥必须一起替换：优先使用申请时生成的新私钥，旧数据回退到当前私钥
	keyPEM := ""
	for _, candidate := range []string{row.PendingKeyPEM, row.KeyPEM} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := parseCertificateWithMatchingKey(certPEM, candidate); err == nil {
			keyPEM = strings.TrimSpace(candidate)
			break
		}
	}
	if keyPEM == "" {
		return fmt.Errorf("ACME返回的证书与本地私钥不匹配，未保存证书内容")
	}
	updates := map[string]any{
		"provider_certificate_id": certURL,
		"content_certificate_id":  certURL,
		"provider_status":         acme.StatusValid,
		"provider_status_msg":     "证书已签发",
		"cert_pem":                certPEM,
		"key_pem":                 keyPEM,
		"pending_key_pem":         "",
		"chain_pem":               strings.TrimSpace(strings.Join(chainPEMs, "\n")),
		"status":                  certStatusIssued,
		"retry_count":             0,
		"next_retry_at":           nil,
		"apply_started_at":        nil,
	}
	if cert, err := x509.ParseCertificate(chain[0]); err == nil {
		updates["issuer"] = cert.Issuer.CommonName
		updates["serial_number"] = cert.SerialNumber.String()
		updates["expires_at"] = cert.NotAfter
	}
	return h.db.Model(row).Updates(updates).Error
}

func (h *CertificateHandler) refreshTencentCertificate(ctx context.Context, row *model.Certificate, account model.SSLAccount) (*tencentRequestTrace, error) {
	previousStatus := row.Status
	detail, trace, err := tencentDescribeCertificate(ctx, account, row.ProviderCertificateID)
	if err != nil || detail == nil {
		if err != nil {
			return trace, err
		}
		return trace, fmt.Errorf("腾讯云未返回证书详情")
	}
	h.applyTencentDetail(row, detail)
	h.db.First(row, row.ID)
	if row.Status == certStatusIssued {
		contentRefreshed := false
		if previousStatus != certStatusIssued || certificateContentNeedsRefresh(*row) {
			if err := h.downloadTencentCertificate(ctx, row, account); err != nil {
				return trace, fmt.Errorf("下载新证书内容失败：%w", err)
			}
			if err := h.db.First(row, row.ID).Error; err != nil {
				return trace, err
			}
			contentRefreshed = true
		}
		if certificateContentNeedsRefresh(*row) {
			return trace, fmt.Errorf("新证书内容校验失败，已停止自动部署")
		}
		cleaned, err := h.cleanupAuthRecord(ctx, row)
		if err != nil {
			h.logCertificate(*row, account, "cleanup", "failed", "删除DNS验证记录失败："+err.Error(), nil)
		} else if cleaned {
			h.logCertificate(*row, account, "cleanup", "success", "证书已签发，DNS验证记录已删除", nil)
		}
		if previousStatus != certStatusIssued {
			h.notifyCertificateIssued(*row)
		}
		if previousStatus != certStatusIssued || contentRefreshed {
			triggerCertificateAutoDeployments(h.db, *row)
		}
	}
	return trace, nil
}

func (h *CertificateHandler) notifyCertificateIssued(row model.Certificate) {
	content := fmt.Sprintf("证书 %s 已签发成功。", row.CommonName)
	expiresAt := ""
	if row.ExpiresAt != nil {
		expiresAt = row.ExpiresAt.Format("2006-01-02 15:04:05")
		content = fmt.Sprintf("证书 %s 已签发成功，到期时间：%s。", row.CommonName, expiresAt)
	}
	notification.DispatchAsync(h.db, notification.EventCertificateIssued, notification.Message{
		Title:   "SSL证书签发成功",
		Content: content,
		Level:   "info",
		Variables: map[string]string{
			"domain":    row.CommonName,
			"expiresAt": expiresAt,
		},
	})
}

func (h *CertificateHandler) cleanupAuthRecord(ctx context.Context, row *model.Certificate) (bool, error) {
	return h.cleanupAuthRecordWithMessage(ctx, row, "certificate_issued_cleanup", "证书签发成功，已删除SSL验证记录", "证书已签发，DNS验证记录已删除")
}

func (h *CertificateHandler) cleanupAuthRecordWithMessage(ctx context.Context, row *model.Certificate, reason, domainLogMessage, statusMessage string) (bool, error) {
	records := parseACMEAuthRecords(row.AuthRecords)
	if len(records) == 0 && strings.TrimSpace(row.AuthRecordID) != "" {
		records = append(records, acmeAuthRecord{
			ID:     row.AuthRecordID,
			Name:   row.AuthRecordName,
			Value:  row.AuthRecordValue,
			Domain: row.CommonName,
		})
	}
	if len(records) == 0 {
		return false, nil
	}
	candidates := []string{row.CommonName}
	for _, record := range records {
		candidates = append(candidates, record.Domain, record.Name)
	}
	domain, err := h.resolveCertificateDomain(row, candidates...)
	if err != nil {
		return false, fmt.Errorf("无法定位证书关联域名，无法删除DNS验证记录：%w", err)
	}
	var domainAccount model.DomainAccount
	if err := h.db.First(&domainAccount, domain.DomainAccountID).Error; err != nil {
		return false, fmt.Errorf("域名账号不存在")
	}
	claimed, err := h.claimAuthCleanup(row)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	deleted := 0
	seenRecordIDs := map[string]struct{}{}
	for _, record := range records {
		recordID := strings.TrimSpace(record.ID)
		if recordID == "" {
			continue
		}
		if _, exists := seenRecordIDs[recordID]; exists {
			continue
		}
		seenRecordIDs[recordID] = struct{}{}
		request := gin.H{
			"domainName": domain.Name,
			"recordId":   recordID,
			"rr":         record.Name,
			"type":       "TXT",
			"reason":     reason,
		}
		dnsAccount, proxyErr := domainhandler.DNSAccountWithProxy(h.db, domainAccount)
		var err error
		if proxyErr != nil {
			err = proxyErr
		} else {
			err = dnsservice.DeleteRecord(ctx, dnsAccount, domain.Name, recordID)
		}
		trace := domainhandler.DNSTrace(domainAccount, "DeleteRecord", request, gin.H{"deleted": err == nil}, err)
		if err != nil {
			if isDNSRecordNotFound(err) {
				domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "delete_record", "success", "SSL验证记录已不存在，继续清理本地状态", domainhandler.DNSTrace(domainAccount, "DeleteRecord", request, gin.H{"deleted": true, "alreadyMissing": true}, nil))
				_ = h.db.Where("domain_id = ? AND provider_record_id = ?", domain.ID, recordID).Delete(&model.DomainRecord{}).Error
				deleted++
				continue
			}
			domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "delete_record", "failed", "删除SSL验证记录失败："+err.Error(), trace)
			h.markAuthCleanupFailed(row, err.Error())
			return deleted > 0, err
		}
		domainhandler.LogDomainAccount(h.db, domainAccount, domain.ID, domain.Name, "delete_record", "success", domainLogMessage, trace)
		_ = h.db.Where("domain_id = ? AND provider_record_id = ?", domain.ID, recordID).Delete(&model.DomainRecord{}).Error
		deleted++
	}
	if deleted == 0 {
		if err := h.finishAuthCleanup(row, statusMessage); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := h.finishAuthCleanup(row, statusMessage); err != nil {
		return false, err
	}
	return true, nil
}

func (h *CertificateHandler) claimAuthCleanup(row *model.Certificate) (bool, error) {
	staleBefore := time.Now().Add(-10 * time.Minute)
	result := h.db.Model(&model.Certificate{}).
		Where("id = ? AND (auth_record_id <> ? OR auth_records <> ?) AND (COALESCE(auth_cleanup_status, '') IN ? OR (auth_cleanup_status = ? AND updated_at < ?))",
			row.ID,
			"",
			"",
			[]string{"", "pending", "failed"},
			"running",
			staleBefore,
		).
		Update("auth_cleanup_status", "running")
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	row.AuthCleanupStatus = "running"
	return true, nil
}

func (h *CertificateHandler) finishAuthCleanup(row *model.Certificate, statusMessage string) error {
	updates := map[string]any{
		"auth_record_id":      "",
		"auth_record_name":    "",
		"auth_record_value":   "",
		"auth_records":        "",
		"auth_cleanup_status": "done",
		"provider_status_msg": statusMessage,
	}
	if err := h.db.Model(row).Updates(updates).Error; err != nil {
		return err
	}
	row.AuthRecordID = ""
	row.AuthRecordName = ""
	row.AuthRecordValue = ""
	row.AuthRecords = ""
	row.AuthCleanupStatus = "done"
	row.ProviderStatusMsg = updates["provider_status_msg"].(string)
	return nil
}

func (h *CertificateHandler) markAuthCleanupFailed(row *model.Certificate, message string) {
	_ = h.db.Model(row).Updates(map[string]any{
		"auth_cleanup_status": "failed",
		"provider_status_msg": "删除DNS验证记录失败：" + truncateMessage(message, 180),
	}).Error
	row.AuthCleanupStatus = "failed"
}

func isDNSRecordNotFound(err error) bool {
	message := err.Error()
	return strings.Contains(message, "HTTP状态码：404") ||
		strings.Contains(message, `"code":81044`) ||
		strings.Contains(strings.ToLower(message), "record not found")
}

func (h *CertificateHandler) downloadTencentCertificate(ctx context.Context, row *model.Certificate, account model.SSLAccount) error {
	bundle, trace, err := tencentDownloadCertificate(ctx, account, row.ProviderCertificateID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(bundle.CertPEM) == "" || strings.TrimSpace(bundle.KeyPEM) == "" {
		return fmt.Errorf("腾讯云下载包中的证书或私钥内容不完整")
	}
	certPEM := strings.TrimSpace(bundle.CertPEM)
	keyPEM := strings.TrimSpace(bundle.KeyPEM)
	cert, err := parseCertificateWithMatchingKey(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("腾讯云下载包校验失败：%w", err)
	}
	updates := map[string]any{
		"cert_pem":               certPEM,
		"key_pem":                keyPEM,
		"chain_pem":              strings.TrimSpace(bundle.ChainPEM),
		"content_certificate_id": strings.TrimSpace(row.ProviderCertificateID),
		"issuer":                 cert.Issuer.CommonName,
		"serial_number":          cert.SerialNumber.String(),
		"expires_at":             cert.NotAfter,
		"pending_key_pem":        "",
		"retry_count":            0,
		"next_retry_at":          nil,
		"apply_started_at":       nil,
	}
	if err := h.db.Model(row).Updates(updates).Error; err != nil {
		return err
	}
	h.logCertificate(*row, account, "detail", "success", "已下载证书内容到本地", trace)
	return nil
}

func (h *CertificateHandler) applyTencentDetail(row *model.Certificate, detail *tencentDetail) {
	updates := map[string]any{}
	if detail.CertificateID != "" {
		updates["provider_certificate_id"] = detail.CertificateID
	}
	if detail.OrderID != "" {
		updates["provider_order_id"] = detail.OrderID
	}
	if detail.Issuer != "" {
		updates["issuer"] = detail.Issuer
	}
	if detail.Status != nil {
		updates["provider_status"] = strconv.FormatUint(*detail.Status, 10)
		updates["status"] = tencentCertStatus(*detail.Status, row.Status)
	}
	if detail.StatusMsg != "" {
		updates["provider_status_msg"] = detail.StatusMsg
	}
	if detail.VerifyType != "" {
		updates["verify_type"] = detail.VerifyType
	}
	if detail.CertEndTime != "" {
		if expiresAt := parseTencentTime(detail.CertEndTime); expiresAt != nil {
			updates["expires_at"] = expiresAt
		}
	}
	if detail.DvAuthDetail != nil {
		name, value := pickTencentAuthRecord(detail.DvAuthDetail)
		if name != "" {
			updates["auth_record_name"] = name
		}
		if value != "" {
			updates["auth_record_value"] = value
		}
	}
	if len(updates) > 0 {
		h.db.Model(row).Updates(updates)
	}
}

func (h *CertificateHandler) certificateAccount(c *gin.Context) (model.Certificate, model.SSLAccount, bool) {
	var row model.Certificate
	if err := h.db.First(&row, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "证书不存在")
		return row, model.SSLAccount{}, false
	}
	var account model.SSLAccount
	if err := h.db.First(&account, row.SSLAccountID).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "SSL账号不存在")
		return row, account, false
	}
	return row, account, true
}

func (h *CertificateHandler) validateRequest(c *gin.Context, req certificateRequest) (model.Domain, model.SSLAccount, string, string, bool) {
	var domain model.Domain
	if err := h.db.First(&domain, req.DomainID).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "域名不存在")
		return domain, model.SSLAccount{}, "", "", false
	}
	var account model.SSLAccount
	if err := h.db.First(&account, req.SSLAccountID).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "SSL账号不存在")
		return domain, account, "", "", false
	}
	if account.Status == model.StatusDisabled {
		core.Fail(c, http.StatusBadRequest, "SSL账号已停用")
		return domain, account, "", "", false
	}
	if strings.TrimSpace(account.Provider) == "" {
		core.Fail(c, http.StatusBadRequest, "SSL账号缺少证书服务")
		return domain, account, "", "", false
	}
	commonName := normalizeCertificateName(req.CommonName)
	if commonName == "" {
		commonName = normalizeCertificateName(domain.Name)
	}
	sans := strings.TrimSpace(req.SANs)
	names := certificateNames(&model.Certificate{CommonName: commonName, SANs: sans}, commonName)
	if err := validateCertificateNames(domain.Name, account.Provider, names); err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return domain, account, "", "", false
	}
	return domain, account, commonName, sans, true
}

func (h *CertificateHandler) resolveCertificateDomain(row *model.Certificate, candidates ...string) (model.Domain, error) {
	if row != nil && row.DomainID > 0 {
		var domain model.Domain
		if err := h.db.First(&domain, row.DomainID).Error; err == nil {
			return domain, nil
		}
	}
	if row != nil {
		candidates = append(candidates, certificateNames(row, row.CommonName)...)
	}
	domain, ok, err := h.findBestDomain(candidates...)
	if err != nil {
		return model.Domain{}, err
	}
	if !ok {
		return model.Domain{}, fmt.Errorf("证书未关联域名，且未能根据主域名匹配本地域名")
	}
	if row != nil && row.ID > 0 && row.DomainID != domain.ID {
		_ = h.db.Model(row).Update("domain_id", domain.ID).Error
		row.DomainID = domain.ID
	}
	return domain, nil
}

func (h *CertificateHandler) findBestDomain(candidates ...string) (model.Domain, bool, error) {
	names := normalizedDomainCandidates(candidates...)
	if len(names) == 0 {
		return model.Domain{}, false, nil
	}
	var domains []model.Domain
	if err := h.db.Find(&domains).Error; err != nil {
		return model.Domain{}, false, err
	}
	sort.SliceStable(domains, func(i, j int) bool {
		return len(domains[i].Name) > len(domains[j].Name)
	})
	for _, candidate := range names {
		for _, domain := range domains {
			name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain.Name, ".")))
			if name == "" {
				continue
			}
			if candidate == name || strings.HasSuffix(candidate, "."+name) {
				return domain, true, nil
			}
		}
	}
	return model.Domain{}, false, nil
}

func normalizedDomainCandidates(candidates ...string) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(candidate, ".")))
		name = strings.TrimPrefix(name, "*.")
		name = strings.TrimPrefix(name, "http://")
		name = strings.TrimPrefix(name, "https://")
		if slash := strings.Index(name, "/"); slash >= 0 {
			name = name[:slash]
		}
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func (h *CertificateHandler) activeCertificateTask(accountID uint, commonName, sans string) (model.Certificate, bool, error) {
	var rows []model.Certificate
	err := h.db.
		Where("ssl_account_id = ? AND common_name = ? AND status IN ?", accountID, strings.TrimSpace(commonName), []string{
			certStatusQueued,
			certStatusPending,
			certStatusApplying,
			certStatusDNSAdded,
			certStatusSubmitted,
		}).
		Order("id DESC").
		Limit(20).
		Find(&rows).Error
	if err != nil {
		return model.Certificate{}, false, err
	}
	// 按规范化后的域名集合比较，避免顺序、大小写或分隔符不同导致重复申请
	target := certificateNamesKey(commonName, sans)
	for _, row := range rows {
		if certificateNamesKey(row.CommonName, row.SANs) == target {
			return row, true, nil
		}
	}
	return model.Certificate{}, false, nil
}

func truncateMessage(message string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(message))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func (h *CertificateHandler) startTaskLog(taskName, taskType string, row model.Certificate) *model.TaskLog {
	now := time.Now()
	log := model.TaskLog{
		TaskName:   taskName,
		TaskType:   taskType,
		TargetID:   row.ID,
		TargetType: "certificate",
		TargetName: row.CommonName,
		Status:     "running",
		Message:    "后台任务开始执行",
		StartedAt:  &now,
		Detail:     taskLogDetail(row, nil, "start"),
	}
	if err := h.db.Create(&log).Error; err != nil {
		return nil
	}
	return &log
}

func (h *CertificateHandler) finishTaskLog(log *model.TaskLog, status, message, detail string) {
	if log == nil || log.ID == 0 {
		return
	}
	now := time.Now()
	duration := int64(0)
	if log.StartedAt != nil {
		duration = now.Sub(*log.StartedAt).Milliseconds()
	}
	_ = h.db.Model(log).Updates(map[string]any{
		"status":      status,
		"message":     message,
		"finished_at": &now,
		"duration_ms": duration,
		"detail":      detail,
	}).Error
}

func taskLogDetail(row model.Certificate, account *model.SSLAccount, phase string) string {
	payload := gin.H{
		"phase":                 phase,
		"certificateId":         row.ID,
		"domainId":              row.DomainID,
		"sslAccountId":          row.SSLAccountID,
		"commonName":            row.CommonName,
		"sans":                  row.SANs,
		"status":                row.Status,
		"providerOrderId":       row.ProviderOrderID,
		"providerCertificateId": row.ProviderCertificateID,
		"providerStatus":        row.ProviderStatus,
		"providerStatusMsg":     row.ProviderStatusMsg,
	}
	if account != nil && account.ID > 0 {
		payload["provider"] = account.Provider
		payload["sslAccountName"] = account.Name
	}
	return core.MustJSON(payload)
}

func (h *CertificateHandler) logCertificate(row model.Certificate, account model.SSLAccount, action, status, message string, trace *tencentRequestTrace) {
	log := model.CertificateLog{
		CertificateID:         row.ID,
		ProviderCertificateID: row.ProviderCertificateID,
		CommonName:            row.CommonName,
		Action:                action,
		Provider:              account.Provider,
		Status:                status,
		Message:               message,
	}
	if trace != nil {
		log.RequestURL = trace.RequestURL
		log.RequestMethod = trace.RequestMethod
		log.RequestHeaders = trace.RequestHeaders
		log.RequestBody = trace.RequestBody
		log.ResponseBody = trace.ResponseBody
	}
	_ = h.db.Create(&log).Error
}

func (h *CertificateHandler) logCertificateDeployment(row model.Certificate, account model.DeployAccount, targetName, status, message string, trace *deployRequestTrace) {
	log := model.CertificateDeployLog{
		CertificateID:   row.ID,
		DeployAccountID: account.ID,
		CommonName:      row.CommonName,
		Provider:        account.Provider,
		TargetName:      targetName,
		Status:          status,
		Message:         message,
	}
	if trace != nil {
		log.RequestURL = trace.RequestURL
		log.RequestMethod = trace.RequestMethod
		log.RequestHeaders = trace.RequestHeaders
		log.RequestBody = trace.RequestBody
		log.ResponseBody = trace.ResponseBody
	}
	_ = h.db.Create(&log).Error
}

type tencentDetail struct {
	CertificateID       string
	OrderID             string
	Issuer              string
	Status              *uint64
	StatusMsg           string
	VerifyType          string
	CertEndTime         string
	DvAuthDetail        *tencentDVAuthDetail
	DvRevokeAuthDetails []tencentRevokeAuth
}

type tencentDVAuthDetail struct {
	DvAuthKey          string              `json:"DvAuthKey"`
	DvAuthValue        string              `json:"DvAuthValue"`
	DvAuthDomain       string              `json:"DvAuthDomain"`
	DvAuthPath         string              `json:"DvAuthPath"`
	DvAuthKeySubDomain string              `json:"DvAuthKeySubDomain"`
	DvAuths            []tencentDVAuthItem `json:"DvAuths"`
}

type tencentDVAuthItem struct {
	DvAuthKey        string `json:"DvAuthKey"`
	DvAuthValue      string `json:"DvAuthValue"`
	DvAuthDomain     string `json:"DvAuthDomain"`
	DvAuthPath       string `json:"DvAuthPath"`
	DvAuthSubDomain  string `json:"DvAuthSubDomain"`
	DvAuthVerifyType string `json:"DvAuthVerifyType"`
}

type tencentRevokeAuth struct {
	Domain string `json:"domain"`
	Key    string `json:"key"`
	Value  string `json:"value"`
	Path   string `json:"path"`
}

type tencentDomainVerificationResult struct {
	Domain       string `json:"domain"`
	VerifyType   string `json:"verifyType"`
	LocalCheck   *int   `json:"localCheck"`
	CACheck      *int   `json:"caCheck"`
	CheckValue   any    `json:"checkValue"`
	FailReason   string `json:"failReason"`
	Issued       *bool  `json:"issued"`
	NeedVerify   *bool  `json:"needVerify"`
	RecordName   string `json:"recordName"`
	RecordValue  string `json:"recordValue"`
	ResourceType string `json:"resourceType"`
}

type tencentError = sslservice.TencentError
type tencentRequestTrace = sslservice.Trace

type tencentCertificateBundle struct {
	CertPEM  string
	KeyPEM   string
	ChainPEM string
}

type sslHTTPClientContextKey = sslservice.HTTPClientContextKey

type tencentTracedError struct {
	err   error
	trace *tencentRequestTrace
}

func (e *tencentTracedError) Error() string {
	return e.err.Error()
}

func (e *tencentTracedError) Unwrap() error {
	return e.err
}

func traceFromError(err error) *tencentRequestTrace {
	var traced *tencentTracedError
	if errors.As(err, &traced) {
		return traced.trace
	}
	return nil
}

func withTencentTrace(err error, trace *tencentRequestTrace) error {
	if err == nil {
		return nil
	}
	return &tencentTracedError{err: err, trace: trace}
}

func (h *CertificateHandler) contextWithSSLHTTPClient(ctx context.Context, account model.SSLAccount) (context.Context, error) {
	client, err := core.SSLHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, sslHTTPClientContextKey{}, client), nil
}

func tencentApplyCertificate(ctx context.Context, account model.SSLAccount, domain string) (string, *tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error         *tencentError `json:"Error"`
			CertificateID string        `json:"CertificateId"`
			RequestID     string        `json:"RequestId"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"DvAuthMethod":   "DNS",
		"DomainName":     domain,
		"PackageType":    "83",
		"ContactEmail":   account.Email,
		"ValidityPeriod": "3",
		"Alias":          "DaSSLm-" + domain,
	}
	trace, err := tencentSSLRequest(ctx, account, "ApplyCertificate", payload, &out)
	if err != nil {
		return "", trace, err
	}
	if err := tencentResponseErr(out.Response.Error, trace); err != nil {
		return "", trace, err
	}
	if out.Response.CertificateID == "" {
		return "", trace, fmt.Errorf("腾讯云未返回证书ID")
	}
	return out.Response.CertificateID, trace, nil
}

func tencentSimpleCertificateAction(ctx context.Context, account model.SSLAccount, action, certificateID string) (*tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error     *tencentError `json:"Error"`
			RequestID string        `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, action, map[string]any{"CertificateId": certificateID}, &out)
	if err != nil {
		return trace, err
	}
	return trace, tencentResponseErr(out.Response.Error, trace)
}

func tencentRevokeCertificate(ctx context.Context, account model.SSLAccount, certificateID, reason string) ([]tencentRevokeAuth, *tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error                     *tencentError `json:"Error"`
			RequestID                 string        `json:"RequestId"`
			RevokeDomainValidateAuths []struct {
				DomainValidateAuthDomain string `json:"DomainValidateAuthDomain"`
				DomainValidateAuthKey    string `json:"DomainValidateAuthKey"`
				DomainValidateAuthValue  string `json:"DomainValidateAuthValue"`
				DomainValidateAuthPath   string `json:"DomainValidateAuthPath"`
			} `json:"RevokeDomainValidateAuths"`
		} `json:"Response"`
	}
	payload := map[string]any{
		"CertificateId": certificateID,
	}
	if strings.TrimSpace(reason) != "" {
		payload["Reason"] = strings.TrimSpace(reason)
	}
	trace, err := tencentSSLRequest(ctx, account, "RevokeCertificate", payload, &out)
	if err != nil {
		return nil, trace, err
	}
	if err := tencentResponseErr(out.Response.Error, trace); err != nil {
		return nil, trace, err
	}
	auths := make([]tencentRevokeAuth, 0, len(out.Response.RevokeDomainValidateAuths))
	for _, item := range out.Response.RevokeDomainValidateAuths {
		auths = append(auths, tencentRevokeAuth{
			Domain: item.DomainValidateAuthDomain,
			Key:    item.DomainValidateAuthKey,
			Value:  item.DomainValidateAuthValue,
			Path:   item.DomainValidateAuthPath,
		})
	}
	return auths, trace, nil
}

func tencentCompleteCertificate(ctx context.Context, account model.SSLAccount, certificateID string) (*tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error     *tencentError `json:"Error"`
			RequestID string        `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, "CompleteCertificate", map[string]any{"CertificateId": certificateID}, &out)
	if err != nil {
		return trace, err
	}
	return trace, tencentResponseErr(out.Response.Error, trace)
}

func tencentCheckCertificateDomainVerification(ctx context.Context, account model.SSLAccount, certificateID string) ([]tencentDomainVerificationResult, *tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error        *tencentError `json:"Error"`
			RequestID    string        `json:"RequestId"`
			Verification []struct {
				Domain       string `json:"Domain"`
				VerifyType   string `json:"VerifyType"`
				LocalCheck   *int   `json:"LocalCheck"`
				CACheck      *int   `json:"CaCheck"`
				CheckValue   any    `json:"CheckValue"`
				FailReason   string `json:"FailReason"`
				Issued       *bool  `json:"Issued"`
				NeedVerify   *bool  `json:"NeedVerify"`
				RecordName   string `json:"RecordName"`
				RecordValue  string `json:"RecordValue"`
				ResourceType string `json:"ResourceType"`
			} `json:"VerificationResults"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, "CheckCertificateDomainVerification", map[string]any{"CertificateId": certificateID}, &out)
	if err != nil {
		return nil, trace, err
	}
	if err := tencentResponseErr(out.Response.Error, trace); err != nil {
		return nil, trace, err
	}
	results := make([]tencentDomainVerificationResult, 0, len(out.Response.Verification))
	for _, item := range out.Response.Verification {
		results = append(results, tencentDomainVerificationResult{
			Domain:       item.Domain,
			VerifyType:   item.VerifyType,
			LocalCheck:   item.LocalCheck,
			CACheck:      item.CACheck,
			CheckValue:   item.CheckValue,
			FailReason:   item.FailReason,
			Issued:       item.Issued,
			NeedVerify:   item.NeedVerify,
			RecordName:   item.RecordName,
			RecordValue:  item.RecordValue,
			ResourceType: item.ResourceType,
		})
	}
	return results, trace, nil
}

func tencentDescribeCertificate(ctx context.Context, account model.SSLAccount, certificateID string) (*tencentDetail, *tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error              *tencentError        `json:"Error"`
			CertificateID      string               `json:"CertificateId"`
			OrderID            string               `json:"OrderId"`
			ProductZhName      string               `json:"ProductZhName"`
			Status             *uint64              `json:"Status"`
			StatusMsg          string               `json:"StatusMsg"`
			VerifyType         string               `json:"VerifyType"`
			CertEndTime        string               `json:"CertEndTime"`
			DvAuthDetail       *tencentDVAuthDetail `json:"DvAuthDetail"`
			DvRevokeAuthDetail []struct {
				DvAuthKey        string `json:"DvAuthKey"`
				DvAuthValue      string `json:"DvAuthValue"`
				DvAuthDomain     string `json:"DvAuthDomain"`
				DvAuthPath       string `json:"DvAuthPath"`
				DvAuthSubDomain  string `json:"DvAuthSubDomain"`
				DvAuthVerifyType string `json:"DvAuthVerifyType"`
				Host             string `json:"Host"`
				Domain           string `json:"Domain"`
			} `json:"DvRevokeAuthDetail"`
			CertificatePublic string `json:"CertificatePublicKey"`
			RequestID         string `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, "DescribeCertificate", map[string]any{"CertificateId": certificateID}, &out)
	if err != nil {
		return nil, trace, err
	}
	if err := tencentResponseErr(out.Response.Error, trace); err != nil {
		return nil, trace, err
	}
	revokeAuths := make([]tencentRevokeAuth, 0, len(out.Response.DvRevokeAuthDetail))
	for _, item := range out.Response.DvRevokeAuthDetail {
		revokeAuths = append(revokeAuths, tencentRevokeAuth{
			Domain: firstNonEmpty(item.DvAuthDomain, item.Domain),
			Key:    firstNonEmpty(item.DvAuthKey, item.DvAuthSubDomain, item.Host),
			Value:  item.DvAuthValue,
			Path:   item.DvAuthPath,
		})
	}
	return &tencentDetail{
		CertificateID:       out.Response.CertificateID,
		OrderID:             out.Response.OrderID,
		Issuer:              out.Response.ProductZhName,
		Status:              out.Response.Status,
		StatusMsg:           out.Response.StatusMsg,
		VerifyType:          out.Response.VerifyType,
		CertEndTime:         out.Response.CertEndTime,
		DvAuthDetail:        out.Response.DvAuthDetail,
		DvRevokeAuthDetails: revokeAuths,
	}, trace, nil
}

func tencentDownloadCertificate(ctx context.Context, account model.SSLAccount, certificateID string) (*tencentCertificateBundle, *tencentRequestTrace, error) {
	var out struct {
		Response struct {
			Error           *tencentError `json:"Error"`
			Content         string        `json:"Content"`
			CertificateInfo string        `json:"CertificateInfo"`
			RequestID       string        `json:"RequestId"`
		} `json:"Response"`
	}
	trace, err := tencentSSLRequest(ctx, account, "DownloadCertificate", map[string]any{"CertificateId": certificateID}, &out)
	if err != nil {
		return nil, trace, err
	}
	if err := tencentResponseErr(out.Response.Error, trace); err != nil {
		return nil, trace, err
	}
	content := strings.TrimSpace(out.Response.Content)
	if content == "" {
		content = strings.TrimSpace(out.Response.CertificateInfo)
	}
	if content == "" {
		return nil, trace, withTencentTrace(fmt.Errorf("腾讯云未返回证书下载内容"), trace)
	}
	bundle, err := parseTencentCertificateContent(content)
	if err != nil {
		return nil, trace, withTencentTrace(err, trace)
	}
	return bundle, trace, nil
}

func tencentSSLRequest(ctx context.Context, account model.SSLAccount, action string, payload any, out any) (*tencentRequestTrace, error) {
	trace, err := sslservice.TencentRequest(ctx, account, action, payload, out)
	if err != nil {
		return trace, withTencentTrace(err, trace)
	}
	return trace, nil
}

func tencentResponseErr(err *tencentError, trace *tencentRequestTrace) error {
	if serviceErr := sslservice.TencentResponseErr(err, trace); serviceErr != nil {
		return withTencentTrace(serviceErr, trace)
	}
	if err == nil {
		return nil
	}
	return nil
}

func parseTencentCertificateContent(content string) (*tencentCertificateBundle, error) {
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("解码腾讯云证书下载内容失败：%w", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("读取腾讯云证书下载包失败：%w", err)
	}
	bundle := &tencentCertificateBundle{}
	seenCerts := map[string]bool{}
	chainParts := []string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		body, err := readZipFile(file)
		if err != nil {
			return nil, err
		}
		name := strings.ToLower(file.Name)
		for {
			block, rest := pem.Decode(body)
			if block == nil {
				break
			}
			pemText := string(pem.EncodeToMemory(block))
			switch {
			case strings.Contains(block.Type, "PRIVATE KEY") && bundle.KeyPEM == "":
				bundle.KeyPEM = strings.TrimSpace(pemText)
			case block.Type == "CERTIFICATE":
				if seenCerts[pemText] {
					body = rest
					continue
				}
				seenCerts[pemText] = true
				if bundle.CertPEM == "" && !looksLikeChainFile(name) {
					bundle.CertPEM = strings.TrimSpace(pemText)
				} else {
					chainParts = append(chainParts, strings.TrimSpace(pemText))
				}
			}
			body = rest
		}
	}
	if bundle.CertPEM == "" && len(chainParts) > 0 {
		bundle.CertPEM = chainParts[0]
		chainParts = chainParts[1:]
	}
	bundle.ChainPEM = strings.TrimSpace(strings.Join(chainParts, "\n"))
	return bundle, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开腾讯云证书文件失败：%w", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("读取腾讯云证书文件失败：%w", err)
	}
	return body, nil
}

func looksLikeChainFile(name string) bool {
	return strings.Contains(name, "chain") ||
		strings.Contains(name, "ca") ||
		strings.Contains(name, "root") ||
		strings.Contains(name, "bundle")
}

func (h *CertificateHandler) acmeClient(ctx context.Context, account *model.SSLAccount) (*acme.Client, *tencentRequestTrace, error) {
	key, keyPEM, err := accountPrivateKey(account.AccountKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	directoryURL := acmeDirectoryURL(*account)
	client := &acme.Client{
		Key:          key,
		DirectoryURL: directoryURL,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		UserAgent:    "DaSSLm/1.0",
	}
	if httpClient, ok := ctx.Value(sslHTTPClientContextKey{}).(*http.Client); ok && httpClient != nil {
		client.HTTPClient = httpClient
	}
	if strings.TrimSpace(account.AccountURI) != "" && strings.TrimSpace(account.AccountKeyPEM) != "" {
		client.KID = acme.KeyID(account.AccountURI)
		return client, acmeTrace(account.Provider, "Account", gin.H{"directoryUrl": directoryURL}, gin.H{"accountUri": account.AccountURI}, nil), nil
	}
	// 同一SSL账号首次注册时加锁，避免并发申请各自注册出不同的ACME账号（订单归属错乱）
	lock := acmeAccountLock(account.ID)
	lock.Lock()
	defer lock.Unlock()
	if account.ID > 0 {
		var latest model.SSLAccount
		if err := h.db.Select("id", "account_uri", "account_key_pem").First(&latest, account.ID).Error; err == nil &&
			strings.TrimSpace(latest.AccountURI) != "" && strings.TrimSpace(latest.AccountKeyPEM) != "" {
			latestKey, err := parseRSAPrivateKeyPEM(latest.AccountKeyPEM)
			if err == nil {
				account.AccountURI = latest.AccountURI
				account.AccountKeyPEM = latest.AccountKeyPEM
				client.Key = latestKey
				client.KID = acme.KeyID(latest.AccountURI)
				return client, acmeTrace(account.Provider, "Account", gin.H{"directoryUrl": directoryURL}, gin.H{"accountUri": account.AccountURI}, nil), nil
			}
		}
		if strings.TrimSpace(account.AccountKeyPEM) == "" {
			// 其他流程可能已生成并保存账号私钥但尚未拿到URI，复用该私钥
			if err := h.db.Select("id", "account_key_pem").First(&latest, account.ID).Error; err == nil && strings.TrimSpace(latest.AccountKeyPEM) != "" {
				if latestKey, err := parseRSAPrivateKeyPEM(latest.AccountKeyPEM); err == nil {
					key, keyPEM = latestKey, latest.AccountKeyPEM
					client.Key = key
				}
			}
		}
	}
	acct := &acme.Account{Contact: []string{"mailto:" + strings.TrimSpace(account.Email)}}
	if strings.TrimSpace(account.EABKid) != "" && strings.TrimSpace(account.EABHmacKey) != "" {
		eabKey, err := acmeEABHmacKey(*account)
		if err != nil {
			return nil, acmeTrace(account.Provider, "RegisterAccount", gin.H{"directoryUrl": directoryURL, "email": account.Email}, nil, err), err
		}
		acct.ExternalAccountBinding = &acme.ExternalAccountBinding{
			KID: account.EABKid,
			Key: eabKey,
		}
	}
	registered, err := client.Register(ctx, acct, acme.AcceptTOS)
	if errors.Is(err, acme.ErrAccountAlreadyExists) {
		registered, err = client.GetReg(ctx, "")
	}
	trace := acmeTrace(account.Provider, "RegisterAccount", gin.H{"directoryUrl": directoryURL, "email": account.Email}, registered, err)
	if err != nil {
		return nil, trace, err
	}
	updates := map[string]any{"account_key_pem": keyPEM}
	if registered != nil && registered.URI != "" {
		updates["account_uri"] = registered.URI
		account.AccountURI = registered.URI
		client.KID = acme.KeyID(registered.URI)
	}
	if strings.TrimSpace(account.AccountKeyPEM) == "" {
		account.AccountKeyPEM = keyPEM
	}
	if err := h.db.Model(account).Updates(updates).Error; err != nil {
		return nil, trace, err
	}
	return client, trace, nil
}

func accountPrivateKey(value string) (*rsa.PrivateKey, string, error) {
	if strings.TrimSpace(value) != "" {
		key, err := parseRSAPrivateKeyPEM(value)
		return key, value, err
	}
	return generateRSAPrivateKeyPEM()
}

func generateRSAPrivateKeyPEM() (*rsa.PrivateKey, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	raw := x509.MarshalPKCS1PrivateKey(key)
	return key, strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: raw}))), nil
}

func parseRSAPrivateKeyPEM(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("PEM私钥内容无效")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("仅支持RSA私钥")
	}
	return key, nil
}

func firstPEMCertificateDER(value string) ([]byte, error) {
	rest := []byte(strings.TrimSpace(value))
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" && len(block.Bytes) > 0 {
			return block.Bytes, nil
		}
	}
	return nil, fmt.Errorf("证书PEM内容无效")
}

func createCSRDER(key *rsa.PrivateKey, names []string) ([]byte, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("CSR域名不能为空")
	}
	req := &x509.CertificateRequest{
		Subject:  pkixName(names[0]),
		DNSNames: names,
	}
	return x509.CreateCertificateRequest(rand.Reader, req, key)
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName}
}

func acmeDirectoryURL(account model.SSLAccount) string {
	if strings.TrimSpace(account.DirectoryURL) != "" {
		return strings.TrimSpace(account.DirectoryURL)
	}
	return acme.LetsEncryptURL
}

func acmeEABHmacKey(account model.SSLAccount) ([]byte, error) {
	value := strings.TrimSpace(account.EABHmacKey)
	if value == "" {
		return nil, fmt.Errorf("EAB HMAC Key不能为空")
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) > 0 {
			return decoded, nil
		}
	}
	if strings.EqualFold(account.Provider, "litessl") {
		return nil, fmt.Errorf("LiteSSL EAB HMAC Key格式无效，请填写FreeSSL生成的EAB MAC Key")
	}
	return []byte(value), nil
}

func isACMEProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "letsencrypt" || provider == "custom_acme" || provider == "zerossl" || provider == "litessl"
}

func certificateNames(row *model.Certificate, fallback string) []string {
	items := []string{firstNonEmpty(row.CommonName, fallback)}
	text := strings.TrimSpace(row.SANs)
	if text != "" {
		var arr []string
		if err := json.Unmarshal([]byte(text), &arr); err == nil {
			items = append(items, arr...)
		} else {
			items = append(items, strings.FieldsFunc(text, func(r rune) bool {
				return r == '\n' || r == '\r' || r == ',' || r == ';'
			})...)
		}
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(item, ".")))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func pickDNS01Challenge(authz *acme.Authorization) *acme.Challenge {
	if authz == nil {
		return nil
	}
	for _, challenge := range authz.Challenges {
		if challenge != nil && challenge.Type == "dns-01" {
			return challenge
		}
	}
	return nil
}

func parseACMEAuthRecords(value string) []acmeAuthRecord {
	var records []acmeAuthRecord
	if strings.TrimSpace(value) == "" {
		return records
	}
	_ = json.Unmarshal([]byte(value), &records)
	return records
}

func hasACMEChallengeRecords(row *model.Certificate) bool {
	records := parseACMEAuthRecords(row.AuthRecords)
	if len(records) == 0 && strings.TrimSpace(row.AuthRecordID) != "" {
		return true
	}
	for _, record := range records {
		if strings.TrimSpace(record.ChallengeURL) != "" {
			return true
		}
	}
	return false
}

func acmeOrderStatus(status, fallback string) string {
	switch status {
	case acme.StatusValid:
		return certStatusIssued
	case acme.StatusInvalid:
		return certStatusFailed
	case acme.StatusReady, acme.StatusProcessing:
		return certStatusSubmitted
	case acme.StatusPending:
		return firstNonEmpty(fallback, certStatusDNSAdded)
	default:
		return firstNonEmpty(fallback, certStatusPending)
	}
}

func acmeTrace(provider, action string, request any, response any, err error) *tencentRequestTrace {
	trace := &tencentRequestTrace{
		RequestURL:     "acme://" + firstNonEmpty(provider, "letsencrypt") + "/" + action,
		RequestMethod:  "ACME",
		RequestHeaders: core.MustJSON(map[string]string{"Provider": provider}),
		RequestBody:    core.MustJSON(request),
	}
	if err != nil {
		trace.ResponseBody = core.MustJSON(map[string]string{"error": err.Error()})
	} else {
		trace.ResponseBody = core.MustJSON(response)
	}
	return trace
}

func tencentVerificationPassed(results []tencentDomainVerificationResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, item := range results {
		if item.Issued != nil {
			if !*item.Issued {
				return false
			}
			continue
		}
		if item.LocalCheck != nil && item.CACheck != nil {
			if *item.LocalCheck != 1 || *item.CACheck != 1 {
				return false
			}
			continue
		}
		if item.NeedVerify != nil && !*item.NeedVerify {
			continue
		}
		return false
	}
	return true
}

func tencentRevokeAuthRecordName(auth tencentRevokeAuth) string {
	key := strings.TrimSpace(strings.TrimSuffix(auth.Key, "."))
	domain := strings.TrimSpace(strings.TrimSuffix(auth.Domain, "."))
	if key == "" {
		return ""
	}
	if domain == "" || strings.Contains(key, ".") {
		return key
	}
	return key + "." + domain
}

func needsCertificateOrder(row model.Certificate, account model.SSLAccount) bool {
	if isACMEProvider(account.Provider) {
		return strings.TrimSpace(row.ProviderOrderID) == ""
	}
	return strings.TrimSpace(row.ProviderCertificateID) == ""
}

func canRequeueCertificateApplication(status string) bool {
	switch status {
	case certStatusFailed, certStatusExpired, certStatusCanceled:
		return true
	default:
		return false
	}
}

func shouldRefreshCertificate(row model.Certificate, account model.SSLAccount) bool {
	if isACMEProvider(account.Provider) {
		return strings.TrimSpace(row.ProviderOrderID) != ""
	}
	return strings.EqualFold(account.Provider, "tencent_free") && strings.TrimSpace(row.ProviderCertificateID) != ""
}

func isTruthyQuery(value string) bool {
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func isLocalACMECertificateIssued(row model.Certificate, certURL string) bool {
	certURL = strings.TrimSpace(certURL)
	return certURL != "" &&
		strings.TrimSpace(row.ProviderCertificateID) == certURL &&
		!certificateContentNeedsRefresh(row)
}

func certificateContentNeedsRefresh(row model.Certificate) bool {
	providerCertificateID := strings.TrimSpace(row.ProviderCertificateID)
	if providerCertificateID != "" && strings.TrimSpace(row.ContentCertificateID) != providerCertificateID {
		return true
	}
	return certificateMaterialNeedsRefresh(row)
}

func certificateMaterialNeedsRefresh(row model.Certificate) bool {
	cert, err := parseCertificateWithMatchingKey(row.CertPEM, row.KeyPEM)
	if err != nil {
		return true
	}
	if row.ExpiresAt != nil {
		delta := cert.NotAfter.Sub(*row.ExpiresAt)
		if delta < 0 {
			delta = -delta
		}
		if delta > 5*time.Minute {
			return true
		}
	}
	if serialNumber := strings.TrimSpace(row.SerialNumber); serialNumber != "" && cert.SerialNumber.String() != serialNumber {
		return true
	}
	return false
}

func parseCertificateWithMatchingKey(certPEM, keyPEM string) (*x509.Certificate, error) {
	certDER, err := firstPEMCertificateDER(certPEM)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	key, err := parseRSAPrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	publicKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey.E != key.PublicKey.E || publicKey.N.Cmp(key.PublicKey.N) != 0 {
		return nil, fmt.Errorf("证书与私钥不匹配")
	}
	return cert, nil
}

func verifyLogMessage(account model.SSLAccount) string {
	if isACMEProvider(account.Provider) {
		return "已提交并检查ACME验证状态"
	}
	return "已刷新证书状态"
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func pickTencentAuthRecord(detail *tencentDVAuthDetail) (string, string) {
	if detail == nil {
		return "", ""
	}
	for _, item := range detail.DvAuths {
		name := firstNonEmpty(item.DvAuthSubDomain, item.DvAuthKey, item.DvAuthDomain)
		value := strings.TrimSpace(item.DvAuthValue)
		if name != "" && value != "" {
			return name, value
		}
	}
	return firstNonEmpty(detail.DvAuthKeySubDomain, detail.DvAuthKey, detail.DvAuthDomain), strings.TrimSpace(detail.DvAuthValue)
}

func normalizeAuthRR(name, domainName string) string {
	rr := strings.TrimSpace(strings.TrimSuffix(name, "."))
	domain := strings.TrimSpace(strings.TrimSuffix(domainName, "."))
	if rr == "" || rr == domain {
		return "@"
	}
	suffix := "." + domain
	if strings.HasSuffix(rr, suffix) {
		rr = strings.TrimSuffix(rr, suffix)
	}
	if rr == "" {
		return "@"
	}
	return rr
}

func acmeAuthRecordFQDN(rr, domainName string) string {
	name := strings.TrimSpace(strings.TrimSuffix(rr, "."))
	domain := strings.TrimSpace(strings.TrimSuffix(domainName, "."))
	if name == "" {
		return ""
	}
	if name == "@" {
		return domain
	}
	if domain == "" || strings.HasSuffix(name, "."+domain) || name == domain {
		return name
	}
	return name + "." + domain
}

func containsTXTValue(values []string, want string) bool {
	want = strings.Trim(strings.TrimSpace(want), `"`)
	for _, value := range values {
		if strings.Trim(strings.TrimSpace(value), `"`) == want {
			return true
		}
	}
	return false
}

func tencentCertStatus(status uint64, fallback string) string {
	return sslservice.TencentCertStatus(status, fallback)
}

func parseTencentTime(value string) *time.Time {
	return sslservice.ParseTencentTime(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
