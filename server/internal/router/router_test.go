package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/auth"
	"dasslm/server/internal/config"
	"dasslm/server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testSecret = "test-secret-0123456789abcdef0123456789"

// 操作员可以做日常运维和查看日志，但账号写入、用户、系统设置、通知和日志删除仅管理员可用
func TestOperatorPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.DomainAccount{}, &model.SSLAccount{}, &model.DeployAccount{},
		&model.Domain{}, &model.Certificate{}, &model.AutoTask{}, &model.AutoTaskTarget{}, &model.OperationLog{},
		&model.SystemLog{}, &model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Mode = "test"
	cfg.JWT.Secret = testSecret
	r := New(cfg, db)

	operator := model.User{Username: "op", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled}
	admin := model.User{Username: "root", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled}
	db.Create(&operator)
	db.Create(&admin)
	token := func(u model.User) string {
		s, _, err := auth.GenerateToken(testSecret, 1, u.ID, u.Username, u.Role, u.TokenVersion)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call := func(u model.User, method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token(u))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	forbidden := [][2]string{
		{http.MethodGet, "/api/users"},
		{http.MethodDelete, "/api/users/1"},
		{http.MethodPost, "/api/domain-accounts"},
		{http.MethodDelete, "/api/ssl-accounts/1"},
		{http.MethodPost, "/api/deploy-accounts/1/test"},
		{http.MethodGet, "/api/system-settings"},
		{http.MethodGet, "/api/system-tasks"},
		{http.MethodPost, "/api/log-cleanup"},
		{http.MethodGet, "/api/proxy-settings"},
		{http.MethodGet, "/api/notification-rules"},
		{http.MethodDelete, "/api/logs/1"},
		{http.MethodDelete, "/api/login-logs/1"},
	}
	for _, c := range forbidden {
		if code := call(operator, c[0], c[1]); code != http.StatusForbidden {
			t.Errorf("operator %s %s: expected 403, got %d", c[0], c[1], code)
		}
	}
	allowed := [][2]string{
		{http.MethodGet, "/api/domains"},
		{http.MethodGet, "/api/certificates"},
		{http.MethodGet, "/api/tasks"},
		{http.MethodGet, "/api/ssl-accounts"},
		{http.MethodGet, "/api/logs"},
	}
	for _, c := range allowed {
		if code := call(operator, c[0], c[1]); code != http.StatusOK {
			t.Errorf("operator %s %s: expected 200, got %d", c[0], c[1], code)
		}
	}
	if code := call(admin, http.MethodGet, "/api/users"); code != http.StatusOK {
		t.Errorf("admin GET /api/users: expected 200, got %d", code)
	}
	if code := call(admin, http.MethodDelete, "/api/users/1=1"); code != http.StatusBadRequest {
		t.Errorf("non-numeric id: expected 400, got %d", code)
	}
}
