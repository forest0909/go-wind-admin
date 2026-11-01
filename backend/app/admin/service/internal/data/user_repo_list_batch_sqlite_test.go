package data

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"

	entCrud "go-wind-admin/third_party/go-crud/entgo"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// countingDriver 统计经 ent 发出的读查询次数。
type countingDriver struct {
	dialect.Driver
	queries atomic.Int64
}

func (d *countingDriver) Query(ctx context.Context, query string, args, v any) error {
	d.queries.Add(1)
	return d.Driver.Query(ctx, query, args, v)
}

// 用户列表回填关联 ID 的查询数不随行数增长。修复前每个用户 3 次（角色/岗位/组织），
// 30 个用户就是 90 次以上；现在三种关联各一次。
func TestUserRepoSqlite_ListRelationsBatched(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:userlist_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	counter := &countingDriver{Driver: drv}
	client := ent.NewClient(ent.Driver(counter))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(context.Background()))

	repo := newUserRepoSqlite(t)
	shared := entCrud.NewEntClient[*ent.Client](client, drv)
	repo.entClient = shared
	repo.userRoleRepo.entClient = shared
	repo.userOrgUnitRepo.entClient = shared
	repo.userPositionRepo.entClient = shared

	ctx := enttest.NewSystemContext(context.Background())
	const n = 30
	for i := 0; i < n; i++ {
		_, err := repo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{
			Username:    trans.Ptr(fmt.Sprintf("u%02d", i)),
			RoleIds:     []uint32{uint32(100 + i), 999},
			OrgUnitIds:  []uint32{uint32(200 + i)},
			PositionIds: []uint32{uint32(300 + i)},
		}})
		require.NoError(t, err)
	}

	counter.queries.Store(0)
	resp, err := repo.List(ctx, &paginationV1.PagingRequest{Page: trans.Ptr(uint32(1)), PageSize: trans.Ptr(uint32(100))})
	require.NoError(t, err)
	require.Len(t, resp.GetItems(), n)
	require.LessOrEqual(t, counter.queries.Load(), int64(10), "relation lookups must not be per user")

	for _, u := range resp.GetItems() {
		var idx int
		_, err := fmt.Sscanf(u.GetUsername(), "u%02d", &idx)
		require.NoError(t, err)
		require.ElementsMatch(t, []uint32{uint32(100 + idx), 999}, u.GetRoleIds())
		require.Equal(t, []uint32{uint32(200 + idx)}, u.GetOrgUnitIds())
		require.Equal(t, []uint32{uint32(300 + idx)}, u.GetPositionIds())
	}
}
