package rpc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	kratosRest "github.com/go-kratos/kratos/v2/transport/http"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

// 底层 http.Server 必须带读超时和空闲超时，且不设写超时（下载、导出要长时间写）。
func TestCreateRestServer_SetsSocketTimeouts(t *testing.T) {
	srv, err := CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{Addr: "127.0.0.1:0"}}})
	if err != nil {
		t.Fatal(err)
	}
	if srv.ReadHeaderTimeout != restReadHeaderTimeout || srv.ReadTimeout != restReadTimeout || srv.IdleTimeout != restIdleTimeout {
		t.Fatalf("unexpected timeouts: header=%v read=%v idle=%v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout must stay 0, got %v", srv.WriteTimeout)
	}
}

func restStatus(t *testing.T, h http.Handler, method, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

// enable_pprof=false 时 /debug/pprof/* 必须 404。修复前未匹配路由回退到 http.DefaultServeMux，
// net/http/pprof 在包初始化时注册在那里，这些路径免登录返回 200。
func TestCreateRestServer_PprofDisabledNotReachable(t *testing.T) {
	srv, err := CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{EnablePprof: false}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/cmdline", "/debug/pprof/goroutine?debug=2", "/debug/heap"} {
		if code := restStatus(t, srv, http.MethodGet, p); code != http.StatusNotFound {
			t.Fatalf("GET %s: want 404, got %d", p, code)
		}
	}
}

// enable_pprof=true 时在本服务路由上能访问到 pprof。
func TestCreateRestServer_PprofEnabledReachable(t *testing.T) {
	srv, err := CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{EnablePprof: true}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/cmdline", "/debug/heap?debug=1"} {
		if code := restStatus(t, srv, http.MethodGet, p); code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d", p, code)
		}
	}
}

// 方法不匹配给 405，不回退到 DefaultServeMux。
func TestCreateRestServer_MethodNotAllowed(t *testing.T) {
	srv, err := CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{}}})
	if err != nil {
		t.Fatal(err)
	}
	srv.Route("/").GET("/only-get", func(ctx kratosRest.Context) error { return ctx.String(200, "ok") })
	if code := restStatus(t, srv, http.MethodGet, "/only-get"); code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", code)
	}
	if code := restStatus(t, srv, http.MethodPost, "/only-get"); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: want 405, got %d", code)
	}
}
