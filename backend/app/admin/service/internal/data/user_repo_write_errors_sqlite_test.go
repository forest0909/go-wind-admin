package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/third_party/go-utils/trans"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	entUser "go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/userrole"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// 更新不存在的用户：修复前 UpdateOne 的 NotFound 被 assignUserRelations 的结果覆盖，
// 返回 nil 并给 user 9999 写了一行角色关联。
func TestUserRepoSqlite_UpdateMissingUserReturns404(t *testing.T) {
	repo := newUserRepoSqlite(t)
	ctx := enttest.NewSystemContext(context.Background())

	err := repo.Update(ctx, &identityV1.UpdateUserRequest{
		Id:   9999,
		Data: &identityV1.User{Nickname: trans.Ptr("x"), RoleIds: []uint32{7}},
	})
	require.True(t, identityV1.IsNotFound(err), "missing user must be 404, got %v", err)

	n, err := repo.entClient.Client().UserRole.Query().Where(userrole.UserIDEQ(9999)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "no relation rows may be written for a missing user")
}

// 编辑表单把空邮箱提交成 ""：修复前第二个无邮箱用户撞 (tenant_id,email) 唯一索引，
// SQLite 上还会返回 nil、昵称没改、角色却改了。
func TestUserRepoSqlite_EmptyEmailNormalizedToNull(t *testing.T) {
	repo := newUserRepoSqlite(t)
	ctx := enttest.NewSystemContext(context.Background())

	a, err := repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ua"), Email: trans.Ptr("")}})
	require.NoError(t, err)
	b, err := repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ub"), Email: trans.Ptr("  ")}})
	require.NoError(t, err, "second user with blank email must be creatable")

	for _, id := range []uint32{a.GetId(), b.GetId()} {
		err = repo.Update(ctx, &identityV1.UpdateUserRequest{
			Id:         id,
			Data:       &identityV1.User{Email: trans.Ptr(""), Nickname: trans.Ptr("changed"), RoleIds: []uint32{42}},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email", "nickname", "role_ids"}},
		})
		require.NoError(t, err)

		u, err := repo.entClient.Client().User.Query().Where(entUser.IDEQ(id)).Only(ctx)
		require.NoError(t, err)
		require.Nil(t, u.Email, "blank email must be stored as NULL")
		require.NotNil(t, u.Nickname)
		require.Equal(t, "changed", *u.Nickname)
	}
}

// 真正的唯一冲突（同租户同邮箱）映射为 409，Create 和 Update 都是。
func TestUserRepoSqlite_DuplicateEmailReturns409(t *testing.T) {
	repo := newUserRepoSqlite(t)
	ctx := enttest.NewSystemContext(context.Background())

	_, err := repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ua"), Email: trans.Ptr("a@x.com")}})
	require.NoError(t, err)
	_, err = repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ub"), Email: trans.Ptr("a@x.com")}})
	require.True(t, identityV1.IsConflict(err), "duplicate email on create must be 409, got %v", err)

	b, err := repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ub")}})
	require.NoError(t, err)
	err = repo.Update(ctx, &identityV1.UpdateUserRequest{
		Id:         b.GetId(),
		Data:       &identityV1.User{Email: trans.Ptr("a@x.com"), RoleIds: []uint32{42}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email", "role_ids"}},
	})
	require.True(t, identityV1.IsConflict(err), "duplicate email on update must be 409, got %v", err)

	n, err := repo.entClient.Client().UserRole.Query().Where(userrole.UserIDEQ(b.GetId())).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "failed update must not rewrite relations")
}
