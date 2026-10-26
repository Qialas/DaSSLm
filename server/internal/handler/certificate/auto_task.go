package certificate

import (
	"context"
	core "dasslm/server/internal/handler"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dasslm/server/internal/model"
	"dasslm/server/internal/service/notification"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AutoTaskHandler struct {
	db *gorm.DB
}

func NewAutoTaskHandler(db *gorm.DB) *AutoTaskHandler {
	return &AutoTaskHandler{db: db}
}

type autoTaskTargetRequest struct {
	ID              uint   `json:"id"`
	DeployAccountID uint   `json:"deployAccountId" binding:"required"`
	SiteName        string `json:"siteName" binding:"required"`
	ProductType     string `json:"productType"`
	Region          string `json:"region"`
	ResourceID      string `json:"resourceId"`
	ResourceName    string `json:"resourceName"`
	ExtraConfig     string `json:"extraConfig"`
	Remark          string `json:"remark"`
	Status          string `json:"status"`
}

type autoTaskRequest struct {
	Name            string                  `json:"name"`
	DeployAccountID uint                    `json:"deployAccountId"`
	CertificateID   uint                    `json:"certificateId" binding:"required"`
	SiteName        string                  `json:"siteName"`
	Remark          string                  `json:"remark"`
	Status          string                  `json:"status"`
	Targets         []autoTaskTargetRequest `json:"targets"`
}

func (h *AutoTaskHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var rows []model.AutoTask
	var total int64
	query := h.db.Model(&model.AutoTask{})
	if certificateID := strings.TrimSpace(c.Query("certificateId")); certificateID != "" {
		query = query.Where("certificate_id = ? OR (target_type = ? AND target_id = ?)", certificateID, "certificate", certificateID)
	}
	if err := query.Count(&total).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询总数失败")
		return
	}
	if err := query.Preload("Targets").Order("id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询列表失败")
		return
	}
	core.Ok(c, gin.H{"items": rows, "total": total, "page": page, "pageSize": pageSize})
}

func (h *AutoTaskHandler) Get(c *gin.Context) {
	var row model.AutoTask
	if err := h.db.Preload("Targets").First(&row, c.Param("id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "自动任务不存在")
		return
	}
	core.Ok(c, row)
}

func (h *AutoTaskHandler) Create(c *gin.Context) {
	var req autoTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	row, targets, err := h.buildAutoDeployTask(req, nil)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for i := range targets {
			targets[i].TaskID = row.ID
		}
		if len(targets) > 0 {
			if err := tx.Create(&targets).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		core.Fail(c, http.StatusInternalServerError, "创建自动任务失败")
		return
	}
	h.db.Preload("Targets").First(&row, row.ID)
	if row.Status == model.StatusEnabled && h.certificateIsIssued(row.CertificateID) {
		h.runAutoDeployTaskAsync(row.ID)
	}
	core.Ok(c, row)
}

func (h *AutoTaskHandler) Update(c *gin.Context) {
	var row model.AutoTask
	if err := h.db.First(&row, c.Param("id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "自动任务不存在")
		return
	}
	var req autoTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	next, targets, err := h.buildAutoDeployTask(req, &row)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&row).Updates(map[string]any{
			"name":           next.Name,
			"task_type":      next.TaskType,
			"cron_expr":      next.CronExpr,
			"target_id":      next.TargetID,
			"target_type":    next.TargetType,
			"certificate_id": next.CertificateID,
			"site_name":      next.SiteName,
			"status":         next.Status,
			"remark":         next.Remark,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", row.ID).Delete(&model.AutoTaskTarget{}).Error; err != nil {
			return err
		}
		for i := range targets {
			targets[i].TaskID = row.ID
		}
		if len(targets) > 0 {
			if err := tx.Create(&targets).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存自动任务失败")
		return
	}
	h.db.Preload("Targets").First(&row, row.ID)
	if row.Status == model.StatusEnabled && h.certificateIsIssued(row.CertificateID) {
		h.runAutoDeployTaskAsync(row.ID)
	}
	core.Ok(c, row)
}

func (h *AutoTaskHandler) Delete(c *gin.Context) {
	taskID := c.Param("id")
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AutoTaskTarget{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AutoTask{}, taskID).Error
	}); err != nil {
		core.Fail(c, http.StatusInternalServerError, "删除自动任务失败")
		return
	}
	core.Ok(c, gin.H{"deleted": true})
}

func (h *AutoTaskHandler) buildAutoDeployTask(req autoTaskRequest, previous *model.AutoTask) (model.AutoTask, []model.AutoTaskTarget, error) {
	var cert model.Certificate
	if err := h.db.First(&cert, req.CertificateID).Error; err != nil {
		return model.AutoTask{}, nil, fmt.Errorf("SSL证书不存在")
	}
	targetReqs := req.Targets
	if len(targetReqs) == 0 && req.DeployAccountID > 0 {
		targetReqs = []autoTaskTargetRequest{{
			DeployAccountID: req.DeployAccountID,
			SiteName:        firstNonEmpty(req.SiteName, cert.CommonName),
			Status:          model.StatusEnabled,
			Remark:          req.Remark,
		}}
	}
	if len(targetReqs) == 0 {
		return model.AutoTask{}, nil, fmt.Errorf("请至少添加一个部署目标")
	}
	targets := make([]model.AutoTaskTarget, 0, len(targetReqs))
	seen := map[string]struct{}{}
	for _, item := range targetReqs {
		var deployAccount model.DeployAccount
		if err := h.db.First(&deployAccount, item.DeployAccountID).Error; err != nil {
			return model.AutoTask{}, nil, fmt.Errorf("部署账号不存在")
		}
		if deployAccount.Status == model.StatusDisabled {
			return model.AutoTask{}, nil, fmt.Errorf("部署账号已停用：%s", deployAccount.Name)
		}
		siteName := firstNonEmpty(item.SiteName, cert.CommonName)
		key := fmt.Sprintf("%d:%s:%s:%s", deployAccount.ID, strings.TrimSpace(item.ProductType), firstNonEmpty(item.ResourceID, siteName), siteName)
		if _, ok := seen[key]; ok {
			return model.AutoTask{}, nil, fmt.Errorf("部署目标重复：%s", siteName)
		}
		seen[key] = struct{}{}
		targets = append(targets, model.AutoTaskTarget{
			DeployAccountID: deployAccount.ID,
			SiteName:        siteName,
			ProductType:     strings.TrimSpace(item.ProductType),
			Region:          strings.TrimSpace(item.Region),
			ResourceID:      strings.TrimSpace(item.ResourceID),
			ResourceName:    strings.TrimSpace(item.ResourceName),
			ExtraConfig:     strings.TrimSpace(item.ExtraConfig),
			Status:          core.DefaultString(item.Status, model.StatusEnabled),
			Remark:          item.Remark,
		})
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s 自动部署", cert.CommonName)
	}
	status := core.DefaultString(req.Status, model.StatusEnabled)
	if previous != nil && strings.TrimSpace(req.Status) == "" {
		status = previous.Status
	}
	return model.AutoTask{
		Name:          name,
		TaskType:      "ssl_deploy",
		CronExpr:      "auto",
		TargetID:      cert.ID,
		TargetType:    "certificate",
		CertificateID: cert.ID,
		SiteName:      fmt.Sprintf("%d个部署目标", len(targets)),
		Status:        status,
		Remark:        req.Remark,
	}, targets, nil
}

func (h *AutoTaskHandler) runAutoDeployTaskAsync(taskID uint) {
	go func() {
		if err := h.runAutoDeployTask(context.Background(), taskID); err != nil {
			core.LogSystem(h.db, "worker", "auto_task", "error", "auto_ssl_deploy", "failed", "自动部署任务执行失败", gin.H{"taskId": taskID, "error": err.Error()})
		}
	}()
}

func (h *AutoTaskHandler) Run(c *gin.Context) {
	taskID, _ := strconv.Atoi(c.Param("id"))
	if taskID <= 0 {
		core.Fail(c, http.StatusBadRequest, "自动部署任务不存在")
		return
	}
	if err := h.runAutoDeployTask(c.Request.Context(), uint(taskID)); err != nil {
		core.Fail(c, http.StatusBadRequest, "执行自动部署失败："+err.Error())
		return
	}
	core.Ok(c, gin.H{"executed": true})
}

func (h *AutoTaskHandler) runAutoDeployTask(ctx context.Context, taskID uint) error {
	now := time.Now()
	tx := h.db.Model(&model.AutoTask{}).Where("id = ? AND (last_run_status IS NULL OR last_run_status <> ?)", taskID, "running").Updates(map[string]any{
		"last_run_at":     &now,
		"last_run_status": "running",
	})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return fmt.Errorf("自动部署任务正在执行中")
	}
	var task model.AutoTask
	if err := h.db.Preload("Targets").First(&task, taskID).Error; err != nil {
		return err
	}
	if task.Status == model.StatusDisabled {
		err := fmt.Errorf("自动部署任务已停用")
		h.finishAutoTask(task, nil, "failed", err.Error())
		return err
	}
	var cert model.Certificate
	if err := h.db.First(&cert, task.CertificateID).Error; err != nil {
		h.finishAutoTask(task, nil, "failed", "SSL证书不存在："+err.Error())
		return err
	}
	if cert.Status != certStatusIssued {
		err := fmt.Errorf("SSL证书未签发，暂不能部署")
		h.finishAutoTask(task, nil, "failed", err.Error())
		return err
	}
	enabledTargets := make([]model.AutoTaskTarget, 0, len(task.Targets))
	for _, target := range task.Targets {
		if target.Status != model.StatusDisabled {
			enabledTargets = append(enabledTargets, target)
		}
	}
	if len(enabledTargets) == 0 {
		err := fmt.Errorf("自动部署任务没有启用的部署目标")
		h.finishAutoTask(task, nil, "failed", err.Error())
		return err
	}
	taskLog := h.startAutoTaskLog(task, cert, len(enabledTargets))
	successCount := 0
	failedMessages := make([]string, 0)
	for _, target := range enabledTargets {
		status, message := h.runAutoDeployTarget(ctx, task, target, cert)
		if status == "success" {
			successCount++
		} else {
			failedMessages = append(failedMessages, fmt.Sprintf("%s：%s", target.SiteName, message))
		}
	}
	status := "success"
	message := fmt.Sprintf("自动部署完成，成功 %d/%d", successCount, len(enabledTargets))
	if successCount < len(enabledTargets) {
		status = "failed"
		message = strings.Join(failedMessages, "；")
	}
	h.finishAutoTask(task, taskLog, status, message)
	if status == "failed" {
		return errors.New(message)
	}
	notification.DispatchAsync(h.db, notification.EventDeploySuccess, notification.Message{
		Title:   "SSL证书自动部署成功",
		Content: fmt.Sprintf("证书 %s 已自动部署到 %d 个目标。", cert.CommonName, successCount),
		Level:   "info",
		Variables: map[string]string{
			"domain":      cert.CommonName,
			"targetCount": fmt.Sprintf("%d", successCount),
		},
	})
	return nil
}

func (h *AutoTaskHandler) runAutoDeployTarget(ctx context.Context, task model.AutoTask, target model.AutoTaskTarget, cert model.Certificate) (string, string) {
	startedAt := time.Now()
	_ = h.db.Model(&model.AutoTaskTarget{}).Where("id = ?", target.ID).Updates(map[string]any{
		"last_run_at":     &startedAt,
		"last_run_status": "running",
	}).Error
	var account model.DeployAccount
	if err := h.db.First(&account, target.DeployAccountID).Error; err != nil {
		message := "部署账号不存在：" + err.Error()
		h.finishAutoTarget(target, "failed", startedAt)
		return "failed", message
	}
	trace, err := deployIssuedCertificateToTarget(ctx, h.db, cert, account, target)
	status := "success"
	message := "自动部署目标执行完成"
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	(&CertificateHandler{db: h.db}).logCertificateDeployment(cert, account, firstNonEmpty(target.SiteName, cert.CommonName), status, message, trace)
	h.finishAutoTarget(target, status, startedAt)
	return status, message
}

func (h *AutoTaskHandler) finishAutoTarget(target model.AutoTaskTarget, status string, finishedAt time.Time) {
	_ = h.db.Model(&model.AutoTaskTarget{}).Where("id = ?", target.ID).Updates(map[string]any{
		"last_run_at":     &finishedAt,
		"last_run_status": status,
	}).Error
}

func (h *AutoTaskHandler) startAutoTaskLog(task model.AutoTask, cert model.Certificate, targetCount int) *model.TaskLog {
	now := time.Now()
	log := model.TaskLog{
		TaskName:   task.Name,
		TaskType:   "ssl_deploy",
		TargetID:   task.CertificateID,
		TargetType: "certificate",
		TargetName: cert.CommonName,
		Status:     "running",
		Message:    "自动部署任务执行中",
		StartedAt:  &now,
		Detail:     core.MustJSON(gin.H{"taskId": task.ID, "certificateId": cert.ID, "targetCount": targetCount}),
	}
	if err := h.db.Create(&log).Error; err != nil {
		return nil
	}
	return &log
}

func (h *AutoTaskHandler) certificateIsIssued(certificateID uint) bool {
	if certificateID == 0 {
		return false
	}
	var count int64
	h.db.Model(&model.Certificate{}).Where("id = ? AND status = ?", certificateID, certStatusIssued).Count(&count)
	return count > 0
}

func triggerCertificateAutoDeployments(db *gorm.DB, certificate model.Certificate) {
	var tasks []model.AutoTask
	if err := db.Where("task_type = ? AND status = ? AND (certificate_id = ? OR (target_type = ? AND target_id = ?))",
		"ssl_deploy", model.StatusEnabled, certificate.ID, "certificate", certificate.ID,
	).Find(&tasks).Error; err != nil {
		core.LogSystem(db, "worker", "auto_task", "error", "auto_ssl_deploy_scan", "failed", "查询证书自动部署任务失败", gin.H{"certificateId": certificate.ID, "error": err.Error()})
		return
	}
	handler := NewAutoTaskHandler(db)
	for _, task := range tasks {
		handler.runAutoDeployTaskAsync(task.ID)
	}
	if len(tasks) > 0 {
		core.LogSystem(db, "worker", "auto_task", "info", "auto_ssl_deploy_scan", "success", "已触发证书自动部署任务", gin.H{"certificateId": certificate.ID, "count": len(tasks)})
	}
}

func (h *AutoTaskHandler) finishAutoTask(task model.AutoTask, taskLog *model.TaskLog, status, message string) {
	finishedAt := time.Now()
	_ = h.db.Model(&model.AutoTask{}).Where("id = ?", task.ID).Updates(map[string]any{
		"last_run_at":     &finishedAt,
		"last_run_status": status,
	}).Error
	if taskLog != nil && taskLog.ID > 0 {
		duration := int64(0)
		if taskLog.StartedAt != nil {
			duration = finishedAt.Sub(*taskLog.StartedAt).Milliseconds()
		}
		_ = h.db.Model(taskLog).Updates(map[string]any{
			"status":      status,
			"message":     message,
			"finished_at": &finishedAt,
			"duration_ms": duration,
		}).Error
	}
}
