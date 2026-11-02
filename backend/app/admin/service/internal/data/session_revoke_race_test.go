package data

import (
	"context"
	"net"
	"sync"
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

// 全量吊销会触发回调（SSE 服务器据此断开该用户的长连接）；单会话踢下线不触发。
func TestAuthenticator_RevokeAllFiresHooks(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	var fired []uint32
	a.OnUserTokensRevoked(func(_ context.Context, uid uint32) { fired = append(fired, uid) })

	_, _, jti := raceLogin(t, a)
	ct := authenticationV1.ClientType_admin
	require.NoError(t, a.RevokeTokenByJti(ctx, &ct, raceTestUID, jti))
	require.Empty(t, fired)

	require.NoError(t, a.RevokeUserToken(ctx, ct, raceTestUID))
	require.Equal(t, []uint32{raceTestUID}, fired)

	fired = nil
	require.NoError(t, a.RevokeUserTokenAllClientTypes(ctx, raceTestUID))
	require.NotEmpty(t, fired)
	for _, uid := range fired {
		require.Equal(t, raceTestUID, uid)
	}
}

// pauseAfterIncr 在 INCR 成功返回后、RevokeToken 后续清理执行前暂停（review-a2 R2 的交错）。
type pauseAfterIncr struct {
	once           sync.Once
	bumped, resume chan struct{}
}

func (h *pauseAfterIncr) DialHook(next redis.DialHook) redis.DialHook {
	return func(c context.Context, n, a string) (net.Conn, error) { return next(c, n, a) }
}
func (h *pauseAfterIncr) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *pauseAfterIncr) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "incr" {
			h.once.Do(func() { close(h.bumped); <-h.resume })
		}
		return err
	}
}

// review-a2 R2：吊销已 INCR、旧 RT 还没删时发起刷新。修复前刷新读到新版本、
// 吊销完成后写回有效令牌；现在 INCR 那一刻旧 RT 就不可消费。
func TestRefresh_RefreshAfterVersionBumpCannotSurviveRevoke(t *testing.T) {
	a, c := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt, _ := raceLogin(t, a)

	hook := &pauseAfterIncr{bumped: make(chan struct{}), resume: make(chan struct{})}
	c.rdb.AddHook(hook)
	done := make(chan error, 1)
	go func() { done <- a.RevokeUserToken(ctx, ct, raceTestUID) }()
	<-hook.bumped

	_, _, _, _, err := a.VerifyRefreshToken(ctx, ct, rt)
	close(hook.resume)
	require.NoError(t, <-done)
	require.Error(t, err, "版本已抬升后，旧 RT 不能再被消费")
	require.True(t, authenticationV1.IsIncorrectRefreshToken(err), "got %v", err)
	require.Empty(t, a.GetAccessTokens(ctx, ct, raceTestUID))
}

// 同一窗口里用宽限 RT 兑换也不行：宽限成员带写入时的版本。
func TestRefresh_GraceAfterVersionBumpCannotSurviveRevoke(t *testing.T) {
	a, c := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	_, rt0, _ := raceLogin(t, a)

	uid, _, _, guard, err := a.VerifyRefreshToken(ctx, ct, rt0) // RT0 进入宽限
	require.NoError(t, err)
	_, _, err = a.CreateUserTokenGuarded(ctx, ct, &authenticationV1.UserTokenPayload{UserId: uid}, guard)
	require.NoError(t, err)

	hook := &pauseAfterIncr{bumped: make(chan struct{}), resume: make(chan struct{})}
	c.rdb.AddHook(hook)
	done := make(chan error, 1)
	go func() { done <- a.RevokeUserToken(ctx, ct, raceTestUID) }()
	<-hook.bumped

	_, _, _, _, err = a.VerifyRefreshToken(ctx, ct, rt0)
	close(hook.resume)
	require.NoError(t, <-done)
	require.Error(t, err, "版本已抬升后，宽限 RT 不能再兑换")
	require.Empty(t, a.GetAccessTokens(ctx, ct, raceTestUID))
}

// 吊销之后的新登录照常可刷新（新 RT 的写入版本就是新版本）。
func TestRefresh_LoginAfterRevokeStillRefreshes(t *testing.T) {
	a, _ := newRaceAuthenticator(t)
	ctx := context.Background()
	ct := authenticationV1.ClientType_admin
	raceLogin(t, a)
	require.NoError(t, a.RevokeUserToken(ctx, ct, raceTestUID))

	_, rt, _ := raceLogin(t, a)
	uid, _, _, guard, err := a.VerifyRefreshToken(ctx, ct, rt)
	require.NoError(t, err)
	at2, _, err := a.CreateUserTokenGuarded(ctx, ct, &authenticationV1.UserTokenPayload{UserId: uid}, guard)
	require.NoError(t, err)
	require.True(t, accessValid(t, a, at2))
}
