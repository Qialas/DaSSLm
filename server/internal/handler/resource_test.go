package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 路径中的非数字 ID 不能被 GORM 当作 SQL 条件执行
func TestResourceRejectsSQLConditionInID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&model.SystemSetting{SettingKey: "a", ValueType: "string"})
	db.Create(&model.SystemSetting{SettingKey: "b", ValueType: "string"})

	h := NewResourceHandler[model.SystemSetting](db)
	r := gin.New()
	r.GET("/settings/:id", h.Get)
	r.DELETE("/settings/:id", h.Delete)

	for _, path := range []string{"/settings/1=1", "/settings/id%3E0", "/settings/0"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusOK {
			t.Fatalf("GET %s should not match any row", path)
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
	}
	var count int64
	db.Model(&model.SystemSetting{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected no rows deleted, %d remain", count)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("numeric id should still work, got %d", w.Code)
	}
}
