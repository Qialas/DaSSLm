package deploy

import (
	core "dasslm/server/internal/handler"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"dasslm/server/internal/model"
	deployservice "dasslm/server/internal/service/deploy"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DeployAccountHandler struct {
	*core.ResourceHandler[model.DeployAccount]
	db *gorm.DB
}

func NewDeployAccountHandler(db *gorm.DB) *DeployAccountHandler {
	return &DeployAccountHandler{ResourceHandler: core.NewResourceHandler[model.DeployAccount](db), db: db}
}

func (h *DeployAccountHandler) Sites(c *gin.Context) {
	account, found := h.deployAccount(c)
	if !found {
		return
	}
	if !deployservice.IsBTPanelProvider(account.Provider) {
		core.Fail(c, http.StatusBadRequest, "当前服务暂不支持站点列表")
		return
	}
	client, err := core.DeployHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
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
	items, total, trace, err := deployservice.ListSites(c.Request.Context(), account, client, page, pageSize, c.Query("keyword"), c.Query("type"))
	status := "success"
	message := "已获取宝塔站点列表"
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	h.logDeployAccount("sites", account, "", status, message, trace)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	core.Ok(c, gin.H{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

func (h *DeployAccountHandler) Test(c *gin.Context) {
	account, found := h.deployAccount(c)
	if !found {
		return
	}
	client, err := core.DeployHTTPClient(h.db, account, 30*time.Second)
	if err != nil {
		core.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if deployservice.IsTencentCloudProvider(account.Provider) {
		trace, err := deployservice.TestTencentCloud(c.Request.Context(), account, client)
		status := "success"
		message := "腾讯云账号检测通过"
		if err != nil {
			status = "failed"
			message = err.Error()
			h.logDeployAccount("test", account, "", status, message, trace)
			core.Fail(c, http.StatusBadRequest, "检测失败："+err.Error())
			return
		}
		h.logDeployAccount("test", account, "", status, message, trace)
		core.Ok(c, gin.H{"message": "检测通过：腾讯云SSL证书服务连通性正常"})
		return
	}
	if !deployservice.IsBTPanelProvider(account.Provider) {
		core.Fail(c, http.StatusBadRequest, "当前只支持宝塔面板检测")
		return
	}
	info, trace, err := deployservice.GetSystemTotal(c.Request.Context(), account, client)
	status := "success"
	message := "部署账号检测通过"
	if err != nil {
		status = "failed"
		message = err.Error()
		h.logDeployAccount("test", account, "", status, message, trace)
		core.Fail(c, http.StatusBadRequest, "检测失败："+err.Error())
		return
	}
	if !info.Valid() {
		status = "failed"
		message = "宝塔面板返回数据不完整"
		h.logDeployAccount("test", account, "", status, message, trace)
		core.Fail(c, http.StatusBadRequest, "检测失败：宝塔面板返回数据不完整")
		return
	}
	h.logDeployAccount("test", account, "", status, message, trace)
	core.Ok(c, gin.H{
		"message":  fmt.Sprintf("检测通过：%s / %s", info.Version, info.System),
		"system":   info.System,
		"version":  info.Version,
		"cpuNum":   info.CPUNum,
		"memTotal": info.MemTotal,
	})
}

func (h *DeployAccountHandler) deployAccount(c *gin.Context) (model.DeployAccount, bool) {
	var account model.DeployAccount
	if err := h.db.First(&account, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "部署账号不存在")
		return account, false
	}
	return account, true
}

func (h *DeployAccountHandler) logDeployAccount(action string, account model.DeployAccount, targetName, status, message string, trace *deployservice.Trace) {
	log := model.CertificateDeployLog{
		DeployAccountID: account.ID,
		Provider:        account.Provider,
		TargetName:      targetName,
		Status:          status,
		Message:         fmt.Sprintf("%s：%s", action, message),
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
