package handler

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

func Ok(c *gin.Context, data any) {
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": data})
}

func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message})
}

func ok(c *gin.Context, data any) {
	Ok(c, data)
}

func fail(c *gin.Context, status int, message string) {
	Fail(c, status, message)
}

func MustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func DefaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
