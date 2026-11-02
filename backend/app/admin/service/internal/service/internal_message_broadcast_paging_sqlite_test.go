package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	internalMessageV1 "go-wind-admin/api/gen/go/internal_message/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/internalmessagerecipient"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// 全员广播必须投递给所有用户。修复前每页请求 1000 行、被 go-crud 截成 100 行，
// 循环又以"本页不足 1000 行"为终止条件，第一页之后就停了：250 个用户只投递 100 个。
func TestInternalMessageServiceSqlite_BroadcastReachesAllUsers(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())

	userRepo := data.NewUserRepo(boot, entClient,
		data.NewUserRoleRepo(boot, entClient),
		data.NewUserOrgUnitRepo(boot, entClient),
		data.NewUserPositionRepo(boot, entClient),
		nil,
	)
	const userCount = 250
	for i := 0; i < userCount; i++ {
		_, err := userRepo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr(fmt.Sprintf("bc%03d", i))}})
		require.NoError(t, err)
	}

	svc := &InternalMessageService{
		log:                          bLogger.NewHelper(bLogger.NopLogger()),
		internalMessageRepo:          data.NewInternalMessageRepoForTest(entClient),
		internalMessageCategoryRepo:  data.NewInternalMessageCategoryRepoForTest(entClient),
		internalMessageRecipientRepo: data.NewInternalMessageRecipientRepoForTest(entClient),
		userRepo:                     userRepo,
		internalMessagePublisher:     noopInternalMessagePublisher{},
	}
	msg, err := svc.internalMessageRepo.Create(ctx, &internalMessageV1.CreateInternalMessageRequest{
		Data: &internalMessageV1.InternalMessage{
			Title:     trans.Ptr("全员公告"),
			Content:   trans.Ptr("正文"),
			Status:    internalMessageV1.InternalMessage_PUBLISHED.Enum(),
			Type:      internalMessageV1.InternalMessage_NOTIFICATION.Enum(),
			CreatedBy: trans.Ptr(uint32(1)),
		},
	})
	require.NoError(t, err)

	svc.executeBroadcast(ctx, msg.GetId(), 1, 0, msg.GetTitle(), msg.GetContent())

	n, err := entClient.Client().InternalMessageRecipient.Query().
		Where(internalmessagerecipient.MessageIDEQ(msg.GetId())).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, userCount, n, "every user must receive the broadcast")
}
