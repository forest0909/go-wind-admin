package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/third_party/kratos-bootstrap/rpc"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"

	"go-wind-admin/app/admin/service/internal/service"
)

// newRejectingRestServer 用真实的 rpc.CreateRestServer 起服务，鉴权中间件一律拒绝（401），
// 记下中间件有没有被调用。路由按生产方式注册，svc 传 nil。
func newRejectingRestServer(t *testing.T) (*kratosHttp.Server, *bool) {
	t.Helper()
	called := new(bool)
	reject := func(middleware.Handler) middleware.Handler {
		return func(context.Context, any) (any, error) {
			*called = true
			return nil, errors.Unauthorized("NO_AUTH", "no token")
		}
	}
	srv, err := rpc.CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{}}}, reject)
	if err != nil {
		t.Fatal(err)
	}
	adminV1.RegisterUserProfileServiceHTTPServer(srv, nil)
	registerFileTransferServiceHandler(srv, nil)
	installRequestBodyLimit(srv)
	return srv, called
}

func jsonBody(field string, n int64) *countingReader {
	return &countingReader{r: io.MultiReader(
		strings.NewReader(`{"`+field+`":"`),
		io.LimitReader(zeroReader{}, n),
		strings.NewReader(`"}`),
	)}
}

// 审查 R2：未登录、chunked 的超大头像请求，修复前会被完整读入（io.ReadAll）再走鉴权返回 401。
// 现在在最外层读到上限就返回 413，不进 Bind 和鉴权中间件。
func TestRequestBodyLimit_AvatarChunkedRejectedBeforeBind(t *testing.T) {
	srv, called := newRejectingRestServer(t)
	body := jsonBody("imageBase64", maxUploadRequestBytes+1<<20)
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/me/avatar", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if *called {
		t.Fatal("auth middleware must not run for an oversized body")
	}
	// 长度未知时只能读到 limit+1 才知道超限，不会多读。
	if body.n > defaultRequestBodyBytes+1 {
		t.Fatalf("read %d bytes, default limit is %d", body.n, defaultRequestBodyBytes)
	}
}

// Content-Length 已知且超限：一个字节都不读。
func TestRequestBodyLimit_AvatarContentLengthRejectedWithoutReading(t *testing.T) {
	srv, called := newRejectingRestServer(t)
	body := jsonBody("imageBase64", defaultRequestBodyBytes)
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/me/avatar", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = defaultRequestBodyBytes + 1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge || *called || body.n != 0 {
		t.Fatalf("code=%d called=%v read=%d", rec.Code, *called, body.n)
	}
}

// 上限以内的头像请求（5 MiB 图片的 base64）照常往下走，到鉴权中间件。
func TestRequestBodyLimit_AvatarWithinLimitPassesThrough(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		srv, called := newRejectingRestServer(t)
		n := (service.MaxAvatarBytes*4 + 2) / 3
		body := jsonBody("imageBase64", n)
		req := httptest.NewRequest(http.MethodPost, "/admin/v1/me/avatar", body)
		req.Header.Set("Content-Type", "application/json")
		if chunked {
			req.ContentLength = -1
		} else {
			req.ContentLength = n + int64(len(`{"imageBase64":""}`))
		}
		rec := httptest.NewRecorder()

		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized || !*called {
			t.Fatalf("chunked=%v: want request to reach auth (401), got %d called=%v: %s", chunked, rec.Code, *called, rec.Body.String())
		}
	}
}

// 文件上传接口保留大上限：超过默认上限、低于上传上限的 chunked 请求能到鉴权中间件。
func TestRequestBodyLimit_UploadKeepsLargerLimit(t *testing.T) {
	srv, called := newRejectingRestServer(t)
	body := jsonBody("file", defaultRequestBodyBytes*2)
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/file/upload", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || !*called {
		t.Fatalf("want 401 from auth, got %d called=%v: %s", rec.Code, *called, rec.Body.String())
	}
}
