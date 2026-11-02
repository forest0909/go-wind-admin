package server

import (
	"github.com/go-kratos/kratos/v2/middleware"
	kratosMetrics "github.com/go-kratos/kratos/v2/middleware/metrics"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelProm "go.opentelemetry.io/otel/exporters/prometheus"
	sdkMetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics 持有 Prometheus 注册表和 Kratos metrics 中间件。仅在 server.rest.enable_metrics=true 时创建。
type Metrics struct {
	registry   *prometheus.Registry
	middleware middleware.Middleware
}

// NewMetrics 用独立的注册表（不污染全局）装配：Go runtime + process 默认采集，
// 以及 Kratos metrics 中间件经 OpenTelemetry → Prometheus exporter 产生的请求指标：
//
//	server_requests_code_total{kind,operation,code,reason}
//	server_requests_seconds_{bucket,sum,count}{kind,operation}
//
// 标签是 proto operation（如 /admin.service.v1.UserService/List），不是原始 URL path，避免高基数。
func NewMetrics() (*Metrics, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	exporter, err := otelProm.New(otelProm.WithRegisterer(reg), otelProm.WithoutScopeInfo(), otelProm.WithoutTargetInfo())
	if err != nil {
		return nil, err
	}
	meter := sdkMetric.NewMeterProvider(sdkMetric.WithReader(exporter)).Meter("go-wind-admin/rest")

	// 名字经 exporter 后：计数器自动加 _total，单位 s 的直方图自动加 _seconds。
	requests, err := kratosMetrics.DefaultRequestsCounter(meter, "server_requests_code")
	if err != nil {
		return nil, err
	}
	seconds, err := kratosMetrics.DefaultSecondsHistogram(meter, "server_requests")
	if err != nil {
		return nil, err
	}
	return &Metrics{
		registry:   reg,
		middleware: kratosMetrics.Server(kratosMetrics.WithRequests(requests), kratosMetrics.WithSeconds(seconds)),
	}, nil
}

// Middleware 返回请求指标中间件。m 为 nil（未开启）时返回 nil。
func (m *Metrics) Middleware() middleware.Middleware {
	if m == nil {
		return nil
	}
	return m.middleware
}

// RegisterMetricsRoute 注册 GET /metrics。与 /healthz 一样用 srv.Route 手工注册：
// 不经过认证、授权、访问日志和审计。该端点不鉴权，只应在内网可达。m 为 nil 时不注册（404）。
func RegisterMetricsRoute(srv *kratosHttp.Server, m *Metrics) {
	if m == nil {
		return
	}
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	srv.Route("/").GET("/metrics", func(ctx kratosHttp.Context) error {
		h.ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	})
}
