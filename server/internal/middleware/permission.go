package middleware

import (
	"net/http"
	"strconv"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
)

// RequireAdmin 只允许管理员访问，须挂在 Auth 之后（依赖 Auth 写入的 role）。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("role") != model.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权限执行此操作，请联系管理员"})
			return
		}
		c.Next()
	}
}

// NumericParams 要求指定的路径参数（存在时）必须是正整数。
// 这是数据库主键参数的统一兜底校验，防止非数字字符串被 GORM 当作 SQL 条件拼接。
func NumericParams(names ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range names {
			value, exists := c.Params.Get(name)
			if !exists {
				continue
			}
			if id, err := strconv.ParseUint(value, 10, strconv.IntSize); err != nil || id == 0 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的ID"})
				return
			}
		}
		c.Next()
	}
}
