package data

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/redis/go-redis/v9"

	"go-wind-admin/third_party/go-utils/authorizer"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/pkg/redissync"
	"go-wind-admin/pkg/serviceid"
	"go-wind-admin/third_party/go-crud/viewer"
)

// 多实例 Casbin 策略同步。
//
// 每个实例的 Casbin 策略都在自己的内存里。任一实例 ResetPolicies 时：先 INCR 全局版本号，
// 再装载本实例策略，然后在 policyChangedChannel 上广播。各实例收到广播、或订阅（重新）建立时，
// 读全局版本号，和本实例上次装载时记下的版本不一致就重载。
//
// 先 INCR 再装载：一个变更总是先提交到库、再 INCR。所以如果本实例记下的版本 v 是在装载开始前读到的，
// 那么版本号 ≤ v 的变更都已包含在这次装载里；更大的版本必然触发再次重载。广播丢失时，
// 订阅重连后的对账会补上。
var (
	policyVersionKey     = serviceid.ProjectName + ":authz:policy_version"
	policyChangedChannel = serviceid.ProjectName + ":authz:policy_changed"
)

// PolicySync 见文件头注释。
type PolicySync struct {
	rdb   redis.UniversalClient
	authz *authorizer.Authorizer
	log   *bLogger.Helper

	// mu 让"读版本号 → 装载 → 记录版本"整体串行，记录的版本才对应得上装载的内容。
	mu     sync.Mutex
	loaded int64
}

// NewPolicySync 给 authz 装上跨实例同步并开始订阅。rdb 为 nil 时不同步（单实例语义）。
// 必须在第一次 ResetPolicies 之前调用。
func NewPolicySync(ctx *bootstrap.Context, rdb *redis.Client, authz *authorizer.Authorizer) *PolicySync {
	if rdb == nil {
		return nil
	}
	p := newPolicySync(rdb, authz, ctx.NewLoggerHelper("authz-sync/data/admin-service"))
	go p.Run(ctx.Context())
	return p
}

func newPolicySync(rdb redis.UniversalClient, authz *authorizer.Authorizer, log *bLogger.Helper) *PolicySync {
	p := &PolicySync{rdb: rdb, authz: authz, log: log}
	authz.SetResetWrapper(p.reset)
	return p
}

// Run 阻塞订阅变更广播，直到 ctx 结束。
func (p *PolicySync) Run(ctx context.Context) {
	redissync.Subscribe(ctx, p.rdb, policyChangedChannel, redissync.Handlers{
		OnSubscribed: func(ctx context.Context) { p.reconcile(ctx) },
		OnMessage:    func(ctx context.Context, _ string) { p.reconcile(ctx) },
	}, p.log)
}

// reset 是 ResetPolicies 的包装：INCR → 本实例装载 → 广播。
func (p *PolicySync) reset(ctx context.Context, load func(context.Context) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	v, err := p.rdb.Incr(ctx, policyVersionKey).Result()
	if err != nil {
		// Redis 不可用：本实例照常生效，其他实例要等 Redis 恢复后的下一次变更或对账
		p.log.Errorf(ctx, "bump policy version failed, other instances will not reload: %v", err)
		return load(ctx)
	}

	loadErr := load(ctx)
	if loadErr == nil {
		p.loaded = v
	}
	// 本实例装载失败也广播：库已经变了，其他实例应该重载
	if err = p.rdb.Publish(ctx, policyChangedChannel, strconv.FormatInt(v, 10)).Err(); err != nil {
		p.log.Errorf(ctx, "publish policy change failed (other instances catch up on resubscribe): %v", err)
	}
	return loadErr
}

// reconcile 全局版本号和本实例记录的不一致时重载。
// 用 != 而不是 >：Redis 数据被清空、版本号回退时也重载一次。
func (p *PolicySync) reconcile(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	v, err := p.rdb.Get(ctx, policyVersionKey).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		p.log.Errorf(ctx, "read policy version failed: %v", err)
		return
	}
	if v == p.loaded {
		return
	}
	// 订阅 goroutine 的 ctx 不带 viewer，provider 查角色和 Api 要系统上下文
	if err = p.authz.ReloadPolicies(viewer.WithSystemContext(ctx)); err != nil {
		p.log.Errorf(ctx, "reload policies for version %d failed: %v", v, err)
		return
	}
	p.loaded = v
}
