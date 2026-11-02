package server

import (
	"bufio"
	"errors"
	"net"
	nethttp "net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
)

// unmatchedRoute 是没匹配到任何路由（404/405）的请求使用的固定标签值，绝不使用原始 URL。
const unmatchedRoute = "unmatched"

// Metrics 持有 Prometheus 注册表和请求指标。仅在 server.rest.enable_metrics=true 时创建。
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	seconds  *prometheus.HistogramVec

	routerOnce sync.Once
	router     *mux.Router
}

// NewMetrics 用独立注册表（不污染全局）装配 Go runtime + process 默认采集和请求指标：
//
//	http_requests_total{route,method,code}
//	http_request_duration_seconds_{bucket,sum,count}{route,method}
//
// route 是路由模板（如 /admin/v1/users/{id}），没匹配到路由时为 "unmatched"，不是原始 URL，避免高基数。
// code 是客户端实际收到的最终 HTTP 状态码。
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by route template, method and final status code.",
		}, []string{"route", "method", "code"}),
		seconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP request duration by route template and method.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"route", "method"}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.seconds,
	)
	return m
}

// 探针和指标端点本身不计入请求指标。
func metricsExcluded(path string) bool {
	return path == "/metrics" || path == "/healthz" || path == "/readyz"
}

// Install 把请求指标装在 REST 服务最外层（包住 CORS、路由、解码、全部 RPC 中间件含 recovery 和错误映射），
// 所以按最终写给客户端的状态码计数：映射后的 404/409、recovery 后的 500、路由 404、解码 400 都各算一次。
// 并注册 GET /metrics：与 /healthz 一样用 srv.Route 手工注册，不经认证、授权、访问日志和审计。
// 该端点不鉴权，只应在内网可达。m 为 nil（未开启）时什么都不做，/metrics 为 404。
func (m *Metrics) Install(srv *kratosHttp.Server) {
	if m == nil {
		return
	}
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	srv.Route("/").GET("/metrics", func(ctx kratosHttp.Context) error {
		h.ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	})

	next := srv.Handler
	srv.Handler = nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if metricsExcluded(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: nethttp.StatusOK}
		next.ServeHTTP(rw, r)
		route, method := m.routeTemplate(srv, r), metricMethod(r.Method)
		m.requests.WithLabelValues(route, method, strconv.Itoa(rw.status)).Inc()
		m.seconds.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
	})
}

// routeTemplate 用路由表的影子 mux 求出请求命中的路由模板。
// 外层拿不到 gorilla 在内层设置的当前路由，而路由都在启动时注册完毕，首个请求时按 WalkRoute 建一次即可。
func (m *Metrics) routeTemplate(srv *kratosHttp.Server, r *nethttp.Request) string {
	m.routerOnce.Do(func() {
		m.router = mux.NewRouter()
		_ = srv.WalkRoute(func(info kratosHttp.RouteInfo) error {
			m.router.Handle(info.Path, nethttp.NotFoundHandler()).Methods(info.Method)
			return nil
		})
	})
	var match mux.RouteMatch
	if m.router.Match(r, &match) && match.Route != nil {
		if tpl, err := match.Route.GetPathTemplate(); err == nil {
			return tpl
		}
	}
	return unmatchedRoute
}

// metricMethod 只放行标准方法，其余归为 OTHER，防止客户端用随意方法名制造序列。
func metricMethod(method string) string {
	switch method {
	case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodPost, nethttp.MethodPut,
		nethttp.MethodPatch, nethttp.MethodDelete, nethttp.MethodOptions:
		return method
	}
	return "OTHER"
}

// statusRecorder 记录最终状态码，同时保持 Flusher/Hijacker 能力。
type statusRecorder struct {
	nethttp.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status, s.wroteHeader = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(nethttp.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(nethttp.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("metrics: underlying ResponseWriter does not support hijacking")
}

func (s *statusRecorder) Unwrap() nethttp.ResponseWriter { return s.ResponseWriter }
