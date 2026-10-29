package account

import (
	"dasslm/server/internal/auth"
	"dasslm/server/internal/config"
	core "dasslm/server/internal/handler"
	"dasslm/server/internal/model"
	"dasslm/server/internal/service/notification"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewAuthHandler(db *gorm.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg}
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		core.Fail(c, http.StatusBadRequest, "请输入用户名和密码")
		return
	}

	var user model.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		h.recordLogin(c, 0, req.Username, "failed", "用户名或密码错误")
		core.Fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if user.Status != model.StatusEnabled {
		h.recordLogin(c, user.ID, user.Username, "failed", "账号已停用")
		core.Fail(c, http.StatusForbidden, "账号已停用")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		h.recordLogin(c, user.ID, user.Username, "failed", "用户名或密码错误")
		core.Fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	token, expiresAt, err := auth.GenerateToken(h.cfg.JWT.Secret, h.cfg.JWT.ExpireHours, user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}

	h.recordLogin(c, user.ID, user.Username, "success", "登录成功")
	notification.DispatchAsync(h.db, notification.EventAdminLoginSuccess, notification.Message{
		Title:   "管理员登录成功",
		Content: "账号 " + user.Username + " 已登录 DaSSLm 管理后台，来源 IP：" + c.ClientIP(),
		Level:   "info",
		Variables: map[string]string{
			"username": user.Username,
			"ip":       c.ClientIP(),
		},
	})
	core.Ok(c, gin.H{"token": token, "expiresAt": expiresAt, "user": user})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("userId")
	var user model.User
	if err := h.db.First(&user, userID).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	core.Ok(c, user)
}

const minPasswordLength = 8

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// ChangePassword 修改当前登录用户自己的密码，需校验原密码。
// 修改后凭证版本递增，其他设备上的登录全部失效；当前会话返回新凭证继续使用。
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		core.Fail(c, http.StatusBadRequest, "请输入原密码和新密码")
		return
	}
	if len(req.NewPassword) < minPasswordLength {
		core.Fail(c, http.StatusBadRequest, "新密码至少需要8位")
		return
	}
	if req.NewPassword == req.OldPassword {
		core.Fail(c, http.StatusBadRequest, "新密码不能与原密码相同")
		return
	}
	var user model.User
	if err := h.db.First(&user, c.GetUint("userId")).Error; err != nil {
		core.Fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		core.Fail(c, http.StatusBadRequest, "原密码错误")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "密码加密失败")
		return
	}
	// 以原凭证版本为条件更新，防止并发修改时两个请求都拿到有效的新凭证
	result := h.db.Model(&model.User{}).Where("id = ? AND token_version = ?", user.ID, user.TokenVersion).
		Updates(map[string]any{"password_hash": string(hash), "token_version": user.TokenVersion + 1})
	if result.Error != nil {
		core.Fail(c, http.StatusInternalServerError, "保存密码失败")
		return
	}
	if result.RowsAffected == 0 {
		core.Fail(c, http.StatusConflict, "登录状态已变化，请重新登录后再试")
		return
	}
	user.TokenVersion++
	token, expiresAt, err := auth.GenerateToken(h.cfg.JWT.Secret, h.cfg.JWT.ExpireHours, user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		core.Fail(c, http.StatusInternalServerError, "密码已修改，生成新登录凭证失败，请重新登录")
		return
	}
	core.Ok(c, gin.H{"token": token, "expiresAt": expiresAt})
}

func (h *AuthHandler) recordLogin(c *gin.Context, userID uint, username, status, message string) {
	_ = h.db.Create(&model.LoginLog{
		UserID:    userID,
		Username:  username,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Status:    status,
		Message:   message,
	}).Error
}
