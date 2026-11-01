package data

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/third_party/go-utils/trans"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// 权限 Update 的三段写（权限行、接口绑定、菜单绑定）必须同进同退。
// 让最后一段（菜单绑定）失败：删掉 sys_permission_menus 表。
// 修复前三段各自提交：名称已改、接口绑定已换成 {20}，接口返回错误但数据是半更新状态。
func TestPermissionRepoSqlite_UpdateIsAtomic(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemContext(context.Background())

	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name:    trans.Ptr("原名称"),
			Code:    trans.Ptr("perm:atomic"),
			MenuIds: []uint32{1, 2},
			ApiIds:  []uint32{10, 11},
		},
	}))
	p, err := repo.entClient.Client().Permission.Query().Where(permission.CodeEQ("perm:atomic")).Only(ctx)
	require.NoError(t, err)

	_, err = entClient.DB().ExecContext(ctx, `DROP TABLE sys_permission_menus`)
	require.NoError(t, err)

	err = repo.Update(ctx, &permissionV1.UpdatePermissionRequest{
		Id:         p.ID,
		Data:       &permissionV1.Permission{Name: trans.Ptr("新名称"), ApiIds: []uint32{20}, MenuIds: []uint32{3}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name", "api_ids", "menu_ids"}},
	})
	require.Error(t, err, "menu binding failure must fail the update")

	after, err := repo.entClient.Client().Permission.Get(ctx, p.ID)
	require.NoError(t, err)
	require.NotNil(t, after.Name)
	require.Equal(t, "原名称", *after.Name, "permission row must be rolled back")

	links, err := repo.entClient.Client().PermissionApi.Query().Where(permissionapi.PermissionIDEQ(p.ID)).All(ctx)
	require.NoError(t, err)
	var apiIDs []uint32
	for _, l := range links {
		apiIDs = append(apiIDs, *l.APIID)
	}
	sort.Slice(apiIDs, func(i, j int) bool { return apiIDs[i] < apiIDs[j] })
	require.Equal(t, []uint32{10, 11}, apiIDs, "api bindings must be rolled back")
}
