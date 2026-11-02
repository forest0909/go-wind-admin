package server

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	sseServer "go-wind-admin/third_party/kratos-transport/transport/sse"

	"go-wind-admin/pkg/redissync"
	"go-wind-admin/pkg/serviceid"
)

// sseFanoutChannel 跨实例 SSE 事件频道。
var sseFanoutChannel = serviceid.ProjectName + ":sse:fanout"

// localSsePublisher 本实例 SSE 服务的推送能力（*sseServer.Server 满足）。
type localSsePublisher interface {
	TryPublish(ctx context.Context, streamId sseServer.StreamID, event *sseServer.Event) bool
}

// sseEnvelope Redis 上传输的一条 SSE 事件。只带站内信用到的 id / event / data。
type sseEnvelope struct {
	Stream string `json:"s"`
	ID     []byte `json:"i,omitempty"`
	Event  []byte `json:"e,omitempty"`
	Data   []byte `json:"d,omitempty"`
}

// sseFanoutPublisher 多实例 SSE 推送：事件先发到 Redis 频道，每个实例（含发送方自己）
// 订阅后投递给本实例上的连接。用户的 SSE 连接可能在任意一个实例上，发信请求或广播任务
// 可能在另一个实例上执行，只推本地会漏（a4 F8）。
//
// 投递语义和原来一样是尽力而为：Pub/Sub 断线期间的事件会丢，站内信以落库为准，
// 客户端可从收件箱补取。
type sseFanoutPublisher struct {
	rdb   redis.UniversalClient
	local localSsePublisher
	log   *bLogger.Helper
}

func newSseFanoutPublisher(rdb redis.UniversalClient, local localSsePublisher, log *bLogger.Helper) *sseFanoutPublisher {
	return &sseFanoutPublisher{rdb: rdb, local: local, log: log}
}

// Publish 和 TryPublish 一样不阻塞：事件经 Redis 转发，接收端统一用 TryPublish 投递。
func (p *sseFanoutPublisher) Publish(ctx context.Context, streamId sseServer.StreamID, event *sseServer.Event) {
	p.TryPublish(ctx, streamId, event)
}

// TryPublish 发布到 Redis 后返回 true（各实例是否有该流的连接，发送方不知道）。
// Redis 不可用时退回只推本实例。
func (p *sseFanoutPublisher) TryPublish(ctx context.Context, streamId sseServer.StreamID, event *sseServer.Event) bool {
	payload, err := json.Marshal(sseEnvelope{Stream: string(streamId), ID: event.ID, Event: event.Event, Data: event.Data})
	if err != nil {
		p.log.Errorf(ctx, "marshal sse fanout event failed, deliver locally only: %v", err)
		return p.local.TryPublish(ctx, streamId, event)
	}
	if err = p.rdb.Publish(ctx, sseFanoutChannel, payload).Err(); err != nil {
		p.log.Errorf(ctx, "publish sse fanout event failed, deliver locally only: %v", err)
		return p.local.TryPublish(ctx, streamId, event)
	}
	return true
}

// Run 订阅频道并投递给本实例的连接，直到 ctx 结束。
func (p *sseFanoutPublisher) Run(ctx context.Context) {
	redissync.Subscribe(ctx, p.rdb, sseFanoutChannel, redissync.Handlers{
		OnMessage: p.deliver,
	}, p.log)
}

func (p *sseFanoutPublisher) deliver(ctx context.Context, payload string) {
	var env sseEnvelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		p.log.Errorf(ctx, "decode sse fanout event failed: %v", err)
		return
	}
	// 本实例没有该流（用户连在别的实例上）或缓冲已满时返回 false，属正常情况
	p.local.TryPublish(ctx, sseServer.StreamID(env.Stream), &sseServer.Event{ID: env.ID, Event: env.Event, Data: env.Data})
}
