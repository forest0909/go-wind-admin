package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	sseServer "go-wind-admin/third_party/kratos-transport/transport/sse"
)

// fakeLocalSse 一个实例本地的 SSE 连接表：只有 streams 里的流在本实例上有连接。
type fakeLocalSse struct {
	mu        sync.Mutex
	streams   map[sseServer.StreamID]bool
	delivered map[sseServer.StreamID][]string
}

func newFakeLocalSse(streams ...sseServer.StreamID) *fakeLocalSse {
	f := &fakeLocalSse{streams: map[sseServer.StreamID]bool{}, delivered: map[sseServer.StreamID][]string{}}
	for _, s := range streams {
		f.streams[s] = true
	}
	return f
}

func (f *fakeLocalSse) TryPublish(_ context.Context, id sseServer.StreamID, ev *sseServer.Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.streams[id] {
		return false
	}
	f.delivered[id] = append(f.delivered[id], string(ev.Event)+":"+string(ev.Data))
	return true
}

func (f *fakeLocalSse) got(id sseServer.StreamID) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.delivered[id]...)
}

// a4 F8：用户 42 连在实例 A，通知由实例 B 发出。修复前 B 只查本地流，直接跳过。
func TestSseFanout_DeliversToInstanceHoldingTheStream(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	newInstance := func(local *fakeLocalSse) *sseFanoutPublisher {
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		p := newSseFanoutPublisher(rdb, local, bLogger.NewHelper(bLogger.NopLogger()))
		go p.Run(ctx)
		return p
	}
	localA := newFakeLocalSse("42")
	localB := newFakeLocalSse("7")
	_ = newInstance(localA)
	b := newInstance(localB)
	require.Eventually(t, func() bool { return mr.PubSubNumSub(sseFanoutChannel)[sseFanoutChannel] == 2 },
		5*time.Second, 10*time.Millisecond)

	require.True(t, b.TryPublish(ctx, "42", &sseServer.Event{Event: []byte("notification"), Data: []byte(`{"id":1}`)}))
	require.True(t, b.TryPublish(ctx, "7", &sseServer.Event{Event: []byte("notification"), Data: []byte(`{"id":2}`)}))

	require.Eventually(t, func() bool { return len(localA.got("42")) == 1 && len(localB.got("7")) == 1 },
		5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{`notification:{"id":1}`}, localA.got("42"), "A 上的连接应收到 B 发出的通知")
	require.Equal(t, []string{`notification:{"id":2}`}, localB.got("7"), "发送方自己的连接也只投递一次")
	time.Sleep(50 * time.Millisecond)
	require.Len(t, localA.got("42"), 1, "不重复投递")
	require.Len(t, localB.got("7"), 1, "不重复投递")
}

// Redis 不可用时退回本地投递，单实例不丢实时通知。
func TestSseFanout_FallsBackToLocalWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	local := newFakeLocalSse("42")
	p := newSseFanoutPublisher(rdb, local, bLogger.NewHelper(bLogger.NopLogger()))
	mr.Close()

	require.True(t, p.TryPublish(context.Background(), "42", &sseServer.Event{Event: []byte("n"), Data: []byte("x")}))
	require.Equal(t, []string{"n:x"}, local.got("42"))
}
