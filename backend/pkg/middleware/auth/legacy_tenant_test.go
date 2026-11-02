package auth

import (
	"testing"

	"github.com/stretchr/testify/require"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/third_party/go-utils/trans"
)

// 令牌校验实现返回了 tid=7 的有效载荷（例如换了校验实现），中间件仍拒绝，不进入业务 handler。
// 改动前（审查 R1 探针）handler 被调用且 viewer.TenantID()==7。
func TestServer_RejectsLegacyTenantToken(t *testing.T) {
	reached, _, err := runAuthMiddleware(t, newTransportCtx("Bearer existing-valid-session"), nil,
		validChecker(&authenticationV1.UserTokenPayload{UserId: 77, TenantId: trans.Ptr(uint32(7))}),
		WithEnableAuthority(false), WithInjectEnt(true))
	require.ErrorIs(t, err, ErrLegacyTenantToken)
	require.False(t, reached)

	// tid=0 显式写出也照常放行
	reached, _, err = runAuthMiddleware(t, newTransportCtx("Bearer t0"), nil,
		validChecker(&authenticationV1.UserTokenPayload{UserId: 1, TenantId: trans.Ptr(uint32(0))}),
		WithEnableAuthority(false), WithInjectEnt(true))
	require.NoError(t, err)
	require.True(t, reached)
}
