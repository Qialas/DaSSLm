package settings

import (
	core "dasslm/server/internal/handler"
	"dasslm/server/internal/service/systemtask"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SystemTaskHandler struct {
	db *gorm.DB
}

func NewSystemTaskHandler(db *gorm.DB) *SystemTaskHandler {
	return &SystemTaskHandler{db: db}
}

func (h *SystemTaskHandler) List(c *gin.Context) {
	tasks, err := systemtask.List(h.db)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询系统任务失败")
		return
	}
	core.Ok(c, tasks)
}

func (h *SystemTaskHandler) Update(c *gin.Context) {
	var req systemtask.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	task, err := systemtask.Save(h.db, c.Param("key"), req)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	core.Ok(c, task)
}
