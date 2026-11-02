package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
	"go.opentelemetry.io/otel"
	traceSdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	applogging "go-wind-admin/pkg/middleware/logging"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"go-wind-admin/third_party/kratos-bootstrap/rpc"
	"go-wind-admin/third_party/kratos-bootstrap/tracer"
)

// 有 TracerProvider 时 tracing 中间件产生 span，访问日志带出同一个 trace_id。
func TestTracing_SpanAndAccessLogTraceID(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := traceSdk.NewTracerProvider(traceSdk.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	var buf bytes.Buffer
	lg := bLogger.WrapTrace(bLogger.NewSlogLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	srv, err := rpc.CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{
		Middleware: &conf.Middleware{EnableTracing: true},
	}}}, applogging.AccessLogServer(bLogger.AsKratosLogger(lg)))
	if err != nil {
		t.Fatal(err)
	}
	srv.Route("/").GET("/x", func(ctx kratosHttp.Context) error {
		kratosHttp.SetOperation(ctx, testOperation)
		h := ctx.Middleware(func(context.Context, any) (any, error) { return "ok", nil })
		if _, err := h(ctx, nil); err != nil {
			return err
		}
		return ctx.String(http.StatusOK, "ok")
	})

	doGet(srv, "/x")

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("有 TracerProvider 时应产生 span")
	}
	traceID := spans[0].SpanContext().TraceID().String()
	if !strings.Contains(buf.String(), traceID) {
		t.Fatalf("访问日志应带 trace_id=%s，实际日志：%s", traceID, buf.String())
	}
}

// 未配置 exporter（endpoint 为空）时初始化不报错；配了 endpoint 但 exporter 名无效则明确报错。
func TestTracing_ProviderWithoutExporter(t *testing.T) {
	info := &conf.AppInfo{AppId: "t", Project: "p", Version: "v", InstanceId: "i"}
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { _ = tracer.ShutdownTracerProvider(context.Background()); otel.SetTracerProvider(prev) })

	if err := tracer.NewTracerProvider(context.Background(), nil, info); err != nil {
		t.Fatalf("trace 未配置不应报错: %v", err)
	}
	if err := tracer.NewTracerProvider(context.Background(), &conf.Tracer{}, info); err != nil {
		t.Fatalf("trace 空配置不应报错: %v", err)
	}
	if err := tracer.NewTracerProvider(context.Background(), &conf.Tracer{Exporter: "nope", Endpoint: "x:1"}, info); err == nil {
		t.Fatal("未知 exporter 应报错")
	}
}
