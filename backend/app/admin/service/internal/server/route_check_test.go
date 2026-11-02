package server

import (
	"context"
	"reflect"
	"testing"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/cmd/server/assets"
)

// newRestServerForRouteTest 用零值 Service 调真实的 NewRestServer：注册函数只保存 handler 闭包，
// 不调用 Service，所以零值足以拿到完整的路由表。
func newRestServerForRouteTest(t *testing.T) *kratosHttp.Server {
	t.Helper()
	cfg := &conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{Addr: "127.0.0.1:0"}}}
	boot := bootstrap.NewContextWithParam(context.Background(), nil, cfg, bLogger.NopLogger())

	ctor := reflect.ValueOf(NewRestServer)
	args := make([]reflect.Value, ctor.Type().NumIn())
	for i := range args {
		pt := ctor.Type().In(i)
		switch {
		case pt == reflect.TypeOf(boot):
			args[i] = reflect.ValueOf(boot)
		case pt.Kind() == reflect.Pointer && pt.Elem().Kind() == reflect.Struct && pt.Elem().Name() != "Authorizer":
			args[i] = reflect.New(pt.Elem())
		default:
			args[i] = reflect.Zero(pt) // authorizer=nil 跳过策略装载；middlewares=nil
		}
	}
	out := ctor.Call(args)
	if errV := out[1]; !errV.IsNil() {
		t.Fatalf("NewRestServer: %v", errV.Interface())
	}
	srv, _ := out[0].Interface().(*kratosHttp.Server)
	if srv == nil {
		t.Fatal("NewRestServer returned nil server")
	}
	return srv
}

// TestEveryOpenAPIOperationHasRoute 内嵌 openapi 的每个接口都必须在 NewRestServer 里注册了路由。
// 漏了 Register*HTTPServer 调用时编译照过、Api 表和权限点照样同步，请求却 404。
func TestEveryOpenAPIOperationHasRoute(t *testing.T) {
	srv := newRestServerForRouteTest(t)
	missing, err := openAPIOperationsWithoutRoute(srv, assets.OpenApiData)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("内嵌 openapi 里有 %d 个接口没注册路由: %v", len(missing), missing)
	}
}

// TestOpenAPIOperationsWithoutRoute_DetectsMissing 自检：少注册一个服务必须被查出来。
func TestOpenAPIOperationsWithoutRoute_DetectsMissing(t *testing.T) {
	srv := kratosHttp.NewServer()
	adminV1.RegisterDictTypeServiceHTTPServer(srv, nil)
	missing, err := openAPIOperationsWithoutRoute(srv, assets.OpenApiData)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"GET /admin/v1/dict/entries": true, "GET /admin/v1/dict/types": false}
	got := map[string]bool{}
	for _, m := range missing {
		got[m] = true
	}
	for key, shouldMiss := range want {
		if got[key] != shouldMiss {
			t.Errorf("%s: missing=%v, want %v", key, got[key], shouldMiss)
		}
	}
}
