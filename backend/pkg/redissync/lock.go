package redissync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 只有持有者（值等于自己的 token）才能续期或释放，避免锁过期后误删别人的锁。
var (
	renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0`)
	releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)
)

// ErrLockTimeout 等待超过 wait 仍未拿到锁。
var ErrLockTimeout = errors.New("lock still held by another holder")

// lockPollInterval 抢锁失败后的重试间隔。
var lockPollInterval = 200 * time.Millisecond

// Lock 阻塞获取分布式互斥锁，直到拿到锁、ctx 结束或等待超过 wait。
//
// 锁的租期是 ttl，持有期间后台每 ttl/3 续期一次，所以临界区可以比 ttl 长；
// 持有者进程崩溃时最多 ttl 后自动释放。返回的 unlock 可重复调用。
func Lock(ctx context.Context, rdb redis.UniversalClient, key string, ttl, wait time.Duration, log Logger) (unlock func(), err error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(wait)
	for {
		ok, err := rdb.SetNX(ctx, key, token, ttl).Result()
		if err != nil {
			return nil, fmt.Errorf("acquire lock %q: %w", key, err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire lock %q after waiting %s: %w", key, wait, ErrLockTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}

	stopRenew := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stopRenew:
				return
			case <-ticker.C:
				n, err := renewScript.Run(context.Background(), rdb, []string{key}, token, ttl.Milliseconds()).Int()
				if err != nil {
					log.Errorf(ctx, "renew lock %q failed: %v", key, err)
				} else if n == 0 {
					log.Errorf(ctx, "lock %q lost before release (expired or taken over)", key)
					return
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stopRenew)
			<-renewDone
			if err := releaseScript.Run(context.Background(), rdb, []string{key}, token).Err(); err != nil {
				log.Errorf(ctx, "release lock %q failed (expires in %s): %v", key, ttl, err)
			}
		})
	}, nil
}

// RunOncePerWindow 在 window 时间窗内，同一个 key 只让一个调用者执行 fn。
//
// 用于多副本各自的 cron 都入队了同一轮系统任务的情况：先到的执行，后到的直接跳过（返回 ran=false）。
// fn 失败时删除标记，好让 asynq 的重试还能再执行一次。window 必须明显短于任务周期。
func RunOncePerWindow(ctx context.Context, rdb redis.UniversalClient, key string, window time.Duration, fn func() error) (ran bool, err error) {
	token, err := newToken()
	if err != nil {
		return false, err
	}
	ok, err := rdb.SetNX(ctx, key, token, window).Result()
	if err != nil {
		return false, fmt.Errorf("claim %q: %w", key, err)
	}
	if !ok {
		return false, nil
	}
	if err = fn(); err != nil {
		if rerr := releaseScript.Run(context.Background(), rdb, []string{key}, token).Err(); rerr != nil {
			return true, errors.Join(err, fmt.Errorf("release claim %q: %w", key, rerr))
		}
		return true, err
	}
	return true, nil
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate lock token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
