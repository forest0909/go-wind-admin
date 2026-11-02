package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/third_party/kratos-bootstrap/rpc"
)

func newHealthServer(t *testing.T, checks ...HealthCheck) (*kratosHttp.Server, *bool) {
	t.Helper()
	called := new(bool)
	// 任何经过中间件链的请求都会置位 called；健康检查路由不应触发。
	spy := func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			*called = true
			return next(ctx, req)
		}
	}
	srv, err := rpc.CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{}}}, spy)
	if err != nil {
		t.Fatal(err)
	}
	RegisterHealthRoutes(srv, checks...)
	return srv, called
}

func get(srv *kratosHttp.Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthz_OKWithoutTokenOrMiddleware(t *testing.T) {
	failing := HealthCheck{Name: "database", Check: func(context.Context) error { return errors.New("down") }}
	srv, called := newHealthServer(t, failing)
	if rec := get(srv, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200 even when dependencies are down", rec.Code)
	}
	if *called {
		t.Fatal("healthz went through the middleware chain (authz/audit)")
	}
}

func TestReadyz_OKWhenAllDependenciesUp(t *testing.T) {
	ok := func(context.Context) error { return nil }
	srv, called := newHealthServer(t, HealthCheck{Name: "database", Check: ok}, HealthCheck{Name: "redis", Check: ok})
	if rec := get(srv, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", rec.Code)
	}
	if *called {
		t.Fatal("readyz went through the middleware chain (authz/audit)")
	}
}

func TestReadyz_503NamesFailedDependencyWithoutLeakingError(t *testing.T) {
	ok := func(context.Context) error { return nil }
	for _, failed := range []string{"database", "redis"} {
		var checks []HealthCheck
		for _, name := range []string{"database", "redis"} {
			c := HealthCheck{Name: name, Check: ok}
			if name == failed {
				c.Check = func(context.Context) error { return errors.New("dial tcp user:secret@10.0.0.1: refused") }
			}
			checks = append(checks, c)
		}
		srv, called := newHealthServer(t, checks...)
		rec := get(srv, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s down: readyz = %d, want 503", failed, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, failed) || strings.Contains(body, "secret") {
			t.Fatalf("%s down: unexpected body %q", failed, body)
		}
		if *called {
			t.Fatal("readyz went through the middleware chain (authz/audit)")
		}
	}
}

// 审查：Redis 客户端不遵守 ctx 期限时，迟到的成功结果不能让 /readyz 返回 200。
func TestReadyz_503WhenCheckIgnoresDeadline(t *testing.T) {
	old := readinessTimeout
	readinessTimeout = 100 * time.Millisecond
	defer func() { readinessTimeout = old }()

	release := make(chan struct{})
	defer close(release)
	srv, _ := newHealthServer(t, HealthCheck{Name: "redis", Check: func(context.Context) error {
		<-release // 忽略 ctx，一直阻塞
		return nil
	}})
	start := time.Now()
	rec := get(srv, "/readyz")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "redis") {
		t.Fatalf("readyz = %d %q, want 503 naming redis", rec.Code, rec.Body.String())
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("readyz took %v, want about the deadline", d)
	}
}
