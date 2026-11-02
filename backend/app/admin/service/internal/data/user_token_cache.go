package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

const (
	// AccessTokenKeyFormat 访问令牌键前缀格式 at:{ct}:{uid}
	AccessTokenKeyFormat = "at:%d:%d"
	// AccessTokenFieldKeyFormat 访问令牌键格式（含 jti）at:{ct}:{uid}:{jti}
	AccessTokenFieldKeyFormat = "at:%d:%d:%s"

	// RefreshTokenKeyFormat 刷新令牌键前缀格式 rt:{ct}:{uid}
	RefreshTokenKeyFormat = "rt:%d:%d"
	// RefreshTokenFieldKeyFormat 刷新令牌键格式（含 jti）rt:{ct}:{uid}:{jti}
	RefreshTokenFieldKeyFormat = "rt:%d:%d:%s"

	// BlacklistKeyFormat 访问令牌黑名单键格式 bl:{jti}
	BlacklistKeyFormat = "bl:%s"

	// UserSessionKeyFormat 用户会话元数据键格式 us:{ct}:{uid}:{jti}
	UserSessionKeyFormat = "us:%d:%d:%s"

	// RefreshTokenGraceKeyFormat 刷新令牌宽限集合键格式 rtg:{ct}:{uid}。
	// 有序集合成员是刚被轮换掉的 RT 原值，score 是该成员自己的过期时间戳（秒）。
	// 并发刷新对撞时，持有上一枚且尚未过期的 RT 凭成员资格兑换新会话。
	// 全量吊销会删掉整个键。
	RefreshTokenGraceKeyFormat = "rtg:%d:%d"

	// TokenLineageKeyFormat 令牌代际链键格式 lin:{ct}:{uid}:{jti}，值=上一代 jti。
	// 轮换不再删除旧 AT（多标签页/多端共享 cookie 的轮换会互杀在用令牌），
	// 踢下线沿此链回溯删除整条会话世系的 AT，保住「踢即全灭」的语义。
	TokenLineageKeyFormat = "lin:%d:%d:%s"

	// RevokeVersionKeyFormat 用户级吊销版本 rv:{ct}:{uid}。全量吊销先 INCR。
	// 刷新在消费旧 RT 的同一个脚本里读出版本，写新令牌时在同一个脚本里比对，
	// 中间被全量吊销穿插就不写，避免在途刷新把已吊销的会话写回来。
	RevokeVersionKeyFormat = "rv:%d:%d"

	// RefreshGenerationKeyFormat RT 写入时的吊销版本 rg:{ct}:{uid}:{jti}，TTL 与 RT 相同。
	// 消费 RT 时要求它等于当前 rv：全量吊销 INCR 的那一刻起，已有 RT 全部不可消费，
	// 不依赖随后逐项删除的进度（删除只是清理）。缺失按 0 处理，兼容升级前写入的 RT。
	RefreshGenerationKeyFormat = "rg:%d:%d:%s"

	// KickedSessionKeyFormat 被踢会话标记 kj:{ct}:{uid}:{jti}，TTL=宽限窗口。
	// 踢单个会话时给它和整条代际世系的祖先都打上标记：
	// 世系里的宽限 RT 不能再兑换，以该会话为父的在途刷新也不能写新令牌。
	KickedSessionKeyFormat = "kj:%d:%d:%s"

	// RefreshTokenGraceWindow 单个旧 RT 的宽限窗口。只覆盖并发刷新对撞和
	// 后台标签页定时器的抖动，60 秒按成员单独计算，后一次轮换不会把更早的成员续期。
	RefreshTokenGraceWindow = 60 * time.Second

	// tokenLineageMaxDepth 踢下线回溯代际链的深度上限（防御异常环）
	tokenLineageMaxDepth = 64

	// scanCount 每次 SCAN 返回的键数量提示（仅是建议值，非强制）
	scanCount = 100
)

// verifyAndRevokeRefreshTokenScript 原子验证并轮换刷新令牌的 Lua 脚本。
// 在单次 Redis 调用中完成「验证 RT → 删除 RT → 删除会话元数据 → 旧 RT 入宽限集合」，
// 避免 TOCTOU 竞态。KEYS: [rtKey, usKey, graceKey, rvKey, rgKey]，ARGV: [refreshToken, graceTTLSeconds]。
// RT 的写入版本（rgKey）不等于当前吊销版本时按无效处理。
// usKey 传空串表示不删除。
// 宽限集合是 ZSET，score 为该成员的过期 Unix 秒。写入时用 Redis TIME 清掉已过期成员，
// 不把整组 TTL 往后推。旧版 SET 在第一次写入时删掉，避免 WRONGTYPE。
// 旧 AT（atKey）不再删除：多标签页/多前端共享同一浏览器 cookie jar，顺序轮换
// 会把上一枚刚发给其他标签页在用的 AT 作废，引发持续互杀的 401 风暴；旧 AT
// 改为自然存活至自身 TTL，踢下线经代际链（lin: 键）追杀。返回值: {ok, 吊销版本}，
// ok 1=验证成功, 0=令牌不存在或值不匹配；吊销版本与消费旧 RT 在同一原子操作里读出。
var verifyAndRevokeRefreshTokenScript = redis.NewScript(`
	local rtKey = KEYS[1]
	local usKey = KEYS[2]
	local graceKey = KEYS[3]
	local rvKey = KEYS[4]
	local rgKey = KEYS[5]
	local refreshToken = ARGV[1]
	local graceTTL = tonumber(ARGV[2])

	local ver = tonumber(redis.call('GET', rvKey) or '0') or 0

	local stored = redis.call('GET', rtKey)
	if not stored or stored ~= refreshToken then
		return {0, ver}
	end
	-- RT 写入后发生过全量吊销（版本已变）：视同已吊销，不消费、不发宽限
	local gen = tonumber(redis.call('GET', rgKey) or '0') or 0
	if gen ~= ver then
		return {0, ver}
	end

	redis.call('DEL', rtKey)
	if usKey and usKey ~= '' then
		redis.call('DEL', usKey)
	end

	local typ = redis.call('TYPE', graceKey)
	if typ and typ.ok == 'set' then
		redis.call('DEL', graceKey)
	end

	local now = tonumber(redis.call('TIME')[1])
	redis.call('ZREMRANGEBYSCORE', graceKey, '-inf', now)
	-- 宽限成员带上版本：全量吊销之后版本变了，旧成员自然对不上
	redis.call('ZADD', graceKey, now + graceTTL, refreshToken .. '|' .. ver)
	redis.call('EXPIRE', graceKey, graceTTL)
	return {1, ver}
`)

// isGraceRefreshTokenScript 只承认还没过期的宽限成员。
// KEYS: [graceKey, kickedKey, rvKey]，ARGV: [refreshToken]。返回 {ok, 吊销版本}，ok 1=未过期成员，0=不是。
// kickedKey 为空串表示不查踢下线标记；该 RT 所在会话已被踢时删掉成员并拒绝。
// 查询时顺手清掉已过期成员。旧版 SET 直接删除并拒绝，避免 WRONGTYPE 被当成命中。
var isGraceRefreshTokenScript = redis.NewScript(`
	local graceKey = KEYS[1]
	local kickedKey = KEYS[2]
	local rvKey = KEYS[3]
	local refreshToken = ARGV[1]

	local ver = tonumber(redis.call('GET', rvKey) or '0') or 0

	local typ = redis.call('TYPE', graceKey)
	if not typ or typ.ok == 'none' then
		return {0, ver}
	end
	if typ.ok == 'set' then
		redis.call('DEL', graceKey)
		return {0, ver}
	end

	if kickedKey and kickedKey ~= '' and redis.call('EXISTS', kickedKey) == 1 then
		redis.call('ZREM', graceKey, refreshToken)
		return {0, ver}
	end

	local now = tonumber(redis.call('TIME')[1])
	redis.call('ZREMRANGEBYSCORE', graceKey, '-inf', now)
	local score = redis.call('ZSCORE', graceKey, refreshToken .. '|' .. ver)
	if not score then
		return {0, ver}
	end
	if tonumber(score) <= now then
		return {0, ver}
	end
	return {1, ver}
`)

// addGuardedTokenPairScript 刷新签发新令牌对：吊销版本没变、父会话没被踢才写入。
// 比对和写入在同一个脚本里，全量吊销 / 踢下线不能从中间穿插。
// KEYS: [atKey, rtKey, rvKey, kickedParentKey, rgKey]，ARGV: [at, rt, atTTLms, rtTTLms, expectedVersion]。
// kickedParentKey 为空串表示没有父会话。返回 1=已写入，0=会话已被吊销。
var addGuardedTokenPairScript = redis.NewScript(`
	local function setx(k, v, ms)
		ms = tonumber(ms)
		if ms and ms > 0 then
			redis.call('SET', k, v, 'PX', ms)
		else
			redis.call('SET', k, v)
		end
	end
	local ver = tonumber(redis.call('GET', KEYS[3]) or '0') or 0
	if ver ~= tonumber(ARGV[5]) then
		return 0
	end
	if KEYS[4] and KEYS[4] ~= '' and redis.call('EXISTS', KEYS[4]) == 1 then
		return 0
	end
	setx(KEYS[1], ARGV[1], ARGV[3])
	setx(KEYS[2], ARGV[2], ARGV[4])
	setx(KEYS[5], ver, ARGV[4])
	return 1
`)

// addTokenPairScript 登录签发令牌对：同时记下写入时的吊销版本，供消费 RT 时比对。
// KEYS: [atKey, rtKey, rvKey, rgKey]，ARGV: [at, rt, atTTLms, rtTTLms]；TTL<=0 表示不过期（与 SET 0 的语义一致）。
var addTokenPairScript = redis.NewScript(`
	local function setx(k, v, ms)
		ms = tonumber(ms)
		if ms and ms > 0 then
			redis.call('SET', k, v, 'PX', ms)
		else
			redis.call('SET', k, v)
		end
	end
	local ver = tonumber(redis.call('GET', KEYS[3]) or '0') or 0
	setx(KEYS[1], ARGV[1], ARGV[3])
	setx(KEYS[2], ARGV[2], ARGV[4])
	setx(KEYS[4], ver, ARGV[4])
	return 1
`)

// ErrSessionRevoked 在途刷新期间会话已被吊销，新令牌没有写入。
var ErrSessionRevoked = errors.New("session revoked during refresh")

// TokenWriteGuard 刷新写新令牌时的条件：吊销版本仍为 RevokeVersion，父会话 ParentJti 没被踢。
type TokenWriteGuard struct {
	RevokeVersion int64
	ParentJti     string
}

// UserTokenCache 用户令牌缓存
type UserTokenCache struct {
	log *bLogger.Helper
	rdb *redis.Client
}

func NewUserTokenCache(ctx *bootstrap.Context, rdb *redis.Client) *UserTokenCache {
	utc := &UserTokenCache{
		rdb: rdb,
		log: ctx.NewLoggerHelper("user-token/cache"),
	}
	return utc
}

// AddTokenPair 添加令牌对
func (r *UserTokenCache) AddTokenPair(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	accessToken string,
	refreshToken string,
	accessTokenExpires time.Duration,
	refreshTokenExpires time.Duration,
) error {
	return addTokenPairScript.Run(ctx, r.rdb,
		[]string{
			r.makeAccessTokenFieldKey(clientType, userId, jti),
			r.makeRefreshTokenFieldKey(clientType, userId, jti),
			r.makeRevokeVersionKey(clientType, userId),
			r.makeRefreshGenerationKey(clientType, userId, jti),
		},
		accessToken, refreshToken,
		accessTokenExpires.Milliseconds(), refreshTokenExpires.Milliseconds(),
	).Err()
}

// AddTokenPairGuarded 按 guard 条件写入令牌对；会话已被吊销时返回 ErrSessionRevoked。
func (r *UserTokenCache) AddTokenPairGuarded(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	accessToken string,
	refreshToken string,
	accessTokenExpires time.Duration,
	refreshTokenExpires time.Duration,
	guard TokenWriteGuard,
) error {
	kickedParentKey := ""
	if guard.ParentJti != "" {
		kickedParentKey = r.makeKickedSessionKey(clientType, userId, guard.ParentJti)
	}
	n, err := addGuardedTokenPairScript.Run(ctx, r.rdb,
		[]string{
			r.makeAccessTokenFieldKey(clientType, userId, jti),
			r.makeRefreshTokenFieldKey(clientType, userId, jti),
			r.makeRevokeVersionKey(clientType, userId),
			kickedParentKey,
			r.makeRefreshGenerationKey(clientType, userId, jti),
		},
		accessToken, refreshToken,
		accessTokenExpires.Milliseconds(), refreshTokenExpires.Milliseconds(),
		guard.RevokeVersion,
	).Int64()
	if err != nil {
		r.log.Errorf(ctx, "add guarded token pair failed for user [%d] jti [%s]: %v", userId, jti, err)
		return err
	}
	if n != 1 {
		return ErrSessionRevoked
	}
	return nil
}

// AddAccessToken 添加访问令牌
func (r *UserTokenCache) AddAccessToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	accessToken string,
	expires time.Duration,
) error {
	key := r.makeAccessTokenFieldKey(clientType, userId, jti)
	return r.set(ctx, key, accessToken, expires)
}

// AddRefreshToken 添加刷新令牌
func (r *UserTokenCache) AddRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	refreshToken string,
	expires time.Duration,
) error {
	key := r.makeRefreshTokenFieldKey(clientType, userId, jti)
	return r.set(ctx, key, refreshToken, expires)
}

// AddBlockedAccessToken 添加被阻止的访问令牌
func (r *UserTokenCache) AddBlockedAccessToken(ctx context.Context, jti string, reason string, expires time.Duration) error {
	key := r.makeBlacklistKey(jti)
	return r.set(ctx, key, reason, expires)
}

// GetAccessTokens 获取访问令牌
func (r *UserTokenCache) GetAccessTokens(ctx context.Context, clientType authenticationV1.ClientType, userId uint32) []string {
	pattern := r.makeAccessTokenKey(clientType, userId) + ":*"
	return r.scanValues(ctx, pattern)
}

// GetRefreshTokens 获取刷新令牌
func (r *UserTokenCache) GetRefreshTokens(ctx context.Context, clientType authenticationV1.ClientType, userId uint32) []string {
	pattern := r.makeRefreshTokenKey(clientType, userId) + ":*"
	return r.scanValues(ctx, pattern)
}

// RevokeToken 移除所有令牌（含会话元数据）
func (r *UserTokenCache) RevokeToken(ctx context.Context, clientType authenticationV1.ClientType, userId uint32) error {
	var err error
	// INCR 吊销版本就是全量吊销的原子生效点：此后已有 RT 的写入版本（rg）、宽限成员的版本
	// 都对不上，消费脚本一律拒绝；在途刷新写新令牌时比对版本也失败。下面的逐项删除只是清理，
	// 和刷新怎么交错都不会让会话复活。INCR 失败时仍尽力删除，但最终必须返回错误。
	incrErr := r.rdb.Incr(ctx, r.makeRevokeVersionKey(clientType, userId)).Err()
	if incrErr != nil {
		r.log.Errorf(ctx, "bump revoke version failed for user [%d]: [%v]", userId, incrErr)
	}

	if err = r.RevokeUserAllAccessToken(ctx, clientType, userId); err != nil {
		r.log.Errorf(ctx, "remove user access token failed: [%v]", err)
	}

	if err = r.RevokeUserAllRefreshToken(ctx, clientType, userId); err != nil {
		r.log.Errorf(ctx, "remove user refresh token failed: [%v]", err)
	}

	// 会话元数据随令牌一并清理，保持「无令牌即无会话记录」的一致性
	if err = r.DeleteUserSessionMetas(ctx, clientType, userId); err != nil {
		r.log.Errorf(ctx, "remove user session metas failed: [%v]", err)
	}

	// 宽限集合不在 rt:{ct}:{uid}:* 的扫描范围内。留下的话，全量吊销后
	// 宽限期内的旧 RT 仍能换出新令牌。
	if err = r.del(ctx, r.makeRefreshTokenGraceKey(clientType, userId)); err != nil {
		r.log.Errorf(ctx, "remove user refresh token grace failed: [%v]", err)
	}

	if incrErr != nil {
		return incrErr
	}
	return err
}

// RevokeAccessToken 移除访问令牌
func (r *UserTokenCache) RevokeAccessToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
) error {
	key := r.makeAccessTokenFieldKey(clientType, userId, jti)
	return r.del(ctx, key)
}

// RevokeRefreshToken 移除刷新令牌
func (r *UserTokenCache) RevokeRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
) error {
	key := r.makeRefreshTokenFieldKey(clientType, userId, jti)
	if err := r.del(ctx, key); err != nil {
		return err
	}
	return r.del(ctx, r.makeRefreshGenerationKey(clientType, userId, jti))
}

// RevokeBlockedAccessToken 撤销被阻止的访问令牌
func (r *UserTokenCache) RevokeBlockedAccessToken(ctx context.Context, jti string) error {
	key := r.makeBlacklistKey(jti)
	return r.del(ctx, key)
}

// IsValidAccessToken 访问令牌是否有效
func (r *UserTokenCache) IsValidAccessToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	uploadedToken string,
) (bool, error) {
	key := r.makeAccessTokenFieldKey(clientType, userId, jti)

	storedToken, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 令牌不存在或已过期
	}

	if storedToken != uploadedToken {
		return false, nil
	}

	return true, nil
}

func (r *UserTokenCache) IsExistAccessTokenByJti(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
) (bool, error) {
	key := r.makeAccessTokenFieldKey(clientType, userId, jti)

	_, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 令牌不存在或已过期
	}

	return true, nil
}

// IsExistAccessToken 访问令牌是否存在
func (r *UserTokenCache) IsExistAccessToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	uploadedToken string,
) (exist bool, jti string, err error) {
	pattern := r.makeAccessTokenKey(clientType, userId) + ":*"
	return r.scanFindValue(ctx, pattern, uploadedToken)
}

// IsExistRefreshToken 刷新令牌是否存在
func (r *UserTokenCache) IsExistRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	uploadedToken string,
) (exist bool, jti string, err error) {
	pattern := r.makeRefreshTokenKey(clientType, userId) + ":*"
	return r.scanFindValue(ctx, pattern, uploadedToken)
}

// IsValidRefreshToken 刷新令牌是否有效
func (r *UserTokenCache) IsValidRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	uploadedToken string,
) (bool, error) {
	key := r.makeRefreshTokenFieldKey(clientType, userId, jti)

	storedToken, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 令牌不存在或已过期
	}

	if storedToken != uploadedToken {
		return false, nil
	}

	return true, nil
}

// IsBlockedAccessToken 访问令牌是否被阻止
func (r *UserTokenCache) IsBlockedAccessToken(ctx context.Context, jti string) bool {
	key := r.makeBlacklistKey(jti)
	return r.exists(ctx, key)
}

// RevokeUserAllAccessToken 删除访问令牌
func (r *UserTokenCache) RevokeUserAllAccessToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
) error {
	pattern := r.makeAccessTokenKey(clientType, userId) + ":*"
	return r.delByPattern(ctx, pattern)
}

// RevokeUserAllRefreshToken 删除刷新令牌
func (r *UserTokenCache) RevokeUserAllRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
) error {
	pattern := r.makeRefreshTokenKey(clientType, userId) + ":*"
	if err := r.delByPattern(ctx, pattern); err != nil {
		return err
	}
	return r.delByPattern(ctx, fmt.Sprintf("rg:%d:%d:*", clientType.Number(), userId))
}

// VerifyAndRevokeTokenPair 原子验证并轮换刷新令牌。使用 Lua 脚本保证
// 「验证→删除→入宽限集合」操作的原子性，避免 TOCTOU 竞态条件。
// 旧 AT 不再随轮换删除（见脚本注释），返回 (true, nil) 表示刷新令牌有效
// 且已完成轮换。
func (r *UserTokenCache) VerifyAndRevokeTokenPair(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	refreshToken string,
) (bool, error) {
	ok, _, err := r.VerifyAndRevokeTokenPairVersioned(ctx, clientType, userId, jti, refreshToken)
	return ok, err
}

// VerifyAndRevokeTokenPairVersioned 同 VerifyAndRevokeTokenPair，另外返回消费旧 RT 时的吊销版本，
// 刷新写新令牌时用它做条件（见 AddTokenPairGuarded）。
func (r *UserTokenCache) VerifyAndRevokeTokenPairVersioned(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	refreshToken string,
) (bool, int64, error) {
	rtKey := r.makeRefreshTokenFieldKey(clientType, userId, jti)
	// 会话元数据随旧令牌对一并原子删除（刷新轮换后旧会话记录即失效）
	usKey := r.makeUserSessionKey(clientType, userId, jti)
	graceKey := r.makeRefreshTokenGraceKey(clientType, userId)

	ok, ver, err := runOkVersionScript(ctx, r.rdb, verifyAndRevokeRefreshTokenScript,
		[]string{rtKey, usKey, graceKey, r.makeRevokeVersionKey(clientType, userId), r.makeRefreshGenerationKey(clientType, userId, jti)},
		refreshToken, int(RefreshTokenGraceWindow.Seconds()))
	if err != nil {
		r.log.Errorf(ctx, "verifyAndRevokeTokenPair failed for user [%d] jti [%s]: %v", userId, jti, err)
		return false, 0, err
	}

	return ok, ver, nil
}

// runOkVersionScript 执行返回 {ok, version} 的脚本。
func runOkVersionScript(ctx context.Context, rdb *redis.Client, script *redis.Script, keys []string, args ...interface{}) (bool, int64, error) {
	vals, err := script.Run(ctx, rdb, keys, args...).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	if len(vals) != 2 {
		return false, 0, fmt.Errorf("unexpected script result length %d", len(vals))
	}
	return vals[0] == 1, vals[1], nil
}

// IsGraceRefreshToken 刷新令牌是否处于宽限窗口内（刚被他人并发轮换掉的 RT）。
// 命中者可兑换一个独立新会话而不是被判死强制登出。
func (r *UserTokenCache) IsGraceRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	refreshToken string,
) bool {
	ok, _ := r.CheckGraceRefreshToken(ctx, clientType, userId, "", refreshToken)
	return ok
}

// CheckGraceRefreshToken 宽限判断，另外返回吊销版本。jti 是该 RT 自己的 jti：
// 它所在的会话世系被踢过时不再接受（jti 为空则不查踢下线标记）。
func (r *UserTokenCache) CheckGraceRefreshToken(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	refreshToken string,
) (bool, int64) {
	kickedKey := ""
	if jti != "" {
		kickedKey = r.makeKickedSessionKey(clientType, userId, jti)
	}
	ok, ver, err := runOkVersionScript(ctx, r.rdb, isGraceRefreshTokenScript,
		[]string{r.makeRefreshTokenGraceKey(clientType, userId), kickedKey, r.makeRevokeVersionKey(clientType, userId)},
		refreshToken)
	if err != nil {
		r.log.Errorf(ctx, "check refresh token grace failed for user [%d]: %v", userId, err)
		return false, 0
	}
	return ok, ver
}

// AddTokenLineage 记录令牌代际：newJti 由 prevJti 轮换而来。TTL 与 AT 生命周期
// 对齐——链节点只需在「持有该代 AT 期间」可被踢下线追到。
func (r *UserTokenCache) AddTokenLineage(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	newJti string,
	prevJti string,
	ttl time.Duration,
) error {
	if newJti == "" || prevJti == "" || prevJti == newJti {
		return nil
	}
	key := r.makeTokenLineageKey(clientType, userId, newJti)
	return r.set(ctx, key, prevJti, ttl)
}

// RevokeTokenByJti 吊销指定会话（含其整条代际世系的 AT）。
// 轮换不再删除旧 AT 后，仅删当前 jti 会漏掉同世系更早各代仍在 TTL 内的 AT；
// 沿 lin: 链回溯逐代删除，恢复「踢即全灭」语义。环/断链由深度上限兜底。
func (r *UserTokenCache) RevokeTokenByJti(ctx context.Context, clientType authenticationV1.ClientType, userId uint32, jti string) error {
	var err error
	// 先给本会话打踢下线标记：宽限 RT 和以它为父的在途刷新都会被拒。祖先在下面回溯时逐个标记。
	if err = r.markKicked(ctx, clientType, userId, jti); err != nil {
		r.log.Errorf(ctx, "mark kicked session failed: [%v]", err)
	}

	if err = r.RevokeAccessToken(ctx, clientType, userId, jti); err != nil {
		r.log.Errorf(ctx, "remove user access token failed: [%v]", err)
	}

	if err = r.RevokeRefreshToken(ctx, clientType, userId, jti); err != nil {
		r.log.Errorf(ctx, "remove user refresh token failed: [%v]", err)
	}

	if err = r.DeleteSessionMeta(ctx, clientType, userId, jti); err != nil {
		r.log.Errorf(ctx, "remove user session meta failed: [%v]", err)
	}

	// 回溯代际链，删除祖先各代残留的 AT 与链节点
	prev := jti
	for i := 0; i < tokenLineageMaxDepth; i++ {
		lineageKey := r.makeTokenLineageKey(clientType, userId, prev)
		parent, err := r.rdb.GetDel(ctx, lineageKey).Result()
		if err != nil || parent == "" {
			break
		}
		// 祖先代的 RT 可能还在宽限集合里，打上标记后不能再兑换
		if err = r.markKicked(ctx, clientType, userId, parent); err != nil {
			r.log.Errorf(ctx, "mark kicked lineage session failed: [%v]", err)
		}
		if err = r.RevokeAccessToken(ctx, clientType, userId, parent); err != nil {
			r.log.Errorf(ctx, "remove lineage access token failed: [%v]", err)
		}
		prev = parent
	}

	return err
}

// ==============================
// key 生成
// ==============================

// makeAccessTokenKey 生成访问令牌键前缀 at:{ct}:{uid}（用于 SCAN 匹配）
func (r *UserTokenCache) makeAccessTokenKey(clientType authenticationV1.ClientType, userId uint32) string {
	return fmt.Sprintf(AccessTokenKeyFormat, clientType.Number(), userId)
}

// makeAccessTokenFieldKey 生成访问令牌键（含 jti）at:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeAccessTokenFieldKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(AccessTokenFieldKeyFormat, clientType.Number(), userId, jti)
}

// makeRefreshTokenKey 生成刷新令牌键前缀 rt:{ct}:{uid}（用于 SCAN 匹配）
func (r *UserTokenCache) makeRefreshTokenKey(clientType authenticationV1.ClientType, userId uint32) string {
	return fmt.Sprintf(RefreshTokenKeyFormat, clientType.Number(), userId)
}

// makeRefreshTokenFieldKey 生成刷新令牌键（含 jti）rt:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeRefreshTokenFieldKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(RefreshTokenFieldKeyFormat, clientType.Number(), userId, jti)
}

// makeRefreshTokenGraceKey 生成刷新令牌宽限集合键 rtg:{ct}:{uid}
func (r *UserTokenCache) makeRefreshTokenGraceKey(clientType authenticationV1.ClientType, userId uint32) string {
	return fmt.Sprintf(RefreshTokenGraceKeyFormat, clientType.Number(), userId)
}

// makeTokenLineageKey 生成令牌代际链键 lin:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeTokenLineageKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(TokenLineageKeyFormat, clientType.Number(), userId, jti)
}

// markKicked 标记会话已被踢。宽限 RT 和在途刷新最多存活一个宽限窗口，标记同样长即可。
func (r *UserTokenCache) markKicked(ctx context.Context, clientType authenticationV1.ClientType, userId uint32, jti string) error {
	if jti == "" {
		return nil
	}
	return r.set(ctx, r.makeKickedSessionKey(clientType, userId, jti), "1", RefreshTokenGraceWindow)
}

// makeRevokeVersionKey 生成用户级吊销版本键 rv:{ct}:{uid}
func (r *UserTokenCache) makeRevokeVersionKey(clientType authenticationV1.ClientType, userId uint32) string {
	return fmt.Sprintf(RevokeVersionKeyFormat, clientType.Number(), userId)
}

// makeRefreshGenerationKey 生成 RT 写入版本键 rg:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeRefreshGenerationKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(RefreshGenerationKeyFormat, clientType.Number(), userId, jti)
}

// makeKickedSessionKey 生成被踢会话标记键 kj:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeKickedSessionKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(KickedSessionKeyFormat, clientType.Number(), userId, jti)
}

// makeBlacklistKey 生成黑名单键
func (r *UserTokenCache) makeBlacklistKey(jti string) string {
	return fmt.Sprintf(BlacklistKeyFormat, jti)
}

// ==============================
// 基础 String 操作（黑名单等使用）
// ==============================

func (r *UserTokenCache) set(ctx context.Context, key string, value string, expires time.Duration) error {
	if err := r.rdb.Set(ctx, key, value, expires).Err(); err != nil {
		r.log.Errorf(ctx, "set key[%s] value[%s] failed: %v", key, value, err)
		return err
	}
	return nil
}

// del 删除键
func (r *UserTokenCache) del(ctx context.Context, key string) error {
	if err := r.rdb.Del(ctx, key).Err(); err != nil {
		r.log.Errorf(ctx, "del key[%s] failed: %v", key, err)
		return err
	}
	return nil
}

func (r *UserTokenCache) exists(ctx context.Context, key string) bool {
	n, err := r.rdb.Exists(ctx, key).Result()
	if err != nil {
		r.log.Errorf(ctx, "exists key[%s] failed: %v", key, err)
		return false
	}
	return n > 0
}

// ==============================
// 基于 SCAN 的批量操作（替代原 Hash 操作）
// ==============================

// scanKeys 使用 SCAN 遍历匹配 pattern 的所有 key。
// 正确处理游标迭代，直到游标归零为止，避免遗漏。
func (r *UserTokenCache) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	var err error

	for {
		var batch []string
		batch, cursor, err = r.rdb.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			r.log.Errorf(ctx, "scan pattern[%s] failed: %v", pattern, err)
			return nil, err
		}

		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

// scanValues 获取匹配 pattern 的所有 key 的值。
func (r *UserTokenCache) scanValues(ctx context.Context, pattern string) []string {
	keys, err := r.scanKeys(ctx, pattern)
	if err != nil || len(keys) == 0 {
		return []string{}
	}

	values, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		r.log.Errorf(ctx, "mget pattern[%s] failed: %v", pattern, err)
		return []string{}
	}

	var result []string
	for _, v := range values {
		if v == nil {
			continue // key 已过期
		}
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}

	return result
}

// scanFindValue 在匹配 pattern 的所有 key 中查找值等于 target 的 key，并返回其 jti。
// 用于「按 token 值反查 jti」的场景（如黑名单操作）。
func (r *UserTokenCache) scanFindValue(ctx context.Context, pattern string, target string) (bool, string, error) {
	keys, err := r.scanKeys(ctx, pattern)
	if err != nil {
		return false, "", err
	}
	if len(keys) == 0 {
		return false, "", nil
	}

	values, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		r.log.Errorf(ctx, "mget pattern[%s] failed: %v", pattern, err)
		return false, "", err
	}

	for i, v := range values {
		if s, ok := v.(string); ok && s == target {
			return true, r.extractJtiFromKey(keys[i]), nil
		}
	}

	return false, "", nil
}

// delByPattern 删除匹配 pattern 的所有 key。
// 使用 SCAN + pipeline DEL，避免阻塞 Redis（不使用 KEYS）。
func (r *UserTokenCache) delByPattern(ctx context.Context, pattern string) error {
	keys, err := r.scanKeys(ctx, pattern)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}

	if err := r.rdb.Del(ctx, keys...).Err(); err != nil {
		r.log.Errorf(ctx, "del by pattern[%s] failed: %v", pattern, err)
		return err
	}

	return nil
}

// extractJtiFromKey 从键名 at:{ct}:{uid}:{jti} 中提取末段的 jti。
func (r *UserTokenCache) extractJtiFromKey(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// ==============================
// 用户会话元数据（在线用户功能）
// ==============================

// SessionMeta 会话元数据：令牌签发时记录的客户端信息，
// 供在线会话列表展示。生命周期与 refresh token 对齐（TTL 同步设置），
// 过期或吊销即从 Redis 消失，无需额外清理。
type SessionMeta struct {
	Username  string `json:"username,omitempty"`
	TenantId  uint32 `json:"tenantId,omitempty"`
	Ip        string `json:"ip,omitempty"`
	UserAgent string `json:"ua,omitempty"`
	DeviceId  string `json:"dev,omitempty"`
	// LoginAt 登录时间（unix 秒）。刷新轮换继承首次登录时间，不重置。
	LoginAt int64 `json:"loginAt"`
}

// SessionEntry 一次会话扫描得到的完整条目（键定位信息 + 元数据）。
type SessionEntry struct {
	ClientType authenticationV1.ClientType
	UserId     uint32
	Jti        string
	Meta       *SessionMeta
}

// SaveSessionMeta 记录会话元数据，TTL 与 refresh token 一致。
func (r *UserTokenCache) SaveSessionMeta(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
	meta *SessionMeta,
	expires time.Duration,
) error {
	if jti == "" || meta == nil {
		return nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		r.log.Errorf(ctx, "marshal session meta failed for user [%d]: %v", userId, err)
		return err
	}
	key := r.makeUserSessionKey(clientType, userId, jti)
	return r.set(ctx, key, string(raw), expires)
}

// GetSessionMeta 读取单个会话元数据；会话不存在（已过期/被吊销）返回 nil。
func (r *UserTokenCache) GetSessionMeta(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
) (*SessionMeta, error) {
	key := r.makeUserSessionKey(clientType, userId, jti)
	raw, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		r.log.Errorf(ctx, "get session meta failed for user [%d]: %v", userId, err)
		return nil, err
	}
	return decodeSessionMeta(raw)
}

// DeleteSessionMeta 删除会话元数据（令牌吊销/轮换后清理）。
func (r *UserTokenCache) DeleteSessionMeta(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
	jti string,
) error {
	if jti == "" {
		return nil
	}
	key := r.makeUserSessionKey(clientType, userId, jti)
	return r.del(ctx, key)
}

// DeleteUserSessionMetas 删除某用户全部会话元数据（全端踢下线后清理）。
func (r *UserTokenCache) DeleteUserSessionMetas(
	ctx context.Context,
	clientType authenticationV1.ClientType,
	userId uint32,
) error {
	pattern := fmt.Sprintf(UserSessionKeyFormat, clientType.Number(), userId, "*")
	return r.delByPattern(ctx, pattern)
}

// ListSessionEntries 扫描全部会话元数据。
// 按 us:{ct}:{uid}:{jti} 键名拆出定位信息，MGET 取值，
// 已过期键在 MGET 中返回 nil 自动跳过，无需额外 TTL 判断。
func (r *UserTokenCache) ListSessionEntries(ctx context.Context) ([]SessionEntry, error) {
	keys, err := r.scanKeys(ctx, "us:*")
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []SessionEntry{}, nil
	}

	values, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		r.log.Errorf(ctx, "mget session metas failed: %v", err)
		return nil, err
	}

	entries := make([]SessionEntry, 0, len(keys))
	for i, v := range values {
		s, ok := v.(string)
		if !ok {
			continue // key 已过期
		}
		meta, err := decodeSessionMeta(s)
		if err != nil || meta == nil {
			r.log.Warnf(ctx, "decode session meta failed for key [%s]: %v", keys[i], err)
			continue
		}
		entry, ok := parseUserSessionKey(keys[i])
		if !ok {
			continue
		}
		entry.Meta = meta
		entries = append(entries, *entry)
	}

	return entries, nil
}

// makeUserSessionKey 生成会话元数据键 us:{ct}:{uid}:{jti}
func (r *UserTokenCache) makeUserSessionKey(clientType authenticationV1.ClientType, userId uint32, jti string) string {
	return fmt.Sprintf(UserSessionKeyFormat, clientType.Number(), userId, jti)
}

// parseUserSessionKey 从键名 us:{ct}:{uid}:{jti} 拆出定位信息。
func parseUserSessionKey(key string) (*SessionEntry, bool) {
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 || parts[3] == "" {
		return nil, false
	}
	ct, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, false
	}
	uid, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, false
	}
	return &SessionEntry{
		ClientType: authenticationV1.ClientType(ct),
		UserId:     uint32(uid),
		Jti:        parts[3],
	}, true
}

func decodeSessionMeta(raw string) (*SessionMeta, error) {
	var meta SessionMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
