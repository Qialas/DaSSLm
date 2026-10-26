package middleware

import (
	"net/http"
	"strings"

	"dasslm/server/internal/auth"
	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Auth 校验登录凭证，并以数据库中的用户状态为准：
// 用户被删除、停用，或凭证版本已变更（改密码、改角色等）时，旧凭证立即失效。
func Auth(db *gorm.DB, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录或登录已过期"})
			return
		}

		claims, err := auth.ParseToken(secret, strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "登录状态无效"})
			return
		}

		var user model.User
		if err := db.Select("id", "username", "role", "status", "token_version").First(&user, claims.UserID).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "登录状态已失效，请重新登录"})
			return
		}
		if user.Status != model.StatusEnabled || user.TokenVersion != claims.TokenVersion {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "登录状态已失效，请重新登录"})
			return
		}

		c.Set("userId", user.ID)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Next()
	}
}
