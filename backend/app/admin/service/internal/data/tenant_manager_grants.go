package data

import (
	"context"
	"time"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/third_party/go-crud/viewer"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
)

// SyncTenantManagerGrants 让租户管理员角色（模板和各租户由模板复制出的角色）恰好拥有
// constants.IsTenantManagerPermissionCode 选中的启用权限点。
//
// 租户管理员的含义是「租户内的全部功能」。权限点由同步按菜单和接口生成，随版本增减，
// 写死在种子里的清单必然过时：种子按位置绑了第 1~128 号接口，后来新增的接口
// （含用户列表）全部 403，细分出来的按钮权限点也一个都没有，页面上没有新增按钮。
// 所以每次同步权限点后重算一遍。只增删差异，没有变化时只有查询。
func (r *PermissionRepo) SyncTenantManagerGrants(ctx context.Context, operatorUserID uint32) error {
	// 要跨租户改所有租户的管理员角色
	ctx = viewer.WithSystemContext(ctx)

	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(context.Background(), "transaction rollback failed: %s", rollbackErr.Error())
	})

	perms, err := tx.Permission.Query().
		Where(permission.StatusEQ(permission.StatusOn)).
		Select(permission.FieldID, permission.FieldCode).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permissions failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query permissions failed")
	}
	want := make(map[uint32]struct{})
	for _, p := range perms {
		if p.Code != nil && constants.IsTenantManagerPermissionCode(*p.Code) {
			want[p.ID] = struct{}{}
		}
	}

	roles, err := tx.Role.Query().
		Where(role.CodeIn(constants.TenantAdminRoleCode, constants.TenantAdminTemplateRoleCode)).
		Select(role.FieldID, role.FieldTenantID).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant manager roles failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query tenant manager roles failed")
	}
	if len(roles) == 0 {
		return nil
	}
	roleIDs := make([]uint32, 0, len(roles))
	for _, ro := range roles {
		roleIDs = append(roleIDs, ro.ID)
	}

	rows, err := tx.RolePermission.Query().
		Where(rolepermission.RoleIDIn(roleIDs...)).
		Select(rolepermission.FieldRoleID, rolepermission.FieldPermissionID).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query tenant manager grants failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query tenant manager grants failed")
	}
	have := make(map[uint32]map[uint32]struct{}, len(roles))
	for _, row := range rows {
		if row.RoleID == nil || row.PermissionID == nil {
			continue
		}
		if have[*row.RoleID] == nil {
			have[*row.RoleID] = make(map[uint32]struct{})
		}
		have[*row.RoleID][*row.PermissionID] = struct{}{}
	}

	now := time.Now()
	var toCreate []*ent.RolePermissionCreate
	for _, ro := range roles {
		var stale []uint32
		for pid := range have[ro.ID] {
			if _, ok := want[pid]; !ok {
				stale = append(stale, pid)
			}
		}
		if len(stale) > 0 {
			if _, err = tx.RolePermission.Delete().
				Where(rolepermission.RoleIDEQ(ro.ID), rolepermission.PermissionIDIn(stale...)).
				Exec(ctx); err != nil {
				r.log.Errorf(ctx, "delete stale grants of role [%d] failed: %s", ro.ID, err.Error())
				return permissionV1.ErrorInternalServerError("delete stale tenant manager grants failed")
			}
		}

		tenantID := uint32(0)
		if ro.TenantID != nil {
			tenantID = *ro.TenantID
		}
		for pid := range want {
			if _, ok := have[ro.ID][pid]; ok {
				continue
			}
			toCreate = append(toCreate, tx.RolePermission.Create().
				SetTenantID(tenantID).
				SetRoleID(ro.ID).
				SetPermissionID(pid).
				SetCreatedBy(operatorUserID).
				SetCreatedAt(now))
		}
	}

	for start := 0; start < len(toCreate); start += syncBulkSize {
		chunk := toCreate[start:min(start+syncBulkSize, len(toCreate))]
		if err = tx.RolePermission.CreateBulk(chunk...).Exec(ctx); err != nil {
			r.log.Errorf(ctx, "insert tenant manager grants failed: %s", err.Error())
			return permissionV1.ErrorInternalServerError("insert tenant manager grants failed")
		}
	}

	if err = commitEntTx(tx, &txCommitted); err != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}
