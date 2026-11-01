package logging

import (
	"context"

	"github.com/go-kratos/kratos/v2/middleware"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"

	"go-wind-admin/pkg/middleware/auth"
)

// 审计中间件挂在认证中间件外层（认证失败的请求也要记），看不到认证中间件注入的内层 ctx。
// 外层先放一个空的身份槽，认证通过后由 CaptureVerifiedIdentity 把已验签的身份写进去。
// 认证失败或白名单接口（login 等）槽保持为空，审计按匿名记录（user_id=0、tenant 0），
// 不再解码请求头里未验签的 JWT。

type identitySlotKey struct{}

type identitySlot struct {
	payload *authenticationV1.UserTokenPayload
}

func withIdentitySlot(ctx context.Context) (context.Context, *identitySlot) {
	slot := &identitySlot{}
	return context.WithValue(ctx, identitySlotKey{}, slot), slot
}

// CaptureVerifiedIdentity 必须紧跟在认证中间件之后：把 auth.FromContext 的已验签身份交给外层审计。
func CaptureVerifiedIdentity() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if slot, ok := ctx.Value(identitySlotKey{}).(*identitySlot); ok {
				if payload, err := auth.FromContext(ctx); err == nil {
					slot.payload = payload
				}
			}
			return handler(ctx, req)
		}
	}
}
