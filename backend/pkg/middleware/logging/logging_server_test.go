// logging_server_test.go —— Server() 外层中间件测试。
//
// 覆盖内容：
//  1. 中间件链的 handler 透传语义：无 transport 与非 *http.Transport 传输层时，
//     业务 handler 照常执行、返回值原样透传，登录和操作审计全部跳过；
//  2. 审计分发：POST/PUT/DELETE + 可解析写操作进操作审计，GET 与会话维护端点跳过；
//     登录/登出端点只进登录审计；未注册路由无审计。
//
// 分发矩阵走 harness 的进程内 server（见 harness_test.go），全程无 socket。
package logging

import (
	"context"
	nethttp "net/http"
	"testing"

	"github.com/go-kratos/kratos/v2/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
)

// ---------------------------------------------------------------------------
// 非 *http.Transport 的传输层假件（覆盖类型断言 false 分支）
// ---------------------------------------------------------------------------

// fakeHeader 以 map 为底座实现 kratos transport.Header 接口。
type fakeHeader struct{ m map[string][]string }

func (h *fakeHeader) Get(key string) string {
	if v, ok := h.m[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func (h *fakeHeader) Set(key, value string) { h.m[key] = []string{value} }
func (h *fakeHeader) Add(key, value string) { h.m[key] = append(h.m[key], value) }
func (h *fakeHeader) Keys() []string {
	out := make([]string, 0, len(h.m))
	for k := range h.m {
		out = append(out, k)
	}
	return out
}
func (h *fakeHeader) Values(key string) []string { return h.m[key] }

// fakeTransporter 满足 transport.Transporter 但不是 *http.Transport：
// Server() 对它必须跳过全部审计（快照与落库两个阶段各有一处类型断言）。
type fakeTransporter struct{ op string }

func (f *fakeTransporter) Kind() transport.Kind { return transport.KindHTTP }
func (f *fakeTransporter) Endpoint() string     { return "fake://endpoint" }
func (f *fakeTransporter) Operation() string    { return f.op }
func (f *fakeTransporter) RequestHeader() transport.Header {
	return &fakeHeader{m: map[string][]string{}}
}
func (f *fakeTransporter) ReplyHeader() transport.Header {
	return &fakeHeader{m: map[string][]string{}}
}

// ---------------------------------------------------------------------------
// handler 透传与审计跳过
// ---------------------------------------------------------------------------

// newDirectCaptureStubOpts 构造登录和操作记录型写入桩，用于不经 server 的直调测试。
func newDirectCaptureStubOpts(t *testing.T, cap *auditCapture) []Option {
	t.Helper()
	return []Option{
		WithWriteLoginLogFunc(func(ctx context.Context, d *auditV1.LoginAuditLog) error {
			cap.login = append(cap.login, d)
			cap.loginMeta = append(cap.loginMeta, cap.metaOf(ctx))
			return nil
		}),
		WithWriteOperationAuditLogFunc(func(ctx context.Context, d *auditV1.OperationAuditLog) error {
			cap.operation = append(cap.operation, d)
			cap.operationMeta = append(cap.operationMeta, cap.metaOf(ctx))
			return nil
		}),
	}
}

// TestServerPassesThroughWithoutTransport 验证：ctx 无传输层时，Server() 包装的
// handler 照常执行且返回值原样透传，五个审计桩零写入——审计必须在真实
// *http.Transport 下才有意义，缺传输层不是静默构造脏记录的理由。
func TestServerPassesThroughWithoutTransport(t *testing.T) {
	cap := &auditCapture{}
	mw := Server(newDirectCaptureStubOpts(t, cap)...)

	innerCalled := false
	inner := func(ctx context.Context, req interface{}) (interface{}, error) {
		innerCalled = true
		return "reply-marker", nil
	}
	wrapped := mw(inner)

	out, err := wrapped(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "reply-marker", out)
	require.True(t, innerCalled, "业务 handler 必须被调用")
	require.Zero(t, cap.total(), "无传输层时不得有任何审计写入")
}

// TestServerSkipsAuditForNonHTTPTransport 验证：传输层不是 kratos *http.Transport
// （如 gRPC 或假件）时，handler 照常执行、返回值透传，登录和操作审计零写入。
func TestServerSkipsAuditForNonHTTPTransport(t *testing.T) {
	cap := &auditCapture{}
	mw := Server(newDirectCaptureStubOpts(t, cap)...)

	inner := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "reply-marker", nil
	}
	wrapped := mw(inner)

	fakeCtx := transport.NewServerContext(context.Background(),
		&fakeTransporter{op: "/demo.v1.GadgetService/Update"})

	out, err := wrapped(fakeCtx, nil)
	require.NoError(t, err)
	require.Equal(t, "reply-marker", out)
	require.Zero(t, cap.total(), "非 HTTP 传输层不得触发任何审计写入")
}

// ---------------------------------------------------------------------------
// 审计分发矩阵
// ---------------------------------------------------------------------------

// dispatchCase 一条分发场景：指定请求形态与登录、操作审计的期望写入数。
type dispatchCase struct {
	name        string
	method      string
	path        string
	op          string
	expectLogin int
	expectOp    int
	expect404   bool
}

// TestServerDispatchMatrix 验证 Server() 把请求分发给登录和操作审计：
//   - POST/PUT/DELETE + 可解析写操作 → 一条操作审计；
//   - GET、解析不出资源的 operation、登录和登出 → 不进操作审计；
//   - 登录和登出只进登录审计；
//   - 未注册的路由方法（PATCH）→ mux 404，中间件链不运行。
func TestServerDispatchMatrix(t *testing.T) {
	cases := []dispatchCase{
		{
			name: "POST可解析写操作", method: nethttp.MethodPost, path: "/case/5",
			op: "/demo.v1.GadgetService/Update", expectOp: 1,
		},
		{
			name: "GET读请求不进操作审计", method: nethttp.MethodGet, path: "/case/5",
			op: "/demo.v1.GadgetService/Update",
		},
		{
			name: "登录端点仅登录审计", method: nethttp.MethodPost, path: "/case/5",
			op: adminV1.OperationAuthenticationServiceLogin, expectLogin: 1,
		},
		{
			name: "登出端点仅登录审计", method: nethttp.MethodPost, path: "/case/5",
			op: adminV1.OperationAuthenticationServiceLogout, expectLogin: 1,
		},
		{
			name: "解析不出资源的operation不落库", method: nethttp.MethodPost, path: "/case/5",
			op: "plain-bad-op",
		},
		{
			name: "未注册方法PATCH零审计", method: nethttp.MethodPatch, path: "/case/5",
			op: "/demo.v1.GadgetService/Update", expect404: true,
		},
		{
			name: "PUT写方法进操作审计", method: nethttp.MethodPut, path: "/case/5",
			op: "/demo.v1.GadgetService/Create", expectOp: 1,
		},
		{
			name: "DELETE写方法进操作审计", method: nethttp.MethodDelete, path: "/case/5",
			op: "/demo.v1.GadgetService/Delete", expectOp: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAuditServer(t)
			rec := env.fire(tc.method, tc.path, map[string]string{
				"X-Test-Operation": tc.op,
			}, "", "127.0.0.1:1234")

			if tc.expect404 {
				require.Equal(t, 404, rec.Code, "未注册路由必须 404")
			} else {
				require.Equal(t, 200, rec.Code)
			}
			require.Len(t, env.capture.login, tc.expectLogin, "登录审计写入数")
			require.Len(t, env.capture.operation, tc.expectOp, "操作审计写入数")
			for _, m := range env.capture.operationMeta {
				require.True(t, m.SystemContext, "操作审计写入必须以系统 viewer 执行")
			}
			for _, m := range env.capture.loginMeta {
				require.True(t, m.SystemContext, "登录审计写入必须以系统 viewer 执行")
			}
		})
	}
}

// TestServerEmptyLoginOperationListOption 验证 WithLoginOperation() 空名单
// 是全量替换：默认登录端点不再进登录审计。操作审计仍按硬编码会话名单跳过。
func TestServerEmptyLoginOperationListOption(t *testing.T) {
	env := newAuditServer(t, WithLoginOperation())
	env.fire(nethttp.MethodPost, "/case/5", map[string]string{
		"X-Test-Operation": adminV1.OperationAuthenticationServiceLogin,
	}, "", "127.0.0.1:1234")
	assert.Empty(t, env.capture.login, "空名单下登录端点不进登录审计")
	assert.Empty(t, env.capture.operation, "会话维护端点仍被操作审计跳过")
}

// TestServerCustomLoginLogoutOperationOptions 验证自定义名单的替换语义：
// 自定义登录端点进登录审计；默认登出端点因名单被替换不再进登录审计；
// 自定义登出端点经 WithLogoutOperation 生效，动作为 LOGOUT。
func TestServerCustomLoginLogoutOperationOptions(t *testing.T) {
	env := newAuditServer(t,
		WithLoginOperation("custom.login"),
		WithLogoutOperation("custom.logout"),
	)

	type expect struct {
		name       string
		op         string
		loginDelta int
		loginType  auditV1.LoginAuditLog_ActionType
	}
	cases := []expect{
		{"自定义登录端点进登录审计", "custom.login", 1, auditV1.LoginAuditLog_LOGIN},
		{"默认登出端点被名单替换跳过登录审计", adminV1.OperationAuthenticationServiceLogout, 0, 0},
		{"自定义登出端点生效", "custom.logout", 1, auditV1.LoginAuditLog_LOGOUT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loginBase := len(env.capture.login)
			env.fire(nethttp.MethodPost, "/case/5", map[string]string{
				"X-Test-Operation": tc.op,
			}, "", "127.0.0.1:1234")
			assert.Len(t, env.capture.login, loginBase+tc.loginDelta, "登录审计写入增量")
			if tc.loginDelta == 1 {
				assert.Equal(t, tc.loginType, env.capture.login[loginBase].GetActionType())
			}
			assert.Empty(t, env.capture.operation, "这些端点解析不出资源，操作审计跳过")
		})
	}
}
