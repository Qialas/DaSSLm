package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/auth"
	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testSecret = "test-secret-0123456789abcdef0123456789"

func setupAuth(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/me", Auth(db, testSecret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"role": c.GetString("role")})
	})
	return db, r
}

func request(r *gin.Engine, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func issue(t *testing.T, user model.User) string {
	t.Helper()
	token, _, err := auth.GenerateToken(testSecret, 1, user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// 改密码等操作递增凭证版本后，旧凭证立即失效
func TestAuthRejectsTokenAfterVersionChange(t *testing.T) {
	db, r := setupAuth(t)
	user := model.User{Username: "alice", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled}
	db.Create(&user)
	token := issue(t, user)
	if code := request(r, token); code != http.StatusOK {
		t.Fatalf("expected valid token to pass, got %d", code)
	}
	db.Model(&user).Update("token_version", user.TokenVersion+1)
	if code := request(r, token); code != http.StatusUnauthorized {
		t.Fatalf("expected old token rejected after version bump, got %d", code)
	}
}

func TestAuthRejectsDisabledOrDeletedUser(t *testing.T) {
	db, r := setupAuth(t)
	user := model.User{Username: "bob", PasswordHash: "x", Role: model.RoleOperator, Status: model.StatusEnabled}
	db.Create(&user)
	token := issue(t, user)

	db.Model(&user).Update("status", "disabled")
	if code := request(r, token); code != http.StatusUnauthorized {
		t.Fatalf("expected disabled user rejected, got %d", code)
	}
	db.Delete(&model.User{}, user.ID)
	if code := request(r, token); code != http.StatusUnauthorized {
		t.Fatalf("expected deleted user rejected, got %d", code)
	}
}

func TestAuthRejectsForeignSecretAndAlgNone(t *testing.T) {
	db, r := setupAuth(t)
	user := model.User{Username: "carol", PasswordHash: "x", Role: model.RoleAdmin, Status: model.StatusEnabled}
	db.Create(&user)
	forged, _, _ := auth.GenerateToken("please-change-this-secret", 1, user.ID, user.Username, user.Role, 0)
	if code := request(r, forged); code != http.StatusUnauthorized {
		t.Fatalf("expected token signed with another secret rejected, got %d", code)
	}
	// alg=none 的伪造凭证
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1c2VySWQiOjEsInJvbGUiOiJhZG1pbiIsInR2IjowfQ."
	if code := request(r, none); code != http.StatusUnauthorized {
		t.Fatalf("expected alg=none token rejected, got %d", code)
	}
}
