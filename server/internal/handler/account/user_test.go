package account

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 改密码、改角色、停用会使该用户凭证版本递增；只改昵称等资料不影响已登录状态
func TestUserUpdateBumpsTokenVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "dave", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled}
	db.Create(&user)

	r := gin.New()
	r.PUT("/users/:id", NewUserHandler(db).Update)
	update := func(body string) uint {
		req := httptest.NewRequest(http.MethodPut, "/users/1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
		}
		var saved model.User
		db.First(&saved, user.ID)
		return saved.TokenVersion
	}

	if v := update(`{"username":"dave","nickname":"D"}`); v != 0 {
		t.Fatalf("profile edit should not revoke sessions, version=%d", v)
	}
	if v := update(`{"username":"dave","password":"new-password"}`); v != 1 {
		t.Fatalf("password change should revoke sessions, version=%d", v)
	}
	if v := update(`{"username":"dave","role":"admin"}`); v != 2 {
		t.Fatalf("role change should revoke sessions, version=%d", v)
	}
	if v := update(`{"username":"dave","role":"admin","status":"disabled"}`); v != 3 {
		t.Fatalf("disable should revoke sessions, version=%d", v)
	}
}
