package data

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/pkg/redissync"
)

// 多个实例同时启动：迁移 + 播种阶段不能重叠（a3 F8 "多副本同时启动会并发跑 DDL"）。
func TestStartupLock_SerializesConcurrentStartups(t *testing.T) {
	mr := miniredis.RunT(t)
	l := bLogger.NewHelper(bLogger.NopLogger())

	var inside, overlaps, done atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			release, err := acquireStartupLock(context.Background(), rdb, l, 10*time.Second)
			require.NoError(t, err)
			if inside.Add(1) > 1 {
				overlaps.Add(1)
			}
			time.Sleep(50 * time.Millisecond) // "迁移 + 播种"
			inside.Add(-1)
			release()
			done.Add(1)
		}()
	}
	wg.Wait()
	require.Zero(t, overlaps.Load(), "启动临界区发生重叠")
	require.Equal(t, int32(3), done.Load())
}

// 别的实例一直没完成启动：超时后启动失败，而不是不加锁继续。
func TestStartupLock_TimeoutFails(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Set(startupLockKey, "other-instance")
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	_, err := acquireStartupLock(context.Background(), rdb, bLogger.NewHelper(bLogger.NopLogger()), 50*time.Millisecond)
	require.True(t, errors.Is(err, redissync.ErrLockTimeout), "got %v", err)
}

// Redis 连不上：记错误、不加锁继续启动（单实例不能因为锁起不来）。
func TestStartupLock_RedisDownDoesNotBlockStartup(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	mr.Close()

	release, err := acquireStartupLock(context.Background(), rdb, bLogger.NewHelper(bLogger.NopLogger()), time.Second)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
}

// review-a2-round2 R3：启动期间租约丢失时进程必须退出，不能只记日志继续迁移。
func TestStartupLock_LostLeaseExitsProcess(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	exited := make(chan int, 1)
	exitProcess = func(code int) { exited <- code }
	t.Cleanup(func() { exitProcess = os.Exit })
	oldTTL := startupLockTTL
	startupLockTTL = 90 * time.Millisecond
	t.Cleanup(func() { startupLockTTL = oldTTL })

	release, err := acquireStartupLock(context.Background(), rdb, bLogger.NewHelper(bLogger.NopLogger()), time.Second)
	require.NoError(t, err)
	defer release()

	mr.Set(startupLockKey, "other-instance") // 另一个实例拿到了锁
	select {
	case code := <-exited:
		require.Equal(t, 1, code)
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after losing the startup lock")
	}
}
