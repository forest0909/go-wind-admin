package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	crudViewer "go-wind-admin/third_party/go-crud/viewer"

	"go-wind-admin/app/admin/service/internal/data/ent/position"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// TestPositionDataScopeSqlite_TenantZero tenantID==0 不再跳过岗位数据范围。
// 用户 A 带 SELF 只能看到自己创建的岗位；ALL 与系统上下文看到全部；没有 scope 拒绝。
func TestPositionDataScopeSqlite_TenantZero(t *testing.T) {
	repo := newPositionRepoSqlite(t)
	sys := enttest.NewSystemContext(context.Background())
	client := repo.entClient.Client()

	for _, row := range []struct {
		code string
		by   uint32
	}{
		{"POS_SCOPE_A", 11},
		{"POS_SCOPE_B", 12},
	} {
		err := client.Position.Create().
			SetName(row.code).
			SetCode(row.code).
			SetTenantID(0).
			SetOrgUnitID(1).
			SetCreatedBy(row.by).
			SetStatus(position.StatusOn).
			Exec(sys)
		require.NoError(t, err)
	}

	asUser := func(scopes ...crudViewer.DataScope) context.Context {
		return crudViewer.WithContext(context.Background(), crudViewer.NewUserContext(11, 0, 0, "", scopes))
	}
	codes := func(t *testing.T, ctx context.Context) []string {
		t.Helper()
		listed, err := repo.List(ctx, &paginationV1.PagingRequest{})
		require.NoError(t, err)
		out := make([]string, 0, len(listed.GetItems()))
		for _, item := range listed.GetItems() {
			out = append(out, item.GetCode())
		}
		return out
	}

	require.Equal(t, []string{"POS_SCOPE_A"}, codes(t, asUser(crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeSelf})),
		"tid=0 的 SELF 只返回 A 自己创建的岗位")
	require.ElementsMatch(t, []string{"POS_SCOPE_A", "POS_SCOPE_B"},
		codes(t, asUser(crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeAll})),
		"令牌 ALL 返回两条")
	require.ElementsMatch(t, []string{"POS_SCOPE_A", "POS_SCOPE_B"}, codes(t, sys),
		"系统上下文返回两条")

	_, err := repo.List(asUser(), &paginationV1.PagingRequest{})
	require.Error(t, err, "没有数据范围应 fail-closed")
}
