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

func setupUserRouter(t *testing.T, currentUserID uint) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	h := NewUserHandler(db)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userId", currentUserID) })
	r.POST("/users", h.Create)
	r.PUT("/users/:id", h.Update)
	r.DELETE("/users/:id", h.Delete)
	return db, r
}

func send(r *gin.Engine, method, path, body string) int {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestUserRejectsUnknownRole(t *testing.T) {
	_, r := setupUserRouter(t, 0)
	if code := send(r, http.MethodPost, "/users", `{"username":"eve","password":"p","role":"superadmin"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown role should be rejected, got %d", code)
	}
	if code := send(r, http.MethodPost, "/users", `{"username":"eve","password":"p","role":"operator"}`); code != http.StatusOK {
		t.Fatalf("operator role should be accepted, got %d", code)
	}
}

// 不能停用、降级或删除最后一个启用中的管理员，也不能删除自己
func TestUserKeepsLastActiveAdmin(t *testing.T) {
	db, r := setupUserRouter(t, 99)
	admin := model.User{Username: "admin", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled}
	db.Create(&admin)

	if code := send(r, http.MethodPut, "/users/1", `{"username":"admin","role":"operator"}`); code != http.StatusBadRequest {
		t.Fatalf("demoting last admin should fail, got %d", code)
	}
	if code := send(r, http.MethodPut, "/users/1", `{"username":"admin","status":"disabled"}`); code != http.StatusBadRequest {
		t.Fatalf("disabling last admin should fail, got %d", code)
	}
	if code := send(r, http.MethodDelete, "/users/1", ""); code != http.StatusBadRequest {
		t.Fatalf("deleting last admin should fail, got %d", code)
	}

	db.Create(&model.User{Username: "second", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled})
	if code := send(r, http.MethodDelete, "/users/1", ""); code != http.StatusOK {
		t.Fatalf("deleting an admin while another exists should succeed, got %d", code)
	}
}

func TestUserCannotDeleteSelf(t *testing.T) {
	db, r := setupUserRouter(t, 2)
	db.Create(&model.User{Username: "admin", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled})
	db.Create(&model.User{Username: "me", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled})
	if code := send(r, http.MethodDelete, "/users/2", ""); code != http.StatusBadRequest {
		t.Fatalf("deleting self should fail, got %d", code)
	}
}

// 非数字 ID 不能被当作 SQL 条件，DELETE /users/1=1 不应删除任何用户
func TestUserDeleteRejectsSQLCondition(t *testing.T) {
	db, r := setupUserRouter(t, 0)
	db.Create(&model.User{Username: "a", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled})
	db.Create(&model.User{Username: "b", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled})
	send(r, http.MethodDelete, "/users/1=1", "")
	var count int64
	db.Model(&model.User{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected no users deleted, %d remain", count)
	}
}
