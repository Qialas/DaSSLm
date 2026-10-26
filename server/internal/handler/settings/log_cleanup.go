package settings

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	core "dasslm/server/internal/handler"
	"dasslm/server/internal/service/logcleanup"
)

const (
	minManualRetentionDays = 1
	maxManualRetentionDays = 3650
)

type LogCleanupHandler struct {
	db *gorm.DB
}

type logCleanupRequest struct {
	RetentionDays int `json:"retentionDays"`
}

func NewLogCleanupHandler(db *gorm.DB) *LogCleanupHandler {
	return &LogCleanupHandler{db: db}
}

func (h *LogCleanupHandler) Run(c *gin.Context) {
	var req logCleanupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if req.RetentionDays < minManualRetentionDays || req.RetentionDays > maxManualRetentionDays {
		core.Fail(c, http.StatusBadRequest, "保留天数需在1到3650之间")
		return
	}
	result, err := logcleanup.Run(h.db, req.RetentionDays)
	if err != nil {
		core.LogSystem(h.db, "admin", "system", "error", "manual_log_cleanup", "failed", "手动日志清理失败", gin.H{"retentionDays": req.RetentionDays, "error": err.Error()})
		core.Fail(c, http.StatusInternalServerError, "日志清理失败："+err.Error())
		return
	}
	core.LogSystem(h.db, "admin", "system", "info", "manual_log_cleanup", "success", "手动日志清理完成", gin.H{
		"retentionDays": req.RetentionDays,
		"cutoff":        result.Cutoff.Format(time.DateTime),
		"deletedTotal":  result.Total,
		"tables":        result.Tables,
	})
	core.Ok(c, result)
}
