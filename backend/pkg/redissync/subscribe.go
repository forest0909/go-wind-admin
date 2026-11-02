// Package redissync 多实例协调用的 Redis 小工具：可感知重连的订阅、分布式锁、时间窗内只执行一次。
// Package redissync holds small Redis helpers for multi-instance coordination.
package redissync

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
)

// Logger 只用到 Warnf/Errorf，bLogger.Helper 满足该接口。
type Logger interface {
	Warnf(ctx context.Context, format string, args ...any)
	Errorf(ctx context.Context, format string, args ...any)
}

// Handlers 订阅回调。两者都在订阅 goroutine 里串行执行。
type Handlers struct {
	// OnSubscribed 每次订阅（首次和每次断线重连后）被 Redis 确认时调用。
	// Pub/Sub 不回放离线期间的消息，调用方应在这里做全量对账（清缓存、比对版本号）。
	OnSubscribed func(ctx context.Context)
	// OnMessage 收到一条消息。
	OnMessage func(ctx context.Context, payload string)
}

var (
	// pingInterval 多久收不到任何消息就主动 PING 一次，用来发现半开连接。
	pingInterval = 30 * time.Second
	// retryDelay 订阅断开后重建前的等待。
	retryDelay = time.Second
)

// Subscribe 阻塞地订阅 channel，直到 ctx 结束。
//
// 和 go-redis 的 PubSub.Channel() 不同，这里会把"订阅已（重新）建立"通知给调用方：
// Channel() 在内部静默重连，丢掉的消息调用方无从知晓。任何读错误都会关掉旧的 PubSub、
// 等 retryDelay 后重建，重建成功时再次触发 OnSubscribed。
func Subscribe(ctx context.Context, rdb redis.UniversalClient, channel string, h Handlers, log Logger) {
	for ctx.Err() == nil {
		err := subscribeOnce(ctx, rdb, channel, h)
		if ctx.Err() != nil {
			return
		}
		log.Warnf(ctx, "redis subscription %q broken, resubscribing: %v", channel, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func subscribeOnce(ctx context.Context, rdb redis.UniversalClient, channel string, h Handlers) error {
	ps := rdb.Subscribe(ctx, channel)
	defer func() { _ = ps.Close() }() // 关闭失败只意味着连接已坏，旧 PubSub 本来就要丢弃

	// ctx 结束时关掉 PubSub，打断阻塞中的读。
	stop := context.AfterFunc(ctx, func() { _ = ps.Close() })
	defer stop()

	pingPending := false
	for {
		msg, err := ps.ReceiveTimeout(ctx, pingInterval)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && !pingPending {
				// 一个周期没有任何数据：发 PING，下个周期还收不到 PONG 就判定连接已死。
				if perr := ps.Ping(ctx); perr != nil {
					return perr
				}
				pingPending = true
				continue
			}
			return err
		}
		pingPending = false

		switch m := msg.(type) {
		case *redis.Subscription:
			if m.Kind == "subscribe" && h.OnSubscribed != nil {
				h.OnSubscribed(ctx)
			}
		case *redis.Message:
			if h.OnMessage != nil {
				h.OnMessage(ctx, m.Payload)
			}
		}
	}
}
