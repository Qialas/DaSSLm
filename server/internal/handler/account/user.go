package account

import (
	core "dasslm/server/internal/handler"
	"dasslm/server/internal/model"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{db: db}
}

type userPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

func (h *UserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var rows []model.User
	var total int64
	query := h.db.Model(&model.User{})
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

func (h *UserHandler) Get(c *gin.Context) {
	var user model.User
	if err := h.db.First(&user, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	core.Ok(c, user)
}

func (h *UserHandler) Create(c *gin.Context) {
	var req userPayload
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		core.Fail(c, http.StatusBadRequest, "请输入用户名和密码")
		return
	}
	role, status := defaultString(req.Role, model.RoleOperator), defaultString(req.Status, model.StatusEnabled)
	if msg := validateRoleStatus(role, status); msg != "" {
		core.Fail(c, http.StatusBadRequest, msg)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "密码加密失败")
		return
	}
	user := model.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Nickname:     req.Nickname,
		Email:        req.Email,
		Phone:        req.Phone,
		Role:         role,
		Status:       status,
	}
	if err := h.db.Create(&user).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	core.Ok(c, user)
}

func (h *UserHandler) Update(c *gin.Context) {
	var user model.User
	if err := h.db.First(&user, core.ParamID(c, "id")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	var req userPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		core.Fail(c, http.StatusBadRequest, "请输入用户名")
		return
	}
	var count int64
	if err := h.db.Model(&model.User{}).Where("username = ? AND id <> ?", username, user.ID).Count(&count).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "检查用户名失败")
		return
	}
	if count > 0 {
		core.Fail(c, http.StatusBadRequest, "用户名已存在")
		return
	}
	previousRole, previousStatus := user.Role, user.Status
	user.Username = username
	user.Nickname = req.Nickname
	user.Email = req.Email
	user.Phone = req.Phone
	user.Role = defaultString(req.Role, user.Role)
	user.Status = defaultString(req.Status, user.Status)
	if msg := validateRoleStatus(user.Role, user.Status); msg != "" {
		core.Fail(c, http.StatusBadRequest, msg)
		return
	}
	if isActiveAdmin(previousRole, previousStatus) && !isActiveAdmin(user.Role, user.Status) {
		if msg := h.checkOtherActiveAdmin(user.ID); msg != "" {
			core.Fail(c, http.StatusBadRequest, msg)
			return
		}
	}
	revoke := user.Role != previousRole || user.Status != previousStatus
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			core.Fail(c, http.StatusInternalServerError, "密码加密失败")
			return
		}
		user.PasswordHash = string(hash)
		revoke = true
	}
	// 改密码、改角色、启用/停用后，该用户已登录的所有设备需重新登录
	if revoke {
		user.TokenVersion++
	}
	if err := h.db.Save(&user).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "保存用户失败")
		return
	}
	core.Ok(c, user)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id := core.ParamID(c, "id")
	if id == 0 {
		core.Fail(c, http.StatusBadRequest, "用户不存在")
		return
	}
	if id == c.GetUint("userId") {
		core.Fail(c, http.StatusBadRequest, "不能删除当前登录的用户")
		return
	}
	var user model.User
	if err := h.db.First(&user, id).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if isActiveAdmin(user.Role, user.Status) {
		if msg := h.checkOtherActiveAdmin(user.ID); msg != "" {
			core.Fail(c, http.StatusBadRequest, msg)
			return
		}
	}
	if err := h.db.Delete(&model.User{}, id).Error; err != nil {
		core.Fail(c, http.StatusInternalServerError, "删除用户失败")
		return
	}
	core.Ok(c, gin.H{"deleted": true})
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validateRoleStatus(role, status string) string {
	if role != model.RoleAdmin && role != model.RoleOperator {
		return "角色只能是管理员或操作员"
	}
	if status != model.StatusEnabled && status != model.StatusDisabled {
		return "状态只能是启用或停用"
	}
	return ""
}

func isActiveAdmin(role, status string) bool {
	return role == model.RoleAdmin && status == model.StatusEnabled
}

// checkOtherActiveAdmin 确认除指定用户外还有启用中的管理员，避免系统失去管理员。
func (h *UserHandler) checkOtherActiveAdmin(userID uint) string {
	var count int64
	if err := h.db.Model(&model.User{}).Where("role = ? AND status = ? AND id <> ?", model.RoleAdmin, model.StatusEnabled, userID).Count(&count).Error; err != nil {
		return "检查管理员数量失败"
	}
	if count == 0 {
		return "至少需要保留一个启用中的管理员"
	}
	return ""
}
