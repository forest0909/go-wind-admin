package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	"go-wind-admin/app/admin/service/internal/data/ent"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"go-wind-admin/third_party/kratos-bootstrap/rpc"
)

const testOperation = "/test.v1.ItemService/Get"

// newMetricsServer 起一个带真实 recovery + ErrorMapping 的 REST 服务（与 NewRestMiddleware 同序），
// 路由 /api/items/{id} 按 id 触发不同结果。
func newMetricsServer(t *testing.T, m *Metrics) (*kratosHttp.Server, *bool) {
	t.Helper()
	spied := new(bool)
	spy := func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			*spied = true
			return next(ctx, req)
		}
	}
	srv, err := rpc.CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{}}},
		recovery.Recovery(), ErrorMapping(bLogger.NewHelper(bLogger.NopLogger())), spy)
	if err != nil {
		t.Fatal(err)
	}
	srv.Route("/").GET("/api/items/{id}", func(ctx kratosHttp.Context) error {
		kratosHttp.SetOperation(ctx, testOperation)
		h := ctx.Middleware(func(ctx context.Context, _ any) (any, error) {
			vars := ctx.(kratosHttp.Context).Vars()
			switch vars["id"][0] {
			case "notfound":
				return nil, &ent.NotFoundError{}
			case "conflict":
				return nil, &ent.ConstraintError{}
			case "panic":
				panic("boom")
			case "unauth":
				return nil, kratosErrors.Unauthorized("UNAUTHORIZED", "no token")
			}
			return "ok", nil
		})
		out, err := h(ctx, nil)
		if err != nil {
			return err
		}
		return ctx.String(http.StatusOK, out.(string))
	})
	srv.Route("/").POST("/api/items", func(ctx kratosHttp.Context) error {
		var body struct{ Name string }
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		return ctx.String(http.StatusOK, "ok")
	})
	m.Install(srv)
	return srv, spied
}

func doGet(srv *kratosHttp.Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func metricsBody(srv *kratosHttp.Server) string { return doGet(srv, "/metrics").Body.String() }

func series(route, method, code string) string {
	return `http_requests_total{code="` + code + `",method="` + method + `",route="` + route + `"} 1`
}

func TestMetrics_Enabled(t *testing.T) {
	srv, spied := newMetricsServer(t, NewMetrics())

	for _, id := range []string{"1", "2"} {
		doGet(srv, "/api/items/"+id)
	}
	*spied = false
	rec := doGet(srv, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d", rec.Code)
	}
	if *spied {
		t.Fatal("/metrics 不应经过中间件链（认证/审计）")
	}
	body := rec.Body.String()
	want := `http_requests_total{code="200",method="GET",route="/api/items/{id}"} 2`
	if !strings.Contains(body, want) {
		t.Fatalf("缺少请求计数 %q:\n%s", want, firstLines(body, "http_requests"))
	}
	for _, name := range []string{"http_request_duration_seconds_bucket", "go_goroutines", "process_cpu_seconds_total"} {
		if !strings.Contains(body, name) {
			t.Errorf("缺少指标 %s", name)
		}
	}
	doGet(srv, "/api/items/3")
	if !strings.Contains(metricsBody(srv), strings.Replace(want, "} 2", "} 3", 1)) {
		t.Fatal("请求后计数应增加到 3")
	}
	// /metrics 自己不计入
	if strings.Contains(metricsBody(srv), `route="/metrics"`) {
		t.Fatal("/metrics 不应计入请求指标")
	}
}

// 按客户端实际收到的最终状态码计数；标签是路由模板或 unmatched，绝不是原始 URL。
func TestMetrics_FinalStatusAndRouteLabel(t *testing.T) {
	srv, _ := newMetricsServer(t, NewMetrics())
	tpl := "/api/items/{id}"

	cases := []struct {
		name         string
		do           func() *httptest.ResponseRecorder
		route, code  string
		method       string
		wantHTTPCode int
	}{
		{"ent NotFound→404", func() *httptest.ResponseRecorder { return doGet(srv, "/api/items/notfound") }, tpl, "404", "GET", 404},
		{"ent Constraint→409", func() *httptest.ResponseRecorder { return doGet(srv, "/api/items/conflict") }, tpl, "409", "GET", 409},
		{"panic→500", func() *httptest.ResponseRecorder { return doGet(srv, "/api/items/panic") }, tpl, "500", "GET", 500},
		{"401", func() *httptest.ResponseRecorder { return doGet(srv, "/api/items/unauth") }, tpl, "401", "GET", 401},
		{"不存在的路由→404 unmatched", func() *httptest.ResponseRecorder { return doGet(srv, "/no/such/path/42") }, "unmatched", "404", "GET", 404},
		{"解码失败→400", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader("{bad json"))
			req.Header.Set("Content-Type", "application/json")
			srv.ServeHTTP(rec, req)
			return rec
		}, "/api/items", "400", "POST", 400},
	}
	for _, c := range cases {
		if got := c.do().Code; got != c.wantHTTPCode {
			t.Fatalf("%s: 实际响应 %d", c.name, got)
		}
	}
	body := metricsBody(srv)
	for _, c := range cases {
		if want := series(c.route, c.method, c.code); !strings.Contains(body, want) {
			t.Errorf("%s: 缺少 %s\n%s", c.name, want, firstLines(body, "http_requests"))
		}
	}
	if strings.Contains(body, "notfound") || strings.Contains(body, "/no/such") {
		t.Fatal("标签不应带原始 URL")
	}
}

func TestMetrics_DisabledIs404(t *testing.T) {
	srv, _ := newMetricsServer(t, nil)
	if rec := doGet(srv, "/metrics"); rec.Code != http.StatusNotFound {
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
