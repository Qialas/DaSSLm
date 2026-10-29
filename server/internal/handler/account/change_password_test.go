package account

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/auth"
	"dasslm/server/internal/config"
	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestChangePassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	user := model.User{Username: "op", PasswordHash: string(hash), Role: model.RoleOperator, Status: model.StatusEnabled}
	db.Create(&user)

	cfg := &config.Config{}
	cfg.JWT.Secret = "test-secret-0123456789abcdef0123456789"
	cfg.JWT.ExpireHours = 1
	r := gin.New()
	r.PUT("/auth/password", func(c *gin.Context) { c.Set("userId", user.ID) }, NewAuthHandler(db, cfg).ChangePassword)
	change := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/auth/password", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for body, reason := range map[string]string{
		`{"oldPassword":"wrong-password","newPassword":"new-password"}`: "wrong old password",
		`{"oldPassword":"old-password","newPassword":"short"}`:          "too short",
		`{"oldPassword":"old-password","newPassword":"old-password"}`:   "same as old",
		`{"oldPassword":"","newPassword":"new-password"}`:               "missing old password",
	} {
		if w := change(body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", reason, w.Code)
		}
	}

	w := change(`{"oldPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("change failed: %d %s", w.Code, w.Body.String())
	}
	var saved model.User
	db.First(&saved, user.ID)
	if bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("new-password")) != nil {
		t.Fatal("new password not saved")
	}
	if saved.TokenVersion != 1 {
		t.Fatalf("token version should be bumped to revoke other sessions, got %d", saved.TokenVersion)
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ParseToken(cfg.JWT.Secret, resp.Data.Token)
	if err != nil || claims.TokenVersion != 1 {
		t.Fatalf("returned token should carry the new version: %v %+v", err, claims)
	}
}
