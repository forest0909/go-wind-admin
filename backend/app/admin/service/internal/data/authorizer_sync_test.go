package data

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/authorizer"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// sharedPolicyProvider 模拟共享数据库：两个实例读同一份角色→API 授权。
type sharedPolicyProvider struct {
	mu    sync.Mutex
	allow bool
}

func (p *sharedPolicyProvider) set(allow bool)                               { p.mu.Lock(); p.allow = allow; p.mu.Unlock() }
func (p *sharedPolicyProvider) ProvideModels(string) authorizer.ModelDataMap { return nil }
func (p *sharedPolicyProvider) ProvidePolicies(context.Context) (authorizer.PermissionDataMap, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.allow {
		return authorizer.PermissionDataMap{}, nil
	}
	return authorizer.PermissionDataMap{"sync-role": {{Domain: "0", Path: "/admin/v1/users", Method: "GET"}}}, nil
}

type policyInstance struct {
	authz *authorizer.Authorizer
	rdb   *redis.Client
	sync  *PolicySync
}

func newPolicyInstance(t *testing.T, ctx context.Context, addr string, p authorizer.Provider) *policyInstance {
	t.Helper()
	authz := authorizer.NewAuthorizer(ctx, bLogger.NopLogger(), &authorizer.EngineConfig{Type: "casbin"}, p)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return &policyInstance{authz: authz, rdb: rdb, sync: newPolicySync(rdb, authz, bLogger.NewHelper(bLogger.NopLogger()))}
}

func (i *policyInstance) allowed(t *testing.T) bool {
	t.Helper()
	ok, err := i.authz.Engine().IsAuthorized(context.Background(), "sync-role", "GET", "/admin/v1/users", "0")
	require.NoError(t, err)
	return ok
}

func eventually(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 10*time.Millisecond, msg)
}

// 实例 A 撤销授权后，实例 B 必须跟着重载。修复前 B 一直沿用旧策略放行（a2 #5）。
func TestPolicySync_ResetOnOneInstanceReloadsOthers(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &sharedPolicyProvider{allow: true}

	a := newPolicyInstance(t, ctx, mr.Addr(), p)
	b := newPolicyInstance(t, ctx, mr.Addr(), p)
	go a.sync.Run(ctx)
	go b.sync.Run(ctx)
	eventually(t, "both subscribed", func() bool { return mr.PubSubNumSub(policyChangedChannel)[policyChangedChannel] == 2 })

	require.NoError(t, a.authz.ResetPolicies(ctx))
	eventually(t, "B loads initial policies", func() bool { return b.allowed(t) })
	require.True(t, a.allowed(t))

	p.set(false)
	require.NoError(t, a.authz.ResetPolicies(ctx))
	require.False(t, a.allowed(t))
	eventually(t, "B must drop the revoked grant", func() bool { return !b.allowed(t) })
}

// 广播丢失（B 的订阅断开期间 A 改了策略）后，B 重新订阅时按版本号对账补上。
func TestPolicySync_ReconcileOnResubscribe(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	p := &sharedPolicyProvider{allow: true}

	a := newPolicyInstance(t, ctx, mr.Addr(), p)
	b := newPolicyInstance(t, ctx, mr.Addr(), p)
	require.NoError(t, a.authz.ResetPolicies(ctx))
	require.NoError(t, b.authz.ResetPolicies(ctx))
	require.True(t, b.allowed(t))

	// B 此时没有订阅：A 的广播 B 收不到
	p.set(false)
	require.NoError(t, a.authz.ResetPolicies(ctx))
	require.True(t, b.allowed(t), "前提：广播丢失时 B 仍是旧策略")

	// B 订阅建立 → OnSubscribed 对账 → 发现版本号变了 → 重载
	bctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go b.sync.Run(bctx)
	eventually(t, "B reconciles on (re)subscribe", func() bool { return !b.allowed(t) })
}

// 发起变更的实例收到自己的广播时不应重复装载。
func TestPolicySync_OwnBroadcastDoesNotReload(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	counting := &countingProvider{inner: &sharedPolicyProvider{allow: true}}
	a := newPolicyInstance(t, ctx, mr.Addr(), counting)

	require.NoError(t, a.authz.ResetPolicies(ctx))
	a.sync.sync(ctx, false) // 等价于收到自己的广播
	require.Equal(t, 1, counting.calls())
}

type countingProvider struct {
	inner authorizer.Provider
	mu    sync.Mutex
	n     int
}

func (c *countingProvider) ProvideModels(s string) authorizer.ModelDataMap { return nil }
func (c *countingProvider) ProvidePolicies(ctx context.Context) (authorizer.PermissionDataMap, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.ProvidePolicies(ctx)
}
func (c *countingProvider) calls() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

// failIncrHook 让 INCR 失败一次，模拟 Redis 短暂故障或请求 ctx 被取消。
type failIncrHook struct{ fail atomic.Bool }

func (h *failIncrHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *failIncrHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "incr" && h.fail.CompareAndSwap(true, false) {
			cmd.SetErr(errors.New("injected INCR failure"))
			return cmd.Err()
		}
		return next(ctx, cmd)
	}
}
func (h *failIncrHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// review-a2-round2 R2 触发 A：A 撤权时 INCR 失败，版本号没变。修复前 B 重连后看到版本相等就跳过，
// 一直沿用旧授权；现在重连无条件重载。
func TestPolicySync_FailedVersionBumpConvergesOnResubscribe(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	p := &sharedPolicyProvider{allow: true}

	a := newPolicyInstance(t, ctx, mr.Addr(), p)
	b := newPolicyInstance(t, ctx, mr.Addr(), p)
	hook := &failIncrHook{}
	a.rdb.AddHook(hook)

	require.NoError(t, a.authz.ResetPolicies(ctx))
	b.sync.sync(ctx, false)
	require.True(t, b.allowed(t))

	p.set(false)
	hook.fail.Store(true)
	require.NoError(t, a.authz.ResetPolicies(ctx), "INCR 失败时本实例照常生效")
	require.False(t, a.allowed(t))
	require.True(t, b.allowed(t), "前提：版本号没变，B 还是旧策略")

	bctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go b.sync.Run(bctx)
	eventually(t, "B reloads unconditionally on (re)subscribe", func() bool { return !b.allowed(t) })
}

// R2 触发 B：Redis 从旧快照恢复，版本号撞回 B 记录的值。订阅一直健康也要在周期全量重载里收敛。
func TestPolicySync_PeriodicResyncCatchesVersionCollision(t *testing.T) {
	old := policyResyncInterval
	policyResyncInterval = 50 * time.Millisecond
	t.Cleanup(func() { policyResyncInterval = old })

	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &sharedPolicyProvider{allow: true}

	a := newPolicyInstance(t, ctx, mr.Addr(), p)
	b := newPolicyInstance(t, ctx, mr.Addr(), p)
	require.NoError(t, a.authz.ResetPolicies(ctx))
	b.sync.sync(ctx, false)
	require.True(t, b.allowed(t))

	// 库里撤权，但版本号保持 B 记录的值（等价于"回退后又 INCR 撞回同一个值"），且没有广播
	p.set(false)
	require.True(t, b.allowed(t))

	go b.sync.Run(ctx)
	eventually(t, "B converges via periodic full reload", func() bool { return !b.allowed(t) })
}
