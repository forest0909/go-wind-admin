package data

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/pkg/redissync"
	"go-wind-admin/pkg/serviceid"
)

var startupLockKey = serviceid.ProjectName + ":startup:migrate-and-seed"

const (
	// startupLockTTL 锁租期，持有期间自动续期；持有者崩溃后最多这么久自动释放。
	startupLockTTL = 30 * time.Second
	// startupLockWait 等别的实例完成启动的最长时间，超过就启动失败（交给编排重启）。
	startupLockWait = 10 * time.Minute
)

// AcquireStartupLock 多实例同时启动时，让自动迁移和默认数据播种 / 接口、权限点、菜单同步串行执行。
//
// 用 Redis 锁而不是 PG advisory lock：对 PostgreSQL、MySQL、SQLite 一样适用，
// 而且 admin 本来就依赖 Redis。Redis 不可用时只记错误、不加锁继续启动（等同修复前的单实例行为），
// 这样只有 Redis 故障时才可能出现并发写，不至于因为锁让单实例起不来。
// 返回的 release 可重复调用。
func AcquireStartupLock(ctx *bootstrap.Context, rdb *redis.Client) (release func(), err error) {
	l := ctx.NewLoggerHelper("startup-lock/data/admin-service")
	if rdb == nil {
		l.Warnf(context.Background(), "redis is not configured, startup migration/seed runs without a cross-instance lock")
		return func() {}, nil
	}
	return acquireStartupLock(ctx.Context(), rdb, l, startupLockWait)
}

func acquireStartupLock(ctx context.Context, rdb redis.UniversalClient, l *bLogger.Helper, wait time.Duration) (func(), error) {
	start := time.Now()
	release, err := redissync.Lock(ctx, rdb, startupLockKey, startupLockTTL, wait, l)
	if err != nil {
		if errors.Is(err, redissync.ErrLockTimeout) || ctx.Err() != nil {
			return nil, err
		}
		l.Errorf(ctx, "startup lock unavailable, continuing WITHOUT cross-instance lock: %v", err)
		return func() {}, nil
	}
	l.Infof(ctx, "startup lock acquired after %s", time.Since(start).Round(time.Millisecond))
	return release, nil
}
