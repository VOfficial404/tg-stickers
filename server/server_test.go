package server_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VOfficial404/tg-stickers/account"
	"github.com/VOfficial404/tg-stickers/server"
)

func TestHandlerServesWebAndProtectsAPI(t *testing.T) {
	dir := t.TempDir()
	acc, err := account.New(account.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// 不启动账号连接；路由测试也不依赖机器上是否安装了 ffmpeg。
	srv, err := server.New(server.Config{
		Account: acc,
		DataDir: dir,
		Token:   "test-token",
		FFmpeg:  filepath.Join(dir, "missing-ffmpeg"),
		Logf:    t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	for _, tc := range []struct {
		name        string
		path        string
		token       string
		status      int
		contentType string
	}{
		{name: "homepage", path: "/", status: http.StatusOK, contentType: "text/html"},
		{name: "script", path: "/app.js", status: http.StatusOK, contentType: "javascript"},
		{name: "state requires token", path: "/api/state", status: http.StatusUnauthorized, contentType: "application/json"},
		{name: "state rejects wrong token", path: "/api/state", token: "wrong-token", status: http.StatusUnauthorized, contentType: "application/json"},
		{name: "state accepts token", path: "/api/state", token: "test-token", status: http.StatusOK, contentType: "application/json"},
		{name: "unknown API", path: "/api/unknown", status: http.StatusNotFound},
		{name: "unknown API with token", path: "/api/unknown", token: "test-token", status: http.StatusNotFound},
		{name: "unknown page", path: "/missing-page", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("GET %s: status = %d, want %d; body: %s", tc.path, rec.Code, tc.status, rec.Body.String())
			}
			if tc.contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) {
				t.Errorf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), tc.contentType)
			}
			body := strings.ToLower(rec.Body.String())
			if tc.status == http.StatusNotFound && strings.Contains(body, "<html") {
				t.Error("unknown route returned the management page")
			}
			if tc.path == "/" || tc.path == "/app.js" {
				if rec.Body.Len() == 0 {
					t.Error("embedded resource is empty")
				}
				if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error("embedded resource is missing nosniff")
				}
				policy := rec.Header().Get("Content-Security-Policy")
				for _, directive := range []string{"script-src 'self'", "style-src 'self'", "connect-src 'self'", "img-src 'self' blob: data:"} {
					if !strings.Contains(policy, directive) {
						t.Errorf("CSP is missing %q", directive)
					}
				}
				if strings.Contains(policy, "'unsafe-inline'") {
					t.Error("CSP allows inline code")
				}
			}
			if tc.path == "/" {
				if !strings.Contains(body, "<html") {
					t.Error("homepage did not contain HTML")
				}
				if rec.Header().Get("Cache-Control") != "no-cache" {
					t.Error("homepage must be revalidated after updates")
				}
			}
		})
	}
}
