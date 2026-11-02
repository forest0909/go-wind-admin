package redissync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type testLogger struct{ t *testing.T }

func (l testLogger) Warnf(_ context.Context, f string, a ...any)  { l.t.Logf("WARN "+f, a...) }
func (l testLogger) Errorf(_ context.Context, f string, a ...any) { l.t.Logf("ERROR "+f, a...) }

func newRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 订阅断开重建后要再次触发 OnSubscribed，调用方才有机会对账离线期间丢掉的消息。
func TestSubscribe_ResubscribeFiresOnSubscribed(t *testing.T) {
	oldDelay := retryDelay
	retryDelay = 20 * time.Millisecond
	t.Cleanup(func() { retryDelay = oldDelay })

	mr, rdb := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var subscribed atomic.Int32
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		Subscribe(ctx, rdb, "ch", Handlers{
			OnSubscribed: func(context.Context) { subscribed.Add(1) },
			OnMessage: func(_ context.Context, p string) {
				mu.Lock()
				got = append(got, p)
				mu.Unlock()
			},
		}, testLogger{t})
	}()

	waitFor(t, "first subscribe", func() bool { return subscribed.Load() == 1 })
	mr.Publish("ch", "a")
	waitFor(t, "message a", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })

	// 模拟 Redis 断线：离线期间发的消息必然丢失，重连后必须再报一次 subscribed。
	mr.Close()
	time.Sleep(50 * time.Millisecond)
	if err := mr.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resubscribe", func() bool { return subscribed.Load() >= 2 })
	waitFor(t, "subscriber registered", func() bool { return mr.PubSubNumSub("ch")["ch"] == 1 })
	mr.Publish("ch", "b")
	waitFor(t, "message b", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 && got[1] == "b" })

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe did not return after ctx cancel")
	}
}

// 两个"实例"并发抢同一把锁，临界区不能重叠；持有时间超过 ttl 也不能被抢走（续期）。
func TestLock_MutualExclusionAndRenewal(t *testing.T) {
	oldPoll := lockPollInterval
	lockPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { lockPollInterval = oldPoll })

	mr, _ := newRedis(t)
	ttl := 150 * time.Millisecond

	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个实例一个独立客户端，模拟不同进程
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			unlock, err := Lock(context.Background(), rdb, "startup", ttl, 10*time.Second, testLogger{t})
			if err != nil {
				t.Error(err)
				return
			}
			n := inside.Add(1)
			for {
				m := maxInside.Load()
				if n <= m || maxInside.CompareAndSwap(m, n) {
					break
				}
			}
			// miniredis 的 TTL 需要手动推进时钟：持有 3 倍 ttl，期间续期必须让锁保持存在
			for j := 0; j < 6; j++ {
				time.Sleep(ttl / 2)
				mr.FastForward(ttl / 2)
			}
			inside.Add(-1)
			unlock()
			unlock() // 可重复调用
		}()
	}
	wg.Wait()
	if maxInside.Load() != 1 {
		t.Fatalf("critical sections overlapped: max concurrent holders = %d", maxInside.Load())
	}
	if mr.Exists("startup") {
		t.Fatal("lock not released")
	}
}

// 锁过期被别人拿走后，原持有者的 unlock 不能删掉别人的锁。
func TestLock_UnlockDoesNotDeleteOthersLock(t *testing.T) {
	mr, rdb := newRedis(t)
	unlock, err := Lock(context.Background(), rdb, "k", time.Hour, time.Second, testLogger{t})
	if err != nil {
		t.Fatal(err)
	}
	mr.Set("k", "someone-else")
	unlock()
	if v, _ := mr.Get("k"); v != "someone-else" {
		t.Fatalf("unlock removed a lock it no longer owns, value=%q", v)
	}
}

func TestLock_WaitTimeout(t *testing.T) {
	mr, rdb := newRedis(t)
	mr.Set("k", "held")
	if _, err := Lock(context.Background(), rdb, "k", time.Second, 50*time.Millisecond, testLogger{t}); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("expected ErrLockTimeout while lock is held, got %v", err)
	}
}

// 多个副本各自入队的同一轮任务只执行一次；失败时释放标记，重试还能执行。
func TestRunOncePerWindow(t *testing.T) {
	mr, rdb := newRedis(t)
	ctx := context.Background()

	var runs atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := RunOncePerWindow(ctx, rdb, "once", time.Hour, func() error {
				runs.Add(1)
				time.Sleep(20 * time.Millisecond)
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if runs.Load() != 1 {
		t.Fatalf("fn ran %d times, want 1", runs.Load())
	}

	// 窗口过后可以再执行
	mr.FastForward(time.Hour + time.Second)
	ran, err := RunOncePerWindow(ctx, rdb, "once", time.Hour, func() error { return nil })
	if err != nil || !ran {
		t.Fatalf("after window: ran=%v err=%v", ran, err)
	}

	// 失败释放标记
	boom := errors.New("boom")
	ran, err = RunOncePerWindow(ctx, rdb, "fail", time.Hour, func() error { return boom })
	if !ran || !errors.Is(err, boom) {
		t.Fatalf("failing fn: ran=%v err=%v", ran, err)
	}
	if mr.Exists("fail") {
		t.Fatal("claim not released after failure, retry would be skipped")
	}
}
