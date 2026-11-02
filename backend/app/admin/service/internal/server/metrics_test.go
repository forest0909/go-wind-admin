package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/middleware"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/third_party/kratos-bootstrap/rpc"
)

const testOperation = "/test.v1.ItemService/Get"

func newMetricsServer(t *testing.T, m *Metrics) (*kratosHttp.Server, *bool) {
	t.Helper()
	spied := new(bool)
	spy := func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			*spied = true
			return next(ctx, req)
		}
	}
	mds := []middleware.Middleware{}
	if mm := m.Middleware(); mm != nil {
		mds = append(mds, mm)
	}
	mds = append(mds, spy)
	srv, err := rpc.CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{}}}, mds...)
	if err != nil {
		t.Fatal(err)
	}
	srv.Route("/").GET("/api/items/{id}", func(ctx kratosHttp.Context) error {
		kratosHttp.SetOperation(ctx, testOperation)
		h := ctx.Middleware(func(context.Context, any) (any, error) { return "ok", nil })
		out, err := h(ctx, nil)
		if err != nil {
			return err
		}
		return ctx.String(http.StatusOK, out.(string))
	})
	RegisterMetricsRoute(srv, m)
	return srv, spied
}

func doGet(srv *kratosHttp.Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestMetrics_Enabled(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatal(err)
	}
	srv, spied := newMetricsServer(t, m)

	get(srv, "/api/items/123")
	get(srv, "/api/items/456")
	*spied = false

	rec := get(srv, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d", rec.Code)
	}
	if *spied {
		t.Fatal("/metrics 不应经过中间件链（认证/审计）")
	}
	body := rec.Body.String()
	want := `server_requests_code_total{code="200",kind="http",operation="` + testOperation + `",reason=""} 2`
	if !strings.Contains(body, want) {
		t.Fatalf("缺少请求计数 %q:\n%s", want, firstLines(body, "server_requests"))
	}
	if strings.Contains(body, "/api/items/123") || strings.Contains(body, "/api/items/456") {
		t.Fatal("标签不应带原始 path")
	}
	for _, name := range []string{"server_requests_seconds_bucket", "go_goroutines", "process_cpu_seconds_total"} {
		if !strings.Contains(body, name) {
			t.Errorf("缺少指标 %s", name)
		}
	}

	get(srv, "/api/items/789")
	body = get(srv, "/metrics").Body.String()
	if !strings.Contains(body, strings.Replace(want, "} 2", "} 3", 1)) {
		t.Fatalf("请求后计数应增加到 3:\n%s", firstLines(body, "server_requests_code"))
	}
}

func TestMetrics_DisabledIs404(t *testing.T) {
	srv, _ := newMetricsServer(t, nil)
	if rec := get(srv, "/metrics"); rec.Code != http.StatusNotFound {
		t.Fatalf("关闭时 /metrics = %d，期望 404", rec.Code)
	}
}

func firstLines(s, prefix string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
