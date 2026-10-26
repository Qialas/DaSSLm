package web

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterServesEmbeddedAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r)

	root := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(root, req)
	if root.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", root.Code, http.StatusOK)
	}
	if !strings.Contains(root.Body.String(), `<div id="app">`) {
		t.Fatalf("GET / did not return admin index")
	}

	// Check every output chunk, including lazy pages and nested CSS. Go embed
	// omits underscore-prefixed files; a missing chunk otherwise returns index HTML.
	assetCount := 0
	err := fs.WalkDir(os.DirFS("dist"), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (path.Ext(name) != ".js" && path.Ext(name) != ".css") {
			return nil
		}
		assetCount++
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(path.Join("dist", name))
			if err != nil {
				t.Fatal(err)
			}
			asset := httptest.NewRecorder()
			r.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/"+name, nil))
			if asset.Code != http.StatusOK || !bytes.Equal(asset.Body.Bytes(), want) {
				t.Fatalf("GET /%s did not serve the built asset (status %d)", name, asset.Code)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if assetCount == 0 {
		t.Fatal("no built JS/CSS assets found")
	}

	deepLink := httptest.NewRecorder()
	r.ServeHTTP(deepLink, httptest.NewRequest(http.MethodGet, "/domains/1", nil))
	if deepLink.Code != http.StatusOK || !bytes.Equal(deepLink.Body.Bytes(), root.Body.Bytes()) {
		t.Fatal("deep link did not return the admin index")
	}
	missingAPI := httptest.NewRecorder()
	r.ServeHTTP(missingAPI, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	if missingAPI.Code != http.StatusNotFound {
		t.Fatalf("missing API status = %d, want 404", missingAPI.Code)
	}
}
