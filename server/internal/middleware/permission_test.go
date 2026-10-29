package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dasslm/server/internal/model"

	"github.com/gin-gonic/gin"
)

func serve(r *gin.Engine, method, path string) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.Code
}

func TestRequireAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for role, want := range map[string]int{model.RoleAdmin: http.StatusOK, model.RoleOperator: http.StatusForbidden, "": http.StatusForbidden} {
		r := gin.New()
		r.GET("/x", func(c *gin.Context) { c.Set("role", role) }, RequireAdmin(), func(c *gin.Context) { c.Status(http.StatusOK) })
		if code := serve(r, http.MethodGet, "/x"); code != want {
			t.Fatalf("role %q: expected %d, got %d", role, want, code)
		}
	}
}

func TestNumericParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NumericParams("id", "recordId"))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/items/:id", ok)
	r.GET("/items/:id/records/:recordId", ok)
	r.GET("/tasks/:key", ok)

	cases := map[string]int{
		"/items/12":            http.StatusOK,
		"/items/1=1":           http.StatusBadRequest,
		"/items/0":             http.StatusBadRequest,
		"/items/-1":            http.StatusBadRequest,
		"/items/3/records/4":   http.StatusOK,
		"/items/3/records/1=1": http.StatusBadRequest,
		"/tasks/log_cleanup":   http.StatusOK,
	}
	for path, want := range cases {
		if code := serve(r, http.MethodGet, path); code != want {
			t.Fatalf("%s: expected %d, got %d", path, want, code)
		}
	}
}
