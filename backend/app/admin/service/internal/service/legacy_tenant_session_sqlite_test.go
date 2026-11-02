package service

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
)

// 升级前租户 7 的用户：角色和后台访问权限都还在，Redis 里留着当时签发的令牌对。
func seedLegacyTenantSession(t *testing.T, e *authSvcEnv) (access, refresh string) {
	t.Helper()
	pid := e.seedBackendAccessPermission(t)
	tid := trans.Ptr(uint32(7))
	rid := e.seedRole(t, tid, permissionV1.Role_TENANT, "tenant:manager", []uint32{pid}, nil, nil)
	e.seedStubUser(authSvcTestUserID, tid, authSvcTestUsername, identityV1.User_NORMAL, []uint32{rid})
	payload := &authenticationV1.UserTokenPayload{UserId: authSvcTestUserID, TenantId: tid, Username: trans.Ptr(authSvcTestUsername)}
	access, refresh, err := e.svc.authenticator.CreateUserToken(context.Background(), authenticationV1.ClientType_admin, payload)
	require.NoError(t, err)
	return access, refresh
}

// 旧租户的 refresh token 不能再换出新令牌。改动前（审查 R1 探针）会签出 tid=7 的新 access token。
func TestLegacyTenantRefreshRejected(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	_, rt := seedLegacyTenantSession(t, e)

	resp, err := e.svc.doGrantTypeRefreshToken(context.Background(),
		&authenticationV1.LoginRequest{ClientType: authenticationV1.ClientType_admin.Enum()}, rt)
	require.Error(t, err)
	require.True(t, authenticationV1.IsIncorrectRefreshToken(err), "旧租户续期应按无效 refresh token 拒绝: %v", err)
	require.Nil(t, resp)
}

// 旧租户的 access token 在公共校验（REST 中间件的 token checker、导出路由、SSE 都经过）里被拒。
func TestLegacyTenantAccessTokenRejectedByAuthenticator(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	at, _ := seedLegacyTenantSession(t, e)

	resp, err := e.svc.authenticator.Authenticate(context.Background(), &authenticationV1.ValidateTokenRequest{
		Token: at, ClientType: authenticationV1.ClientType_admin, TokenCategory: authenticationV1.TokenCategory_ACCESS,
	})
	require.Error(t, err)
	require.False(t, resp.GetIsValid())
	require.Nil(t, resp.GetPayload(), "不应把旧租户身份交给调用方")
}

// SSE 握手：旧租户令牌拒绝；租户 0 的令牌正常通过（对照组）。
func TestLegacyTenantSseHandshakeRejected(t *testing.T) {
	e := newAuthenticationServiceForTest(t)
	at, _ := seedLegacyTenantSession(t, e)
	im := &InternalMessageService{
		log:           bLogger.NewHelper(bLogger.NopLogger()),
		authenticator: e.svc.authenticator,
		clientType:    authenticationV1.ClientType_admin,
	}
	uid := strconv.FormatUint(uint64(authSvcTestUserID), 10)

	req := httptest.NewRequest("GET", "/events?stream="+uid, nil)
	require.Error(t, im.HandleAuthorize(req, at), "旧租户令牌不应建立 SSE 连接")

	okAT, _, err := e.svc.authenticator.CreateUserToken(context.Background(), authenticationV1.ClientType_admin,
		&authenticationV1.UserTokenPayload{UserId: 7002, Username: trans.Ptr("authsvc-t0")})
	require.NoError(t, err)
	req = httptest.NewRequest("GET", "/events?stream=7002", nil)
	require.NoError(t, im.HandleAuthorize(req, okAT), "租户 0 的令牌应正常握手")
}
