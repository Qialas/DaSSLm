package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// ParamID 把路径参数解析为数字主键，非法时返回 0（查询结果为“不存在”）。
// 不能把 c.Param 的字符串直接传给 GORM 的 First/Delete：非数字字符串会被当作 SQL 条件拼接，
// 例如 DELETE /api/users/1=1 会删除整张表。
func ParamID(c *gin.Context, name string) uint {
	id, err := strconv.ParseUint(c.Param(name), 10, strconv.IntSize)
	if err != nil {
		return 0
	}
	return uint(id)
}
