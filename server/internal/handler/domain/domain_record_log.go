package domain

import (
	core "dasslm/server/internal/handler"
	"net/http"
	"strconv"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DomainRecordLogHandler struct {
	*core.ResourceHandler[model.DomainAccountLog]
	db *gorm.DB
}

func NewDomainRecordLogHandler(db *gorm.DB) *DomainRecordLogHandler {
	return &DomainRecordLogHandler{ResourceHandler: core.NewResourceHandler[model.DomainAccountLog](db), db: db}
}

func (h *DomainRecordLogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := h.db.Model(&model.DomainAccountLog{}).
		Where("action IN ?", []string{"create_record", "update_record", "delete_record"})
	if domainID := c.Query("domainId"); domainID != "" {
		query = query.Where("domain_id = ?", domainID)
	}

	var rows []model.DomainAccountLog
	var total int64
	if err := query.Count(&total).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询总数失败")
		return
	}
	if err := query.Order("id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "查询列表失败")
		return
	}
	core.Ok(c, gin.H{"items": rows, "total": total, "page": page, "pageSize": pageSize})
}
