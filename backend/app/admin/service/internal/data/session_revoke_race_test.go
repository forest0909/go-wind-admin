package data

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

const raceTestUID = uint32(77)

func newRaceAuthenticator(t *testing.T) (*Authenticator, *UserTokenCache) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewUserTokenCacheForTest(rdb)
	a, err := NewAuthenticatorForTest(&conf.Authentication_Jwt{Method: "HS256", Key: "race-test-hs256-secret-key-0123456789abcdef"}, cache)
	require.NoError(t, err)
	return a, cache
}

func raceLogin(t *testing.T, a *Authenticator) (at, rt, jti string) {
	t.Helper()
	p := &authenticationV1.UserTokenPayload{UserId: raceTestUID, Username: trans.Ptr("race")}
	at, rt, err := a.CreateUserToken(context.Background(), authenticationV1.ClientType_admin, p)
	require.NoError(t, err)
	return at, rt, p.GetJti()
}

func accessValid(t *testing.T, a *Authenticator, at string) bool {
	t.Helper()
	resp, err := a.Authenticate(context.Background(), &authenticationV1.ValidateTokenRequest{
		Token: at, ClientType: authenticationV1.ClientType_admin, TokenCategory: authenticationV1.TokenCategory_ACCESS,
	})
	return err == nil && resp.GetIsValid()
}

// a2 #9：刷新验证完旧 RT 之后发生全量吊销（登出/改密/停用），在途刷新不能写回有效令牌。
func TestRefresh_RevokeAllDuringRefreshDiscardsNewToken(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt, _ := raceLogin(t, a)

	uid, _, grace, guard, err := a.VerifyRefreshToken(ctx, ct, rt)
	require.NoError(t, err)
	require.False(t, grace)

	require.NoError(t, a.RevokeUserToken(ctx, ct, uid)) // 穿插在验证和写入之间

	p := &authenticationV1.UserTokenPayload{UserId: uid, Username: trans.Ptr("race")}
	at2, _, err := a.CreateUserTokenGuarded(ctx, ct, p, guard)
	require.Error(t, err)
	require.True(t, authenticationV1.IsIncorrectRefreshToken(err))
	require.Empty(t, at2)
	require.Empty(t, a.GetAccessTokens(ctx, ct, uid), "吊销后不应留下任何访问令牌")

	// 吊销之后正常登录不受影响
	at3, _, _ := raceLogin(t, a)
	require.True(t, accessValid(t, a, at3))
}

// 单会话踢下线穿插在刷新验证与写入之间，同样不能写回。
func TestRefresh_KickDuringRefreshDiscardsNewToken(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt, jti := raceLogin(t, a)

	uid, oldJti, _, guard, err := a.VerifyRefreshToken(ctx, ct, rt)
	require.NoError(t, err)
	require.Equal(t, jti, oldJti)

	require.NoError(t, a.RevokeTokenByJti(ctx, &ct, uid, oldJti))

	_, _, err = a.CreateUserTokenGuarded(ctx, ct, &authenticationV1.UserTokenPayload{UserId: uid}, guard)
	require.True(t, authenticationV1.IsIncorrectRefreshToken(err), "got %v", err)
	require.Empty(t, a.GetAccessTokens(ctx, ct, uid))
}

// 无穿插时，带 guard 的刷新照常写入。
func TestRefresh_GuardedWriteSucceedsWithoutRevoke(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt, _ := raceLogin(t, a)

	uid, _, _, guard, err := a.VerifyRefreshToken(ctx, ct, rt)
	require.NoError(t, err)
	at2, rt2, err := a.CreateUserTokenGuarded(ctx, ct, &authenticationV1.UserTokenPayload{UserId: uid}, guard)
	require.NoError(t, err)
	require.True(t, accessValid(t, a, at2))
	require.NotEmpty(t, rt2)
}

// a2 #4：RT0 轮换出 RT1 后，踢掉 RT1 的会话；持 RT0 的客户端不能再在宽限期内兑换。
func TestRefresh_KickRevokesLineageGraceRefreshTokens(t *testing.T) {
	a, cache := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt0, _ := raceLogin(t, a)

	uid, jti0, _, guard, err := a.VerifyRefreshToken(ctx, ct, rt0)
	require.NoError(t, err)
	p1 := &authenticationV1.UserTokenPayload{UserId: uid}
	_, _, err = a.CreateUserTokenGuarded(ctx, ct, p1, guard)
	require.NoError(t, err)
	require.NoError(t, a.AddTokenLineage(ctx, ct, uid, p1.GetJti(), jti0))

	// 踢之前：RT0 在宽限内可兑换（并发刷新对撞的正常语义）
	ok, _ := cache.CheckGraceRefreshToken(ctx, ct, uid, jti0, rt0)
	require.True(t, ok)

	require.NoError(t, a.RevokeTokenByJti(ctx, &ct, uid, p1.GetJti()))

	_, _, _, _, err = a.VerifyRefreshToken(ctx, ct, rt0)
	require.Error(t, err, "被踢会话世系里的宽限 RT 不能再兑换")
	require.True(t, authenticationV1.IsIncorrectRefreshToken(err))
}

// 同一用户的其他会话不受单会话踢下线影响。
func TestRefresh_KickDoesNotAffectOtherSessions(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, _, jtiA := raceLogin(t, a)
	_, rtB, _ := raceLogin(t, a)

	require.NoError(t, a.RevokeTokenByJti(ctx, &ct, raceTestUID, jtiA))

	uid, _, _, guard, err := a.VerifyRefreshToken(ctx, ct, rtB)
	require.NoError(t, err)
	_, _, err = a.CreateUserTokenGuarded(ctx, ct, &authenticationV1.UserTokenPayload{UserId: uid}, guard)
	require.NoError(t, err)
}
