package server

import (
	"context"
	"strconv"

	"github.com/redis/go-redis/v9"

	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	"go-wind-admin/third_party/kratos-bootstrap/transport/sse"

	sseServer "go-wind-admin/third_party/kratos-transport/transport/sse"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

// NewSseServer creates a new SSE server.
func NewSseServer(
	ctx *bootstrap.Context,
	internalMessageService *service.InternalMessageService,
	authenticator *data.Authenticator,
	rdb *redis.Client,
) *sseServer.Server {
	cfg := ctx.GetConfig()

	if cfg == nil || cfg.Server == nil || cfg.Server.Sse == nil {
		return nil
	}

	srv := sse.NewSseServer(cfg.Server.Sse,
		sseServer.WithSubscriberFunction(internalMessageService.HandleSubscribe),
		sseServer.WithAuthorizeFunc(internalMessageService.HandleAuthorize),
	)

	if rdb != nil {
		// 多实例：经 Redis 转发，每个实例投递给自己的连接
		fanout := newSseFanoutPublisher(rdb, srv, ctx.NewLoggerHelper("sse-fanout/server/admin-service"))
		go fanout.Run(ctx.Context())
		internalMessageService.RegisterInternalMessagePublisher(fanout)
	} else {
		internalMessageService.RegisterInternalMessagePublisher(srv)
	}

	// 用户全部令牌被吊销（登出、改密、停用、删除、全部下线）时断开其已建立的 SSE 连接。
	// SSE 只在建连时校验令牌，不断开的话旧连接会继续收消息。流 ID 即用户 ID。
	if authenticator != nil {
		authenticator.OnUserTokensRevoked(func(_ context.Context, userId uint32) {
			srv.CloseStream(sseServer.StreamID(strconv.FormatUint(uint64(userId), 10)))
		})
	}

	//srv.CreateStream("test")

	return srv
}
