package captcha

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newMiniCaptcha(t *testing.T) *Captcha {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewCaptcha(rdb, WithDriverType(DriverMath), WithExpire(5*time.Minute), WithKeyPrefix("test:consume"))
}

// 答错一次即作废：不能对同一个 id 依次试 0..81。
func TestVerify_WrongAnswerConsumes(t *testing.T) {
	c := newMiniCaptcha(t)
	ctx := context.Background()
	require.NoError(t, c.Save(ctx, "id1", "81"))

	ok, err := c.Verify(ctx, "id1", "80")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = c.Verify(ctx, "id1", "81")
	require.NoError(t, err)
	require.False(t, ok, "答错之后正确答案也不能再通过")

	exists, err := c.Exists(ctx, "id1")
	require.NoError(t, err)
	require.False(t, exists)
}

// 穷举：0..81 逐个提交，最多第一次有机会命中。
func TestVerify_BruteForceFails(t *testing.T) {
	c := newMiniCaptcha(t)
	ctx := context.Background()
	require.NoError(t, c.Save(ctx, "id2", "81"))
	for i := 0; i <= 81; i++ {
		ok, err := c.Verify(ctx, "id2", strconv.Itoa(i))
		require.NoError(t, err)
		require.False(t, ok, "第 %d 次不应通过", i)
	}
}

// 并发提交同一个正确答案，只有一个能通过。
func TestVerify_ConcurrentSingleUse(t *testing.T) {
	c := newMiniCaptcha(t)
	ctx := context.Background()
	require.NoError(t, c.Save(ctx, "id3", "42"))

	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := c.Verify(ctx, "id3", "42")
			if err != nil {
				t.Errorf("verify: %v", err)
				return
			}
			if ok {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), passed.Load())
}
