package settings

import (
	"context"
	core "dasslm/server/internal/handler"
	"dasslm/server/internal/model"
	"dasslm/server/internal/service/notification"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type NotificationSettingHandler struct {
	*core.ResourceHandler[model.NotificationSetting]
	db *gorm.DB
}

func NewNotificationSettingHandler(db *gorm.DB) *NotificationSettingHandler {
	return &NotificationSettingHandler{
		ResourceHandler: core.NewResourceHandler[model.NotificationSetting](db),
		db:              db,
	}
}

func (h *NotificationSettingHandler) Create(c *gin.Context) {
	var row model.NotificationSetting
	if err := c.ShouldBindJSON(&row); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := notification.Validate(&row); err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.db.Create(&row).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "创建失败")
		return
	}
	core.Ok(c, row)
}

func (h *NotificationSettingHandler) Update(c *gin.Context) {
	var row model.NotificationSetting
	if err := h.db.First(&row, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "数据不存在")
		return
	}
	if err := c.ShouldBindJSON(&row); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	if err := notification.Validate(&row); err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.db.Save(&row).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	core.Ok(c, row)
}

func (h *NotificationSettingHandler) Test(c *gin.Context) {
	var row model.NotificationSetting
	if err := h.db.First(&row, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "数据不存在")
		return
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := notification.Send(ctx, row, notification.Message{
		Title:   "DaSSLm 通知测试",
		Content: fmt.Sprintf("通知渠道「%s」已连接成功。", row.Name),
		Event:   "test",
		Level:   "info",
	}); err != nil {
		core.Fail(c, http.StatusBadGateway, "通知测试失败："+err.Error())
		return
	}
	core.Ok(c, gin.H{"message": fmt.Sprintf("通知测试发送成功，耗时 %dms", time.Since(startedAt).Milliseconds())})
}

func (h *NotificationSettingHandler) Rules(c *gin.Context) {
	rules, err := notification.GetRules(h.db)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询通知规则失败")
		return
	}
	core.Ok(c, rules)
}

func (h *NotificationSettingHandler) SaveRules(c *gin.Context) {
	var rules []notification.Rule
	if err := c.ShouldBindJSON(&rules); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	saved, err := notification.SaveRules(h.db, rules)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存通知规则失败")
		return
	}
	core.Ok(c, saved)
}
