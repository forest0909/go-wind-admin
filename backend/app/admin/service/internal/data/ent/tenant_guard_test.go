package ent_test

import (
	"context"
	"testing"

	"go-wind-admin/third_party/go-crud/viewer"

	ent "go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/dicttype"
)

// mockViewer 测试用 ViewerContext：模拟指定租户的用户。
type mockViewer struct {
	tid uint64
}

func (m mockViewer) UserID() uint64                    { return m.tid }
func (m mockViewer) TenantID() uint64                  { return m.tid }
func (m mockViewer) OrgUnitID() uint64                 { return 0 }
func (m mockViewer) Permissions() []string             { return nil }
func (m mockViewer) Roles() []string                   { return nil }
func (m mockViewer) DataScope() []viewer.DataScope     { return nil }
func (m mockViewer) TraceID() string                   { return "test" }
func (m mockViewer) HasPermission(string, string) bool { return true }
func (m mockViewer) IsPlatformContext() bool           { return false }
func (m mockViewer) IsTenantContext() bool             { return m.tid > 0 }
func (m mockViewer) IsSystemContext() bool             { return false }
func (m mockViewer) ShouldAudit() bool                 { return false }

func openGuardTestClient(t *testing.T) *ent.Client {
	t.Helper()
	return openGuardClient(t, "sys_dict_types", "sys_dict_entries")
}

func tenantCtx(ctx context.Context, tid uint64) context.Context {
	return viewer.WithContext(ctx, mockViewer{tid: tid})
}

// TestTenantMutationGuard 验证跨租户 Update/Delete 被 TenantMutationGuardPolicy 拦截，
// 本租户变更不受影响。
func TestTenantMutationGuard(t *testing.T) {
	client := openGuardTestClient(t)
	ctx := context.Background()

	// 种数据：分别属于租户 1 / 2（用租户自身 viewer 写入，Create 强制覆盖 tenant_id）
	{
		ctx1 := tenantCtx(ctx, 1)
		client.DictType.Create().SetTypeCode("GUARD_T1").SetTypeName("tenant1-type").SetIsEnabled(true).ExecX(ctx1)
		ctx2 := tenantCtx(ctx, 2)
		client.DictType.Create().SetTypeCode("GUARD_T2").SetTypeName("tenant2-type").SetIsEnabled(true).ExecX(ctx2)
	}

	ctx1 := tenantCtx(ctx, 1)
	ctx2 := tenantCtx(ctx, 2)

	// 用 type_code 反查主键 id（供 Update/Delete 使用）
	t1ID := dictTypeIDByCode(client, ctx1, "GUARD_T1")
	t2ID := dictTypeIDByCode(client, ctx2, "GUARD_T2")

	// Update：租户 1 用户改租户 2 的行 → 必须命中 0 行（隔离生效）
	n := client.DictType.Update().Where(dicttype.IDEQ(t2ID)).SetTypeName("hacked").SaveX(ctx1)
	if n != 0 {
		t.Fatalf("cross-tenant update affected %d rows, want 0", n)
	}

	// 跨租户改名后，租户 2 视角下的行保持原状
	if got := *client.DictType.GetX(ctx2, t2ID).TypeName; got != "tenant2-type" {
		t.Fatalf("cross-tenant update leaked: name=%q", got)
	}

	// Update：本租户行正常更新
	n = client.DictType.Update().Where(dicttype.IDEQ(t1ID)).SetTypeName("t1-renamed").SaveX(ctx1)
	if n != 1 {
		t.Fatalf("own update affected %d rows, want 1", n)
	}

	// Delete：租户 1 用户删租户 2 的行 → 必须命中 0 行（隔离生效）
	n = client.DictType.Delete().Where(dicttype.IDEQ(t2ID)).ExecX(ctx1)
	if n != 0 {
		t.Fatalf("cross-tenant delete affected %d rows, want 0", n)
	}

	// 跨租户行未被删除
	if !client.DictType.Query().Where(dicttype.IDEQ(t2ID)).ExistX(ctx2) {
		t.Fatal("cross-tenant delete removed the row")
	}

	// Delete：本租户行正常删除
	n = client.DictType.Delete().Where(dicttype.IDEQ(t1ID)).ExecX(ctx1)
	if n != 1 {
		t.Fatalf("own delete affected %d rows, want 1", n)
	}
}

// dictTypeIDByCode 在指定租户视角下按 type_code 反查主键。
func dictTypeIDByCode(client *ent.Client, ctx context.Context, code string) uint32 {
	row := client.DictType.Query().Where(dicttype.TypeCodeEQ(code)).OnlyX(ctx)
	return row.ID
}
