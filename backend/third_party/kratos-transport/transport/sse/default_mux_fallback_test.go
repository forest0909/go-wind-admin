package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof" // 让 DefaultServeMux 上有 /debug/pprof/*，和生产二进制一致
	"testing"
)

// SSE 路由的未匹配请求必须 404，不能回退到 http.DefaultServeMux。
// 修复前 /debug/pprof/heap 经 DefaultServeMux 免鉴权返回 200。
func TestServer_UnmatchedRoutesDoNotFallBackToDefaultServeMux(t *testing.T) {
	srv := NewServer()
	defer func() { _ = srv.Stop(context.Background()) }()
	srv.HandleServeHTTP("/events")

	for _, p := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/cmdline"} {
		rec := httptest.NewRecorder()
		srv.Server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: want 404, got %d", p, rec.Code)
		}
	}
}
