package settings

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	core "dasslm/server/internal/handler"
	"dasslm/server/internal/service/logcleanup"
	"dasslm/server/internal/service/systemtask"
)

func StartLogCleanupWorker(db *gorm.DB) {
	go func() {
		time.Sleep(2 * time.Second)
		delay := systemtask.Interval(db, systemtask.LogCleanupTaskKey)
		core.LogSystem(db, "monitor", "system", "info", "log_cleanup_worker_start", "success", "日志清理后台监控已启动", gin.H{"nextDelaySeconds": int(delay.Seconds())})
		for {
			time.Sleep(systemtask.Interval(db, systemtask.LogCleanupTaskKey))
			retentionDays := systemtask.RetentionDays(db, systemtask.LogCleanupTaskKey)
			result, err := logcleanup.Run(db, retentionDays)
			if err != nil {
				core.LogSystem(db, "monitor", "system", "error", "log_cleanup", "failed", "日志清理失败", gin.H{"retentionDays": retentionDays, "error": err.Error()})
				continue
			}
			core.LogSystem(db, "monitor", "system", "info", "log_cleanup", "success", "日志清理完成", gin.H{
				"retentionDays": retentionDays,
				"cutoff":        result.Cutoff.Format(time.DateTime),
				"deletedTotal":  result.Total,
				"tables":        result.Tables,
			})
		}
	}()
}
