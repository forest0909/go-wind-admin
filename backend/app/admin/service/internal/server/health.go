package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
)

// readinessTimeout 单个依赖探测的超时。
// 用 var 是为了让测试缩短期限。
var readinessTimeout = 2 * time.Second

// HealthCheck 一个就绪探测项。Name 会原样出现在 503 响应里，只放 "database"、"redis" 这类固定名字。
type HealthCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// RegisterHealthRoutes 注册 /healthz（存活）和 /readyz（就绪）。
//
// 用 srv.Route 手工注册：没有 Operation，不经过 Kratos 中间件链（认证、Casbin、访问日志、
// 审计日志都不会碰到），探针的高频请求不会写进日志，也不需要令牌。
// 探测失败只在服务端日志里记原始错误，响应只带失败的依赖名，不泄露连接串或错误细节。
func RegisterHealthRoutes(srv *kratosHttp.Server, checks ...HealthCheck) {
	r := srv.Route("/")
	r.GET("/healthz", func(ctx kratosHttp.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})
	r.GET("/readyz", func(ctx kratosHttp.Context) error {
		for _, c := range checks {
			err := runCheck(ctx.Request().Context(), c)
			if err != nil {
				log.Errorf("readiness check %s failed: %v", c.Name, err)
				return ctx.String(http.StatusServiceUnavailable, c.Name+" unavailable")
			}
		}
		return ctx.String(http.StatusOK, "ok")
	})
}

// runCheck 在独立 goroutine 里跑探测并按 readinessTimeout 兜底：
// 共享的 Redis 客户端没开 ContextTimeoutEnabled，Ping 不一定遵守 ctx 期限，
// 所以不能只靠 ctx，期限一到就按该依赖失败处理。channel 带缓冲，迟到的结果写入后 goroutine 即退出。
func runCheck(parent context.Context, c HealthCheck) error {
	cctx, cancel := context.WithTimeout(parent, readinessTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Check(cctx) }()
	select {
	case err := <-done:
		return err
	case <-cctx.Done():
		return cctx.Err()
	}
}
