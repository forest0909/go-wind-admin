package data

import (
	"context"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	crudViewer "go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// 数据范围矩阵的种子（单元 × 用户 × 创建人）：
//
//	单元 1 /1/、单元 2 /1/2/（1 的子）、单元 3 /3/
//	用户 10∈{1}、11∈{2}、12∈{3}、13 无单元、14∈{2,3}
//	用户行的 created_by 一律记为 10：用户表的范围看归属单元，不看创建人
//	登录/操作日志：操作人 10..14 各一条
type dataScopeFixture struct {
	users     *userRepo
	orgUnits  *OrgUnitRepo
	loginLogs *LoginAuditLogRepo
	opLogs    *OperationAuditLogRepo
	logIDByOp map[uint32]uint32 // 操作人 → 登录日志 id
}

func newDataScopeFixture(t *testing.T) *dataScopeFixture {
	t.Helper()
	users := newUserRepoSqlite(t)
	entClient := users.entClient
	client := entClient.Client()
	sys := enttest.NewSystemContext(context.Background())
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())

	for _, u := range []struct {
		id   uint32
		path string
		pid  uint32
	}{{1, "/1/", 0}, {2, "/1/2/", 1}, {3, "/3/", 0}} {
		b := client.OrgUnit.Create().SetID(u.id).SetName("unit").SetCode("U" + string(rune('0'+u.id))).SetPath(u.path)
		if u.pid != 0 {
			b.SetParentID(u.pid)
		}
		require.NoError(t, b.Exec(sys))
	}
	for _, id := range []uint32{10, 11, 12, 13, 14} {
		require.NoError(t, client.User.Create().SetID(id).
			SetUsername("u"+string(rune('0'+id-10))).SetCreatedBy(10).Exec(sys))
	}
	// (tenant_id, user_id, is_primary) 唯一：14 的第二个单元记为主单元
	for _, m := range []struct {
		uid, unit uint32
		primary   bool
	}{{10, 1, false}, {11, 2, false}, {12, 3, false}, {14, 2, false}, {14, 3, true}} {
		require.NoError(t, client.UserOrgUnit.Create().SetUserID(m.uid).SetOrgUnitID(m.unit).SetIsPrimary(m.primary).Exec(sys))
	}

	f := &dataScopeFixture{
		users:     users,
		orgUnits:  NewOrgUnitRepoForTest(entClient),
		loginLogs: NewLoginAuditLogRepo(boot, entClient),
		opLogs:    NewOperationAuditLogRepo(boot, entClient),
		logIDByOp: map[uint32]uint32{},
	}
	for _, id := range []uint32{10, 11, 12, 13, 14} {
		row, err := client.LoginAuditLog.Create().SetUserID(id).Save(sys)
		require.NoError(t, err)
		f.logIDByOp[id] = row.ID
		require.NoError(t, client.OperationAuditLog.Create().SetUserID(id).Exec(sys))
	}
	return f
}

func scopedAs(uid uint64, scopes ...crudViewer.DataScope) context.Context {
	ctx := crudViewer.WithContext(context.Background(), crudViewer.NewUserContext(uid, 0, 0, "", scopes))
	return WithDataScope(ctx)
}

func unitScope(ids ...uint64) crudViewer.DataScope {
	return crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeUnit, TargetIDs: ids}
}

var (
	selfScope = crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeSelf}
	allScope  = crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeAll}
	noneScope = crudViewer.DataScope{ScopeType: crudViewer.ScopeTypeNone}
)

func (f *dataScopeFixture) visibleUsers(t *testing.T, ctx context.Context) []uint32 {
	t.Helper()
	resp, err := f.users.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	out := make([]uint32, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		out = append(out, it.GetId())
	}
	n, err := f.users.Count(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Equal(t, len(out), n, "Count 与 List 的范围一致")
	require.EqualValues(t, len(out), resp.GetTotal(), "List total 与条目一致")
	return out
}

// TestUserDataScopeGuard 用户表：UNIT 类按 user↔单元关系，SELF 只有本人，本人总可见，
// 无单元的用户只有 ALL 看得到；越界 Get 回 404。
func TestUserDataScopeGuard(t *testing.T) {
	f := newDataScopeFixture(t)
	all := []uint32{10, 11, 12, 13, 14}

	cases := []struct {
		name string
		ctx  context.Context
		want []uint32
	}{
		{"ALL", scopedAs(11, allScope), all},
		{"系统上下文", WithDataScope(enttest.NewSystemContext(context.Background())), all},
		{"未标记的 ctx 不过滤（内部查询）", crudViewer.WithContext(context.Background(), crudViewer.NewUserContext(11, 0, 0, "", []crudViewer.DataScope{selfScope})), all},
		{"UNIT_ONLY[1] 本人 10", scopedAs(10, unitScope(1)), []uint32{10}},
		{"UNIT_AND_CHILD[1,2] 本人 10", scopedAs(10, unitScope(1, 2)), []uint32{10, 11, 14}},
		{"SELECTED[3] 本人 10 不在 3 里仍可见自己；created_by=10 不放行", scopedAs(10, unitScope(3)), []uint32{10, 12, 14}},
		{"SELF 本人 11", scopedAs(11, selfScope), []uint32{11}},
		{"UNIT[3] ∪ SELF 本人 11", scopedAs(11, unitScope(3), selfScope), []uint32{11, 12, 14}},
		{"无单元用户 13 带 UNIT[1] 只看到 10 和自己", scopedAs(13, unitScope(1)), []uint32{10, 13}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.ElementsMatch(t, c.want, f.visibleUsers(t, c.ctx))
		})
	}

	for name, ctx := range map[string]context.Context{
		"NONE":   scopedAs(10, noneScope),
		"无 scope": scopedAs(10),
	} {
		_, err := f.users.List(ctx, &paginationV1.PagingRequest{})
		require.Error(t, err, name)
		require.True(t, kerrors.IsForbidden(err), "%s: %v", name, err)
	}

	get := func(ctx context.Context, id uint32) error {
		_, err := f.users.Get(ctx, &identityV1.GetUserRequest{QueryBy: &identityV1.GetUserRequest_Id{Id: id}})
		return err
	}
	require.NoError(t, get(scopedAs(10, unitScope(1, 2)), 14))
	err := get(scopedAs(10, unitScope(1, 2)), 12)
	require.True(t, kerrors.IsNotFound(err), "越界 Get 应 404，得到 %v", err)
	err = get(scopedAs(10, unitScope(1, 2)), 13)
	require.True(t, kerrors.IsNotFound(err), "无单元用户对 UNIT 档越界，得到 %v", err)
	require.NoError(t, get(scopedAs(13, selfScope), 13))
}

// TestOrgUnitDataScopeGuard 组织单元：UNIT 类按目标集，SELF 为本人所属单元；
// 可见子树的父节点不可见时作为根返回；越界 Get 回 404。
func TestOrgUnitDataScopeGuard(t *testing.T) {
	f := newDataScopeFixture(t)

	flatten := func(nodes []*identityV1.OrgUnit) []uint32 {
		var out []uint32
		var walk func([]*identityV1.OrgUnit)
		walk = func(ns []*identityV1.OrgUnit) {
			for _, n := range ns {
				out = append(out, n.GetId())
				walk(n.GetChildren())
			}
		}
		walk(nodes)
		return out
	}
	visible := func(t *testing.T, ctx context.Context) []uint32 {
		t.Helper()
		resp, err := f.orgUnits.List(ctx, &paginationV1.PagingRequest{})
		require.NoError(t, err)
		n, err := f.orgUnits.Count(ctx, &paginationV1.PagingRequest{})
		require.NoError(t, err)
		ids := flatten(resp.GetItems())
		require.Equal(t, len(ids), n)
		require.EqualValues(t, len(ids), resp.GetTotal())
		return ids
	}

	require.ElementsMatch(t, []uint32{1, 2, 3}, visible(t, scopedAs(10, allScope)))
	require.ElementsMatch(t, []uint32{1, 2}, visible(t, scopedAs(10, unitScope(1, 2))))
	require.ElementsMatch(t, []uint32{1}, visible(t, scopedAs(10, unitScope(1))))
	require.ElementsMatch(t, []uint32{2, 3}, visible(t, scopedAs(14, selfScope)), "SELF=本人所属单元")
	require.ElementsMatch(t, []uint32{1, 3}, visible(t, scopedAs(12, unitScope(1), selfScope)))
	require.Empty(t, visible(t, scopedAs(13, selfScope)), "无单元用户 SELF 看不到任何单元")

	// 父节点 1 不可见时，子节点 2 作为根出现（不能被当孤儿丢掉），parent_id 原样保留
	resp, err := f.orgUnits.List(scopedAs(11, unitScope(2)), &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetItems(), 1)
	require.EqualValues(t, 2, resp.GetItems()[0].GetId())
	require.EqualValues(t, 1, resp.GetItems()[0].GetParentId())

	_, err = f.orgUnits.List(scopedAs(10, noneScope), &paginationV1.PagingRequest{})
	require.True(t, kerrors.IsForbidden(err), "NONE 应拒绝，得到 %v", err)

	get := func(ctx context.Context, id uint32) error {
		_, err := f.orgUnits.Get(ctx, &identityV1.GetOrgUnitRequest{QueryBy: &identityV1.GetOrgUnitRequest_Id{Id: id}})
		return err
	}
	require.NoError(t, get(scopedAs(10, unitScope(1, 2)), 2))
	err = get(scopedAs(10, unitScope(1, 2)), 3)
	require.True(t, kerrors.IsNotFound(err), "越界 Get 应 404，得到 %v", err)
	require.NoError(t, get(scopedAs(14, selfScope), 3))
}

// TestAuditLogDataScopeGuard 登录/操作日志：操作人属于"可见用户"才可见。
func TestAuditLogDataScopeGuard(t *testing.T) {
	f := newDataScopeFixture(t)

	loginOps := func(t *testing.T, ctx context.Context) []uint32 {
		t.Helper()
		resp, err := f.loginLogs.List(ctx, &paginationV1.PagingRequest{})
		require.NoError(t, err)
		var out []uint32
		for _, it := range resp.GetItems() {
			out = append(out, it.GetUserId())
		}
		require.EqualValues(t, len(out), resp.GetTotal())
		return out
	}
	opOps := func(t *testing.T, ctx context.Context) []uint32 {
		t.Helper()
		resp, err := f.opLogs.List(ctx, &paginationV1.PagingRequest{})
		require.NoError(t, err)
		var out []uint32
		for _, it := range resp.GetItems() {
			out = append(out, it.GetUserId())
		}
		require.EqualValues(t, len(out), resp.GetTotal())
		return out
	}

	cases := []struct {
		name string
		ctx  context.Context
		want []uint32
	}{
		{"ALL", scopedAs(10, allScope), []uint32{10, 11, 12, 13, 14}},
		{"UNIT[2] 本人 10", scopedAs(10, unitScope(2)), []uint32{10, 11, 14}},
		{"SELF 本人 12", scopedAs(12, selfScope), []uint32{12}},
		{"UNIT[3] ∪ SELF 本人 13", scopedAs(13, unitScope(3), selfScope), []uint32{12, 13, 14}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.ElementsMatch(t, c.want, loginOps(t, c.ctx), "登录日志")
			require.ElementsMatch(t, c.want, opOps(t, c.ctx), "操作日志")
		})
	}

	_, err := f.loginLogs.List(scopedAs(10), &paginationV1.PagingRequest{})
	require.True(t, kerrors.IsForbidden(err), "无 scope 应拒绝，得到 %v", err)

	_, err = f.loginLogs.Get(scopedAs(12, selfScope), &auditV1.GetLoginAuditLogRequest{
		QueryBy: &auditV1.GetLoginAuditLogRequest_Id{Id: f.logIDByOp[11]}})
	require.True(t, kerrors.IsNotFound(err), "越界日志详情应 404，得到 %v", err)
	_, err = f.loginLogs.Get(scopedAs(12, selfScope), &auditV1.GetLoginAuditLogRequest{
		QueryBy: &auditV1.GetLoginAuditLogRequest_Id{Id: f.logIDByOp[12]}})
	require.NoError(t, err)
}
