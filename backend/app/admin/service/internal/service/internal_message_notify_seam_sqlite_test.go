// 站内信投递的 SQLite 集成测试。
//
// 定向发送必须落下收件行并带上收件行主键推 SSE；全员广播的 SSE 载荷同样必须带主键。
// 缺 id 时前端通知面板会在第一行守卫处丢掉整条推送，桌面通知和未读数都不动，也不报错。
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"go-wind-admin/third_party/kratos-transport/transport/sse"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	internalMessageV1 "go-wind-admin/api/gen/go/internal_message/service/v1"

	"go-wind-admin/pkg/middleware/auth"
)

type payloadRecordingPublisher struct {
	events []sse.Event
}

func (p *payloadRecordingPublisher) Publish(_ context.Context, _ sse.StreamID, event *sse.Event) {
	if event != nil {
		p.events = append(p.events, *event)
	}
}

func (p *payloadRecordingPublisher) TryPublish(_ context.Context, _ sse.StreamID, event *sse.Event) bool {
	p.Publish(nil, "", event)
	return true
}

func ssePayloadAt(t *testing.T, p *payloadRecordingPublisher, idx int) map[string]any {
	t.Helper()
	require.Greater(t, len(p.events), idx, "期望的 SSE 推送不存在")

	var got map[string]any
	require.NoError(t, json.Unmarshal(p.events[idx].Data, &got))
	return got
}

type notifySeamEnv struct {
	im  *InternalMessageService
	ctx context.Context
	pub *payloadRecordingPublisher
}

func newNotifySeamEnv(t *testing.T) *notifySeamEnv {
	t.Helper()

	entClient := enttest.NewEntClientForTest(t)
	pub := &payloadRecordingPublisher{}

	im := &InternalMessageService{
		log:                          bLogger.NewHelper(bLogger.NopLogger()),
		internalMessageRepo:          data.NewInternalMessageRepoForTest(entClient),
		internalMessageCategoryRepo:  data.NewInternalMessageCategoryRepoForTest(entClient),
		internalMessageRecipientRepo: data.NewInternalMessageRecipientRepoForTest(entClient),
		userRepo: &internalMessageServiceUserRepoStub{
			tenantByUserID: map[uint32]uint32{1024: 5},
		},
		internalMessagePublisher: pub,
	}

	ctx := auth.NewContext(
		enttest.NewSystemContext(context.Background()),
		&authenticationV1.UserTokenPayload{UserId: 88},
	)

	return &notifySeamEnv{im: im, ctx: ctx, pub: pub}
}

func TestDirectedSendWritesRecipientAndSSE(t *testing.T) {
	e := newNotifySeamEnv(t)

	resp, err := e.im.SendMessage(e.ctx, &internalMessageV1.SendMessageRequest{
		Type:            internalMessageV1.InternalMessage_NOTIFICATION,
		Title:           trans.Ptr("定向标题"),
		Content:         "定向正文",
		RecipientUserId: trans.Ptr(uint32(1024)),
	})
	require.NoError(t, err)

	inbox, err := e.im.internalMessageRecipientRepo.List(e.ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, inbox.GetItems(), 1)
	require.Equal(t, resp.GetMessageId(), inbox.GetItems()[0].GetMessageId())
	require.Equal(t, uint32(1024), inbox.GetItems()[0].GetRecipientUserId())
	require.Equal(t, uint32(5), inbox.GetItems()[0].GetTenantId(),
		"收件行必须打在收件人的租户上")

	payload := ssePayloadAt(t, e.pub, 0)
	require.NotZero(t, payload["id"], "SSE 载荷必须带收件行主键")
	require.Equal(t, float64(resp.GetMessageId()), payload["messageId"])
	require.Equal(t, "定向标题", payload["title"])
}

func TestBroadcastSsePayloadCarriesRecipientId(t *testing.T) {
	e := newNotifySeamEnv(t)

	mkUsers := func(ids ...uint32) []*identityV1.User {
		users := make([]*identityV1.User, 0, len(ids))
		for _, id := range ids {
			users = append(users, &identityV1.User{Id: trans.Ptr(id)})
		}
		return users
	}
	e.im.userRepo = &broadcastUserRepoStub{
		usersByTenant: map[uint32][]*identityV1.User{7: mkUsers(701, 702, 703)},
	}

	msg, err := e.im.internalMessageRepo.Create(e.ctx, &internalMessageV1.CreateInternalMessageRequest{
		Data: &internalMessageV1.InternalMessage{
			TenantId:  trans.Ptr(uint32(7)),
			Title:     trans.Ptr("广播标题"),
			Content:   trans.Ptr("广播正文"),
			Status:    internalMessageV1.InternalMessage_PUBLISHED.Enum(),
			Type:      internalMessageV1.InternalMessage_NOTIFICATION.Enum(),
			CreatedBy: trans.Ptr(uint32(1)),
		},
	})
	require.NoError(t, err)

	broadcastCtx := viewer.WithContext(context.Background(), viewer.NewUserContext(0, 7, 0, "", nil))
	e.im.executeBroadcast(broadcastCtx, msg.GetId(), 1, 0, "广播标题", "广播正文")

	require.Len(t, e.pub.events, 3, "三个收件人各推一条")
	for i := range e.pub.events {
		payload := ssePayloadAt(t, e.pub, i)
		require.NotZero(t, payload["id"], "第 %d 条推送缺收件行主键", i)
		require.Equal(t, float64(msg.GetId()), payload["messageId"])
	}
}
