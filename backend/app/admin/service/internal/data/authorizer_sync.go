package data

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

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
// 那么版本号 ≤ v 的变更都已包含在这次装载里；更大的版本必然触发再次重载。
//
// 版本号只用来省掉收到广播时的多余重载，不作为一致性依据：INCR 失败时版本号不变，
// Redis 从旧快照恢复后版本号也可能撞回本实例记录的值。所以订阅（重新）建立时无条件重载，
// 另外每 policyResyncInterval 无条件全量重载一次，任何漏掉的变更最多这么久收敛。
var (
	policyVersionKey     = serviceid.ProjectName + ":authz:policy_version"
	policyChangedChannel = serviceid.ProjectName + ":authz:policy_changed"

	// policyResyncInterval 无条件全量重载的周期（测试里缩短）。
	policyResyncInterval = 5 * time.Minute
)

// PolicySync 见文件头注释。
type PolicySync struct {
	rdb   redis.UniversalClient
	authz *authorizer.Authorizer
	log   *bLogger.Helper

	// mu 让"读版本号 → 装载 → 记录版本"整体串行，记录的版本才对应得上装载的内容。
	mu     sync.Mutex
	loaded int64

	resyncInterval time.Duration
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
	p := &PolicySync{rdb: rdb, authz: authz, log: log, resyncInterval: policyResyncInterval}
	authz.SetResetWrapper(p.reset)
	return p
}

// Run 阻塞订阅变更广播并定期全量重载，直到 ctx 结束。
func (p *PolicySync) Run(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.resyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.sync(ctx, true)
			}
		}
	}()

	redissync.Subscribe(ctx, p.rdb, policyChangedChannel, redissync.Handlers{
		// 重连期间可能丢了广播，而版本号不一定变了（见文件头），所以无条件重载
		OnSubscribed: func(ctx context.Context) { p.sync(ctx, true) },
		OnMessage:    func(ctx context.Context, _ string) { p.sync(ctx, false) },
	}, p.log)
}

// reset 是 ResetPolicies 的包装：INCR → 本实例装载 → 广播。
func (p *PolicySync) reset(ctx context.Context, load func(context.Context) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	v, err := p.rdb.Incr(ctx, policyVersionKey).Result()
	if err != nil {
		// Redis 不可用：本实例照常生效，其他实例靠订阅重连或周期全量重载收敛（最多 policyResyncInterval）
		p.log.Errorf(ctx, "bump policy version failed, other instances reload on resubscribe or within %s: %v",
			p.resyncInterval, err)
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

// sync 重载本实例策略。force=false 时全局版本号等于本实例记录的就跳过（收到广播时用，
// 发起变更的实例收到自己的广播不重复装载）；force=true 时无条件重载。
// 用 != 而不是 >：版本号回退时也重载。
func (p *PolicySync) sync(ctx context.Context, force bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	v, err := p.rdb.Get(ctx, policyVersionKey).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		if !force {
			p.log.Errorf(ctx, "read policy version failed: %v", err)
			return
		}
		// 强制重载不依赖版本号；记下 -1，下一次广播一定会再重载一次
		p.log.Warnf(ctx, "read policy version failed, reloading anyway: %v", err)
		v = -1
	}
	if !force && v == p.loaded {
		return
	}
	// 订阅 goroutine 的 ctx 不带 viewer，provider 查角色和 Api 要系统上下文
	if err = p.authz.ReloadPolicies(viewer.WithSystemContext(ctx)); err != nil {
		p.log.Errorf(ctx, "reload policies for version %d failed: %v", v, err)
		return
	}
	p.loaded = v
}
