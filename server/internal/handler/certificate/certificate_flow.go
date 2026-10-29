package certificate

import (
	"context"
	core "dasslm/server/internal/handler"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/acme"
)

const (
	// certFlowPollTimeout 单次后台验证/签发流程最长轮询时间，超时后由后台扫描继续跟进。
	certFlowPollTimeout = 20 * time.Minute
	// certIssueDeadline 一轮申请从开始到签发的最长时间，超过后标记失败并按退避重试。
	certIssueDeadline = 72 * time.Hour
	// certApplyStaleAfter 处于 applying（创建订单中）超过该时间且没有后台任务时，视为中断。
	certApplyStaleAfter = 10 * time.Minute
	// certRetryMaxCount 申请失败后的最大自动重试次数，退避间隔 1h、2h、4h…，最长 24h。
	certRetryMaxCount  = 6
	certRetryBaseDelay = time.Hour
	certRetryMaxDelay  = 24 * time.Hour
	// certFlowWaitLogInterval 轮询过程中相同的“继续等待/检查失败”日志最多每隔该时间记录一次。
	certFlowWaitLogInterval = 10 * time.Minute
	// certWorkCancelWait 重新申请/删除时等待正在执行的后台任务退出的时间。
	certWorkCancelWait = 8 * time.Second
	// acmeDNSPropagationChecks 提交ACME验证前检查TXT记录生效的次数（间隔 certFlowInterval）。
	acmeDNSPropagationChecks = 18
)

// ---------------- 后台任务互斥 ----------------

type certificateWork struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// claimCertificateWork 为证书占用后台任务，返回的 context 会在任务被接管（重新申请/删除）时取消。
func (h *CertificateHandler) claimCertificateWork(rowID uint) (context.Context, bool) {
	h.contentSyncMu.Lock()
	defer h.contentSyncMu.Unlock()
	if h.contentSyncInFlight == nil {
		h.contentSyncInFlight = map[uint]*certificateWork{}
	}
	if _, exists := h.contentSyncInFlight[rowID]; exists {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.contentSyncInFlight[rowID] = &certificateWork{cancel: cancel, done: make(chan struct{})}
	return ctx, true
}

func (h *CertificateHandler) claimCertificateContentWork(rowID uint) bool {
	_, ok := h.claimCertificateWork(rowID)
	return ok
}

func (h *CertificateHandler) releaseCertificateContentWork(rowID uint) {
	h.contentSyncMu.Lock()
	work := h.contentSyncInFlight[rowID]
	delete(h.contentSyncInFlight, rowID)
	h.contentSyncMu.Unlock()
	if work != nil {
		work.cancel()
		close(work.done)
	}
}

func (h *CertificateHandler) certificateWorkRunning(rowID uint) bool {
	h.contentSyncMu.Lock()
	defer h.contentSyncMu.Unlock()
	_, exists := h.contentSyncInFlight[rowID]
	return exists
}

// takeoverCertificateWork 取消证书正在执行的后台任务并等待其退出，然后由调用方占用。
// 成功时必须调用 release，或把 ctx 交给 startCertificateApplyClaimed 由其释放。
func (h *CertificateHandler) takeoverCertificateWork(rowID uint, wait time.Duration) (context.Context, func(), bool) {
	deadline := time.Now().Add(wait)
	for {
		if ctx, ok := h.claimCertificateWork(rowID); ok {
			return ctx, func() { h.releaseCertificateContentWork(rowID) }, true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil, false
		}
		h.contentSyncMu.Lock()
		work := h.contentSyncInFlight[rowID]
		h.contentSyncMu.Unlock()
		if work == nil {
			continue
		}
		work.cancel()
		select {
		case <-work.done:
		case <-time.After(remaining):
			return nil, nil, false
		}
	}
}

// ---------------- 轮询与日志节流 ----------------

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// certFlowPollDelay 轮询退避：5s、10s、15s、20s、30s、45s，之后每 60s 一次。
func certFlowPollDelay(attempt int) time.Duration {
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second, 20 * time.Second, 30 * time.Second, 45 * time.Second}
	if attempt <= 0 {
		return 0
	}
	if attempt <= len(delays) {
		return delays[attempt-1]
	}
	return time.Minute
}

// pollCertificateFlow 反复执行 check 直到返回 done、出错、超时（certFlowPollTimeout）或 ctx 被取消。
// 超时不返回错误，调用方根据自身状态判断是否完成。
func pollCertificateFlow(ctx context.Context, check func() (bool, error)) error {
	deadline := time.Now().Add(certFlowPollTimeout)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if time.Now().After(deadline) {
				return nil
			}
			if err := sleepContext(ctx, certFlowPollDelay(attempt)); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := check()
		if err != nil || done {
			return err
		}
	}
}

type flowLogThrottle struct {
	last map[string]time.Time
}

func newFlowLogThrottle() *flowLogThrottle {
	return &flowLogThrottle{last: map[string]time.Time{}}
}

func (t *flowLogThrottle) allow(key string) bool {
	now := time.Now()
	if last, ok := t.last[key]; ok && now.Sub(last) < certFlowWaitLogInterval {
		return false
	}
	t.last[key] = now
	return true
}

// allowProgressLog 跨轮次节流：同一证书同一类错误最多每 certFlowWaitLogInterval 记录一次。
func (h *CertificateHandler) allowProgressLog(rowID uint, action, message string) bool {
	h.progressLogMu.Lock()
	defer h.progressLogMu.Unlock()
	if h.progressLogAt == nil {
		h.progressLogAt = map[string]time.Time{}
	}
	now := time.Now()
	for key, at := range h.progressLogAt {
		if now.Sub(at) > certFlowWaitLogInterval {
			delete(h.progressLogAt, key)
		}
	}
	key := strconv.FormatUint(uint64(rowID), 10) + "|" + action + "|" + message
	if _, exists := h.progressLogAt[key]; exists {
		return false
	}
	h.progressLogAt[key] = now
	return true
}

// ---------------- 失败、重试与重新申请 ----------------

func certificateRetryDelay(retryCount int) time.Duration {
	delay := certRetryBaseDelay
	for i := 1; i < retryCount; i++ {
		delay *= 2
		if delay >= certRetryMaxDelay {
			return certRetryMaxDelay
		}
	}
	return delay
}

// markCertificateFailed 将证书标记为失败：清理DNS验证记录、丢弃未签发的新私钥，并安排退避重试。
// 旧证书内容（cert_pem/key_pem）保持不变。
func (h *CertificateHandler) markCertificateFailed(row *model.Certificate, account model.SSLAccount, message string, trace *tencentRequestTrace) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if account.ID > 0 {
		if sslCtx, err := h.contextWithSSLHTTPClient(cleanupCtx, account); err == nil {
			cleanupCtx = sslCtx
		}
	}
	if _, err := h.cleanupAuthRecordWithMessage(cleanupCtx, row, "certificate_failed_cleanup", "证书申请失败，已删除SSL验证记录", message); err != nil {
		core.LogSystem(h.db, "worker", "certificate", "warning", "certificate_failed_cleanup", "failed", "证书申请失败后清理DNS验证记录失败", gin.H{"certificateId": row.ID, "error": err.Error()})
	}
	retryCount := row.RetryCount + 1
	var nextRetryAt any
	statusMsg := message
	if retryCount <= certRetryMaxCount {
		next := time.Now().Add(certificateRetryDelay(retryCount))
		nextRetryAt = &next
		statusMsg = fmt.Sprintf("%s（将于 %s 自动重试）", truncateMessage(message, 200), next.Format("01-02 15:04"))
	} else {
		statusMsg = truncateMessage(message, 200) + "（已达自动重试上限，请手动重新申请）"
	}
	updates := map[string]any{
		"status":              certStatusFailed,
		"provider_status_msg": truncateMessage(statusMsg, 250),
		"retry_count":         retryCount,
		"next_retry_at":       nextRetryAt,
		"pending_key_pem":     "",
		"apply_started_at":    nil,
	}
	if err := h.db.Model(row).Updates(updates).Error; err != nil {
		core.LogSystem(h.db, "worker", "certificate", "error", "certificate_mark_failed", "failed", "保存证书失败状态失败", gin.H{"certificateId": row.ID, "error": err.Error()})
	}
	row.Status = certStatusFailed
	row.RetryCount = retryCount
	row.PendingKeyPEM = ""
	row.ProviderStatusMsg = truncateMessage(statusMsg, 250)
	if account.ID > 0 {
		h.logCertificate(*row, account, "apply", "failed", message, trace)
	}
}

// certificateRequeueUpdates 重新申请需要清空的订单与验证信息；证书内容保持不变。
func certificateRequeueUpdates(message string) map[string]any {
	return map[string]any{
		"status":                  certStatusQueued,
		"provider_certificate_id": "",
		"provider_order_id":       "",
		"provider_status":         "",
		"provider_status_msg":     message,
		"auth_record_id":          "",
		"auth_record_name":        "",
		"auth_record_value":       "",
		"auth_records":            "",
		"auth_cleanup_status":     "",
		"pending_key_pem":         "",
		"apply_started_at":        nil,
		"next_retry_at":           nil,
	}
}

func tencentOrderCancelable(status string) bool {
	switch status {
	case certStatusApplying, certStatusDNSAdded, certStatusSubmitted, certStatusFailed:
		return true
	default:
		return false
	}
}

// cancelTencentOrderBestEffort 取消未完成的腾讯云证书订单，避免残留订单占用免费证书额度。
func (h *CertificateHandler) cancelTencentOrderBestEffort(ctx context.Context, row *model.Certificate, account model.SSLAccount) {
	if !strings.EqualFold(account.Provider, "tencent_free") || strings.TrimSpace(row.ProviderCertificateID) == "" || !tencentOrderCancelable(row.Status) {
		return
	}
	sslCtx, err := h.contextWithSSLHTTPClient(ctx, account)
	if err != nil {
		return
	}
	trace, err := tencentSimpleCertificateAction(sslCtx, account, "CancelCertificateOrder", row.ProviderCertificateID)
	if err != nil {
		h.logCertificate(*row, account, "cancel", "failed", "取消旧的腾讯云证书订单失败（已忽略）："+err.Error(), traceFromError(err))
		return
	}
	h.logCertificate(*row, account, "cancel", "success", "已取消旧的腾讯云证书订单", trace)
}

// requeueCertificateApplication 清理旧订单与DNS验证记录后重新进入申请队列。调用方需持有该证书的后台任务。
func (h *CertificateHandler) requeueCertificateApplication(ctx context.Context, row *model.Certificate, account model.SSLAccount, message string, resetRetry bool) error {
	h.cancelTencentOrderBestEffort(ctx, row, account)
	cleanupCtx := ctx
	if sslCtx, err := h.contextWithSSLHTTPClient(ctx, account); err == nil {
		cleanupCtx = sslCtx
	}
	if _, err := h.cleanupAuthRecordWithMessage(cleanupCtx, row, "certificate_requeue_cleanup", "证书重新申请，已删除旧DNS验证记录", message); err != nil {
		core.LogSystem(h.db, "worker", "certificate", "warning", "certificate_requeue_cleanup", "failed", "证书重新申请前清理旧DNS验证记录失败，继续重新申请", gin.H{"certificateId": row.ID, "error": err.Error()})
	}
	updates := certificateRequeueUpdates(message)
	if resetRetry {
		updates["retry_count"] = 0
	}
	return h.db.Model(row).Updates(updates).Error
}

// scanCertificateRetry 到达重试时间的失败证书重新进入申请队列。
func (h *CertificateHandler) scanCertificateRetry() {
	var rows []model.Certificate
	now := time.Now()
	if err := h.db.
		Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?", certStatusFailed, now).
		Order("next_retry_at ASC").
		Limit(20).
		Find(&rows).Error; err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_retry_scan", "failed", "扫描证书重试任务失败", gin.H{"error": err.Error()})
		return
	}
	for _, row := range rows {
		var account model.SSLAccount
		if err := h.db.First(&account, row.SSLAccountID).Error; err != nil || account.Status == model.StatusDisabled {
			continue
		}
		if !strings.EqualFold(account.Provider, "tencent_free") && !isACMEProvider(account.Provider) {
			continue
		}
		workCtx, ok := h.claimCertificateWork(row.ID)
		if !ok {
			continue
		}
		if err := h.db.First(&row, row.ID).Error; err != nil || row.Status != certStatusFailed || row.NextRetryAt == nil || row.NextRetryAt.After(time.Now()) {
			h.releaseCertificateContentWork(row.ID)
			continue
		}
		message := fmt.Sprintf("第 %d 次自动重试，已重新进入申请队列", row.RetryCount)
		if err := h.requeueCertificateApplication(workCtx, &row, account, message, false); err != nil {
			h.releaseCertificateContentWork(row.ID)
			core.LogSystem(h.db, "worker", "certificate", "error", "certificate_retry", "failed", "证书自动重试加入队列失败", gin.H{"certificateId": row.ID, "error": err.Error()})
			continue
		}
		_ = h.db.First(&row, row.ID).Error
		h.logCertificate(row, account, "apply", "success", message, nil)
		h.startCertificateApplyClaimed(workCtx, row.ID)
	}
}

// recoverStaleApplying 处理“创建订单中”被中断（服务重启、异常退出）的证书，避免永久卡在 applying。
func (h *CertificateHandler) recoverStaleApplying() {
	var rows []model.Certificate
	if err := h.db.
		Where("status = ? AND updated_at < ?", certStatusApplying, time.Now().Add(-certApplyStaleAfter)).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		core.LogSystem(h.db, "monitor", "certificate", "error", "certificate_stale_apply_scan", "failed", "扫描中断的证书申请失败", gin.H{"error": err.Error()})
		return
	}
	for _, row := range rows {
		var account model.SSLAccount
		if err := h.db.First(&account, row.SSLAccountID).Error; err != nil {
			continue
		}
		// 腾讯云订单已创建时由签发进度扫描补全DNS记录继续签发
		if strings.EqualFold(account.Provider, "tencent_free") && strings.TrimSpace(row.ProviderCertificateID) != "" {
			continue
		}
		workCtx, ok := h.claimCertificateWork(row.ID)
		if !ok {
			continue
		}
		if err := h.db.First(&row, row.ID).Error; err != nil || row.Status != certStatusApplying {
			h.releaseCertificateContentWork(row.ID)
			continue
		}
		message := "上次申请被中断（服务重启或异常退出），已自动重新进入申请队列"
		if err := h.requeueCertificateApplication(workCtx, &row, account, message, false); err != nil {
			h.releaseCertificateContentWork(row.ID)
			core.LogSystem(h.db, "worker", "certificate", "error", "certificate_stale_apply", "failed", "恢复中断的证书申请失败", gin.H{"certificateId": row.ID, "error": err.Error()})
			continue
		}
		_ = h.db.First(&row, row.ID).Error
		h.logCertificate(row, account, "apply", "success", message, nil)
		h.startCertificateApplyClaimed(workCtx, row.ID)
	}
}

// failTimedOutCertificate 超过 certIssueDeadline 仍未签发的证书标记失败。
func (h *CertificateHandler) failTimedOutCertificate(row model.Certificate, account model.SSLAccount) {
	if _, ok := h.claimCertificateWork(row.ID); !ok {
		return
	}
	defer h.releaseCertificateContentWork(row.ID)
	if err := h.db.First(&row, row.ID).Error; err != nil || !isCertificateIssueInProgress(row.Status) {
		return
	}
	h.cancelTencentOrderBestEffort(context.Background(), &row, account)
	h.markCertificateFailed(&row, account, fmt.Sprintf("超过 %d 小时仍未完成签发，已停止本轮申请", int(certIssueDeadline.Hours())), nil)
}

// cancelLocalApplication 取消进行中的申请：ACME停用验证授权，清理DNS记录并标记为已取消。
func (h *CertificateHandler) cancelLocalApplication(c *gin.Context, row model.Certificate, account model.SSLAccount) {
	if row.Status == certStatusCanceled || row.Status == certStatusRevoked {
		core.Ok(c, row)
		return
	}
	_, release, ok := h.takeoverCertificateWork(row.ID, certWorkCancelWait)
	if !ok {
		core.Fail(c, http.StatusConflict, "后台任务正在处理该证书，请稍后再试")
		return
	}
	defer release()
	_ = h.db.First(&row, row.ID).Error
	ctx, err := h.contextWithSSLHTTPClient(c.Request.Context(), account)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if records := parseACMEAuthRecords(row.AuthRecords); len(records) > 0 && isACMEProvider(account.Provider) {
		if client, _, err := h.acmeClient(ctx, &account); err == nil {
			for _, record := range records {
				if strings.TrimSpace(record.AuthorizationURL) != "" {
					_ = client.RevokeAuthorization(ctx, record.AuthorizationURL)
				}
			}
		}
	}
	if _, err := h.cleanupAuthRecordWithMessage(ctx, &row, "certificate_cancel_cleanup", "证书申请已取消，已删除SSL验证记录", "已取消证书申请"); err != nil {
		h.logCertificate(row, account, "cleanup", "failed", "删除DNS验证记录失败："+err.Error(), nil)
	}
	if err := h.db.Model(&row).Updates(map[string]any{
		"status":              certStatusCanceled,
		"provider_status_msg": "已取消证书申请",
		"provider_order_id":   "",
		"provider_status":     "",
		"pending_key_pem":     "",
		"next_retry_at":       nil,
		"apply_started_at":    nil,
	}).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存证书状态失败")
		return
	}
	_ = h.db.First(&row, row.ID).Error
	h.logCertificate(row, account, "revoke", "success", "已取消证书申请", nil)
	core.Ok(c, row)
}

// List 证书列表，keyword 按主域名与备用域名搜索（通用列表按 name/remark 搜索，证书表没有这两列）。
func (h *CertificateHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := h.db.Model(&model.Certificate{})
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("common_name LIKE ? OR sans LIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询总数失败")
		return
	}
	var rows []model.Certificate
	if err := query.Order("id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询列表失败")
		return
	}
	core.Ok(c, gin.H{"items": rows, "total": total, "page": page, "pageSize": pageSize})
}

// Delete 删除证书前停止后台任务、取消未完成的腾讯云订单并清理DNS验证记录。
func (h *CertificateHandler) Delete(c *gin.Context) {
	var row model.Certificate
	if err := h.db.First(&row, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "证书不存在")
		return
	}
	_, release, ok := h.takeoverCertificateWork(row.ID, certWorkCancelWait)
	if !ok {
		core.Fail(c, http.StatusConflict, "后台任务正在处理该证书，请稍后再试")
		return
	}
	defer release()
	_ = h.db.First(&row, row.ID).Error
	var account model.SSLAccount
	if err := h.db.First(&account, row.SSLAccountID).Error; err == nil {
		h.cancelTencentOrderBestEffort(c.Request.Context(), &row, account)
		ctx := c.Request.Context()
		if sslCtx, err := h.contextWithSSLHTTPClient(ctx, account); err == nil {
			ctx = sslCtx
		}
		if _, err := h.cleanupAuthRecordWithMessage(ctx, &row, "certificate_delete_cleanup", "证书已删除，已删除SSL验证记录", "证书已删除"); err != nil {
			core.LogSystem(h.db, "worker", "certificate", "warning", "certificate_delete_cleanup", "failed", "删除证书时清理DNS验证记录失败，请手动删除", gin.H{"certificateId": row.ID, "error": err.Error()})
		}
	}
	if err := h.db.Delete(&model.Certificate{}, row.ID).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	core.Ok(c, gin.H{"deleted": true})
}

// ---------------- ACME 订单状态 ----------------

// isACMEOrderUnusable 订单（或其下的授权/验证）在CA侧已不存在或不属于当前账号，无法继续使用。
func isACMEOrderUnusable(err error) bool {
	var acmeErr *acme.Error
	if !errors.As(err, &acmeErr) {
		return false
	}
	if acmeErr.StatusCode == http.StatusNotFound {
		return true
	}
	switch acmeErr.ProblemType {
	case "urn:ietf:params:acme:error:accountDoesNotExist", "urn:ietf:params:acme:error:unauthorized":
		return true
	}
	return false
}

func acmeOrderExpired(order *acme.Order) bool {
	return order != nil && !order.Expires.IsZero() && order.Expires.Before(time.Now()) &&
		order.Status != acme.StatusValid
}

// checkACMEOrderUsable 检查进行中的ACME订单是否还能继续签发。
func (h *CertificateHandler) checkACMEOrderUsable(ctx context.Context, row *model.Certificate, account *model.SSLAccount) (bool, string, error) {
	if strings.TrimSpace(row.ProviderOrderID) == "" {
		return false, "ACME订单不存在", nil
	}
	client, _, err := h.acmeClient(ctx, account)
	if err != nil {
		return false, "", err
	}
	order, err := client.GetOrder(ctx, row.ProviderOrderID)
	if err != nil {
		if isACMEOrderUnusable(err) {
			return false, "ACME订单已失效或不存在（可能已过期）", nil
		}
		return false, "", err
	}
	if order.Status == acme.StatusInvalid {
		return false, "ACME订单无效" + acmeOrderErrorDetail(order), nil
	}
	if acmeOrderExpired(order) {
		return false, "ACME订单已过期", nil
	}
	return true, "", nil
}

func acmeProblemDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := strings.TrimSpace(err.Error())
	var acmeErr *acme.Error
	if errors.As(err, &acmeErr) && acmeErr != nil {
		detail = firstNonEmpty(acmeErr.Detail, acmeErr.ProblemType, detail)
	}
	if detail == "" {
		return ""
	}
	return "（" + truncateMessage(detail, 120) + "）"
}

func acmeOrderErrorDetail(order *acme.Order) string {
	if order == nil || order.Error == nil {
		return ""
	}
	return acmeProblemDetail(order.Error)
}

func acmeAuthorizationErrorDetail(authz *acme.Authorization) string {
	if authz == nil {
		return ""
	}
	for _, challenge := range authz.Challenges {
		if challenge != nil && challenge.Error != nil {
			return acmeProblemDetail(challenge.Error)
		}
	}
	return ""
}

var acmeAccountLocks sync.Map

func acmeAccountLock(accountID uint) *sync.Mutex {
	value, _ := acmeAccountLocks.LoadOrStore(accountID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// ---------------- DNS 生效检查 ----------------

// txtRecordPropagated 向域名的权威DNS查询TXT记录，所有有响应的权威DNS都返回目标值才算生效。
// 无法获取或连接权威DNS时回退到系统解析器。
func txtRecordPropagated(ctx context.Context, zone, fqdn, value string) bool {
	zone = strings.TrimSuffix(strings.TrimSpace(zone), ".")
	fqdn = strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	nsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	nsRecords, err := net.DefaultResolver.LookupNS(nsCtx, zone)
	cancel()
	responded := 0
	if err == nil {
		for _, ns := range nsRecords {
			host := strings.TrimSuffix(strings.TrimSpace(ns.Host), ".")
			if host == "" {
				continue
			}
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			values, err := authoritativeResolver(host).LookupTXT(lookupCtx, fqdn+".")
			cancel()
			if err != nil {
				var dnsErr *net.DNSError
				if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
					return false
				}
				continue
			}
			responded++
			if !containsTXTValue(values, value) {
				return false
			}
		}
	}
	if responded > 0 {
		return true
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	values, err := net.DefaultResolver.LookupTXT(lookupCtx, fqdn)
	return err == nil && containsTXTValue(values, value)
}

func authoritativeResolver(host string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, "53"))
		},
	}
}

// ---------------- 域名校验 ----------------

func normalizeCertificateName(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

// validateCertificateNames 校验证书域名都属于所选域名（DNS验证记录只能添加到该域名下），
// 并检查证书服务的限制。
func validateCertificateNames(domainName, provider string, names []string) error {
	zone := normalizeCertificateName(domainName)
	if zone == "" {
		return fmt.Errorf("关联域名为空")
	}
	if len(names) == 0 {
		return fmt.Errorf("证书域名不能为空")
	}
	wildcard := false
	for _, name := range names {
		bare := strings.TrimPrefix(name, "*.")
		if bare != name {
			wildcard = true
		}
		if bare == "" || strings.Contains(bare, "*") || strings.ContainsAny(name, " \t/\\:@") || strings.Contains(bare, "..") {
			return fmt.Errorf("证书域名格式无效：%s", name)
		}
		if bare != zone && !strings.HasSuffix(bare, "."+zone) {
			return fmt.Errorf("证书域名 %s 不属于所选域名 %s，请为该域名单独创建证书", name, zone)
		}
	}
	if strings.EqualFold(strings.TrimSpace(provider), "tencent_free") {
		if wildcard {
			return fmt.Errorf("腾讯云免费证书不支持通配符域名，请改用ACME证书服务")
		}
		if len(names) > 1 {
			return fmt.Errorf("腾讯云免费证书只支持单个域名，请删除备用域名或改用ACME证书服务")
		}
	}
	return nil
}

// certificateNamesKey 规范化后的证书域名集合，用于比较两个证书任务是否相同。
func certificateNamesKey(commonName, sans string) string {
	names := certificateNames(&model.Certificate{CommonName: commonName, SANs: sans}, commonName)
	sort.Strings(names)
	return strings.Join(names, ",")
}

// certificateDeployable 证书是否可以部署：已签发，或续期进行中/续期失败但旧证书仍有效。
func certificateDeployable(row model.Certificate) bool {
	if row.Status == certStatusIssued {
		return true
	}
	if row.Status == certStatusRevoked || row.Status == certStatusRevoking {
		return false
	}
	cert, err := parseCertificateWithMatchingKey(row.CertPEM, row.KeyPEM)
	return err == nil && time.Now().Before(cert.NotAfter)
}
