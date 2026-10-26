package logging

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/third_party/go-utils/auditutil"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
)

// sessionOnlyOperations 是会话维护类端点：虽为 POST 但不构成业务变更。
// refresh-token 会被前端定时刷新器周期触发、login 已有专门的登录审计，
// 记入操作审计只是持续噪音。
var sessionOnlyOperations = map[string]bool{
	adminV1.OperationAuthenticationServiceLogin:        true,
	adminV1.OperationAuthenticationServiceRefreshToken: true,
	adminV1.OperationAuthenticationServiceLogout:       true,
}

// Server is an server logging middleware.
func Server(opts ...Option) middleware.Middleware {
	op := options{
		loginOperations: []string{
			adminV1.OperationAuthenticationServiceLogin,
		},
		logoutOperation: adminV1.OperationAuthenticationServiceLogout,
	}
	for _, o := range opts {
		o(&op)
	}

	if op.ecPrivateKey == nil || op.ecPublicKey == nil {
		op.ecPrivateKey, op.ecPublicKey, _ = auditutil.GenerateECDSAKeyPair()
	}

	loginAuditLogMiddleware := NewLoginAuditLogMiddleware(&op)
	operationAuditLogMiddleware := NewOperationAuditLogMiddleware(&op)

	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (reply interface{}, err error) {
			startTime := time.Now()

			reply, err = handler(ctx, req)

			latencyMs := time.Since(startTime).Milliseconds()

			if tr, ok := transport.FromServerContext(ctx); ok {
				var htr *http.Transport
				if htr, ok = tr.(*http.Transport); ok {
					loginAuditLogMiddleware.Handle(ctx, htr, err)
					operationAuditLogMiddleware.Handle(ctx, htr, err, latencyMs)
				}
			}

			return
		}
	}
}
