package data

import (
	"context"
	"strings"
	"time"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionmenu"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/third_party/go-crud/viewer"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
)

// syncBulkSize 批量插入每批行数，避免单条语句参数过多
const syncBulkSize = 500

// SyncedPermission 「同步权限」生成的一条权限点。
// LegacyName 是旧版同步写入的名称（菜单内部名），用于判断已有条目能否改名。
type SyncedPermission struct {
	Data       *permissionV1.Permission
	LegacyName string
}

// ShouldRenameSynced 已有条目的名称仍是同步生成的旧值或为空时才改名，管理员手工改过的名称保留。
func ShouldRenameSynced(current, legacy, want string) bool {
	if current == want {
		return false
	}
	return current == "" || current == legacy
}

// MergeSynced 在一个事务里按编码合并同步生成的权限点：
// 已有的原地更新名称和分组（ID 不变，角色绑定保留；状态不动），缺的批量新建；
// 菜单/接口关联先一次查出现状，只删多余、只补缺少。
// 远程数据库上逐条更新加逐条重写关联会超过前端超时，所以这里尽量减少往返次数。
func (r *PermissionRepo) MergeSynced(ctx context.Context, items []SyncedPermission, operatorUserID uint32) (err error) {
	if len(items) == 0 {
		return nil
	}

	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(context.Background(), "transaction rollback failed: %s", rollbackErr.Error())
	})

	existing, err := tx.Permission.Query().
		Select(permission.FieldID, permission.FieldCode, permission.FieldName, permission.FieldGroupID).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permissions failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query permissions failed")
	}
	byCode := make(map[string]*ent.Permission, len(existing))
	for _, p := range existing {
		if p.Code != nil {
			byCode[*p.Code] = p
		}
	}

	now := time.Now()
	wantMenus := make(map[uint32][]uint32)
	wantApis := make(map[uint32][]uint32)
	var toCreate []*permissionV1.Permission

	for _, item := range items {
		data := item.Data
		cur, ok := byCode[data.GetCode()]
		if !ok {
			toCreate = append(toCreate, data)
			continue
		}
		if strings.HasPrefix(data.GetCode(), constants.SystemPermissionCodePrefix) {
			continue
		}

		builder := tx.Permission.UpdateOneID(cur.ID)
		changed := false
		if data.GroupId != nil && (cur.GroupID == nil || *cur.GroupID != data.GetGroupId()) {
			builder.SetGroupID(data.GetGroupId())
			changed = true
		}
		var curName string
		if cur.Name != nil {
			curName = *cur.Name
		}
		if ShouldRenameSynced(curName, item.LegacyName, data.GetName()) {
			builder.SetName(data.GetName())
			changed = true
		}
		if changed {
			if err = builder.SetUpdatedBy(operatorUserID).SetUpdatedAt(now).Exec(ctx); err != nil {
				r.log.Errorf(ctx, "update permission [%s] failed: %s", data.GetCode(), err.Error())
				return permissionV1.ErrorInternalServerError("update permission failed")
			}
		}

		wantMenus[cur.ID] = data.GetMenuIds()
		wantApis[cur.ID] = data.GetApiIds()
	}

	for start := 0; start < len(toCreate); start += syncBulkSize {
		chunk := toCreate[start:min(start+syncBulkSize, len(toCreate))]
		builders := make([]*ent.PermissionCreate, 0, len(chunk))
		for _, data := range chunk {
			builders = append(builders, tx.Permission.Create().
				SetName(data.GetName()).
				SetCode(data.GetCode()).
				SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
				SetNillableDescription(data.Description).
				SetNillableGroupID(data.GroupId).
				SetCreatedBy(operatorUserID).
				SetCreatedAt(now))
		}
		created, cerr := tx.Permission.CreateBulk(builders...).Save(ctx)
		if cerr != nil {
			r.log.Errorf(ctx, "batch insert permissions failed: %s", cerr.Error())
			return permissionV1.ErrorInternalServerError("batch insert permissions failed")
		}
		for i, entity := range created {
			wantMenus[entity.ID] = chunk[i].GetMenuIds()
			wantApis[entity.ID] = chunk[i].GetApiIds()
		}
	}

	if err = r.syncMenuLinks(ctx, tx, wantMenus, operatorUserID, now); err != nil {
		return err
	}
	if err = r.syncApiLinks(ctx, tx, wantApis, operatorUserID, now); err != nil {
		return err
	}

	if err = commitEntTx(tx, &txCommitted); err != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// RetireCodes 迁走不再生成的旧编码：旧权限点上的角色授权转给承接编码，再删除旧权限点及其关联。
// 承接编码有任何一个还不存在（如接口还没同步）就保留该旧编码，等下次同步再迁，避免授权丢失。
// 旧编码不存在时什么都不做，每次同步都可以调用。
func (r *PermissionRepo) RetireCodes(ctx context.Context, retired map[string][]string, operatorUserID uint32) (err error) {
	if len(retired) == 0 {
		return nil
	}
	// 角色授权跨租户，按系统身份读写 / Role bindings span tenants.
	ctx = viewer.WithSystemContext(ctx)

	codes := make([]string, 0, len(retired))
	for old, successors := range retired {
		codes = append(codes, old)
		codes = append(codes, successors...)
	}
	rows, err := r.entClient.Client().Permission.Query().
		Where(permission.CodeIn(codes...)).
		Select(permission.FieldID, permission.FieldCode).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permissions by codes failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query permissions failed")
	}
	idByCode := make(map[string]uint32, len(rows))
	for _, p := range rows {
		if p.Code != nil {
			idByCode[*p.Code] = p.ID
		}
	}

	successorIDs := make(map[uint32][]uint32)
	for old, successors := range retired {
		oldID, ok := idByCode[old]
		if !ok {
			continue
		}
		ids := make([]uint32, 0, len(successors))
		for _, code := range successors {
			if id, ok := idByCode[code]; ok {
				ids = append(ids, id)
			}
		}
		if len(ids) < len(successors) {
			r.log.Warnf(ctx, "retire permission [%s] skipped: successors %v not all synced yet", old, successors)
			continue
		}
		successorIDs[oldID] = ids
	}
	if len(successorIDs) == 0 {
		return nil
	}

	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(context.Background(), "transaction rollback failed: %s", rollbackErr.Error())
	})

	now := time.Now()
	oldIDs := mapKeys(successorIDs)
	bindings, err := tx.RolePermission.Query().
		Where(rolepermission.PermissionIDIn(oldIDs...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query role permissions failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query role permissions failed")
	}
	for _, b := range bindings {
		if b.RoleID == nil || b.PermissionID == nil {
			continue
		}
		for _, newID := range successorIDs[*b.PermissionID] {
			exists, qerr := tx.RolePermission.Query().
				Where(rolepermission.RoleIDEQ(*b.RoleID), rolepermission.PermissionIDEQ(newID)).
				Exist(ctx)
			if qerr != nil {
				r.log.Errorf(ctx, "query role permission failed: %s", qerr.Error())
				return permissionV1.ErrorInternalServerError("query role permission failed")
			}
			if exists {
				continue
			}
			if err = tx.RolePermission.Create().
				SetNillableTenantID(b.TenantID).
				SetRoleID(*b.RoleID).
				SetPermissionID(newID).
				SetNillableEffect(b.Effect).
				SetNillablePriority(b.Priority).
				SetNillableStatus(b.Status).
				SetCreatedBy(operatorUserID).
				SetCreatedAt(now).
				Exec(ctx); err != nil {
				r.log.Errorf(ctx, "migrate role permission failed: %s", err.Error())
				return permissionV1.ErrorInternalServerError("migrate role permission failed")
			}
		}
	}

	if _, err = tx.RolePermission.Delete().Where(rolepermission.PermissionIDIn(oldIDs...)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete retired role permissions failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("delete retired role permissions failed")
	}
	if _, err = tx.PermissionApi.Delete().Where(permissionapi.PermissionIDIn(oldIDs...)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete retired permission apis failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("delete retired permission apis failed")
	}
	if _, err = tx.PermissionMenu.Delete().Where(permissionmenu.PermissionIDIn(oldIDs...)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete retired permission menus failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("delete retired permission menus failed")
	}
	if _, err = tx.Permission.Delete().Where(permission.IDIn(oldIDs...)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete retired permissions failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("delete retired permissions failed")
	}

	if err = commitEntTx(tx, &txCommitted); err != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// linkDiff 对比现有关联与目标关联，返回每个权限要删除的目标 ID 和要新增的 (权限, 目标) 对
func linkDiff(have map[uint32]map[uint32]struct{}, want map[uint32][]uint32) (stale map[uint32][]uint32, missing [][2]uint32) {
	stale = make(map[uint32][]uint32)
	for permID, targets := range want {
		wantSet := make(map[uint32]struct{}, len(targets))
		for _, id := range targets {
			if _, dup := wantSet[id]; dup {
				continue
			}
			wantSet[id] = struct{}{}
			if _, ok := have[permID][id]; !ok {
				missing = append(missing, [2]uint32{permID, id})
			}
		}
		for id := range have[permID] {
			if _, ok := wantSet[id]; !ok {
				stale[permID] = append(stale[permID], id)
			}
		}
	}
	return stale, missing
}

func mapKeys(m map[uint32][]uint32) []uint32 {
	keys := make([]uint32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (r *PermissionRepo) syncMenuLinks(ctx context.Context, tx *ent.Tx, want map[uint32][]uint32, operatorUserID uint32, now time.Time) error {
	if len(want) == 0 {
		return nil
	}

	rows, err := tx.PermissionMenu.Query().
		Where(permissionmenu.PermissionIDIn(mapKeys(want)...)).
		Select(permissionmenu.FieldPermissionID, permissionmenu.FieldMenuID).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission menus failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query permission menus failed")
	}
	have := make(map[uint32]map[uint32]struct{})
	for _, row := range rows {
		if row.PermissionID == nil || row.MenuID == nil {
			continue
		}
		if have[*row.PermissionID] == nil {
			have[*row.PermissionID] = make(map[uint32]struct{})
		}
		have[*row.PermissionID][*row.MenuID] = struct{}{}
	}

	stale, missing := linkDiff(have, want)
	for permID, menuIDs := range stale {
		if _, err = tx.PermissionMenu.Delete().
			Where(permissionmenu.PermissionIDEQ(permID), permissionmenu.MenuIDIn(menuIDs...)).
			Exec(ctx); err != nil {
			r.log.Errorf(ctx, "delete permission menus failed: %s", err.Error())
			return permissionV1.ErrorInternalServerError("delete permission menus failed")
		}
	}

	for start := 0; start < len(missing); start += syncBulkSize {
		chunk := missing[start:min(start+syncBulkSize, len(missing))]
		builders := make([]*ent.PermissionMenuCreate, 0, len(chunk))
		for _, pair := range chunk {
			builders = append(builders, tx.PermissionMenu.Create().
				SetPermissionID(pair[0]).
				SetMenuID(pair[1]).
				SetCreatedBy(operatorUserID).
				SetCreatedAt(now))
		}
		if err = tx.PermissionMenu.CreateBulk(builders...).Exec(ctx); err != nil {
			r.log.Errorf(ctx, "batch insert permission menus failed: %s", err.Error())
			return permissionV1.ErrorInternalServerError("batch insert permission menus failed")
		}
	}
	return nil
}

func (r *PermissionRepo) syncApiLinks(ctx context.Context, tx *ent.Tx, want map[uint32][]uint32, operatorUserID uint32, now time.Time) error {
	if len(want) == 0 {
		return nil
	}

	rows, err := tx.PermissionApi.Query().
		Where(permissionapi.PermissionIDIn(mapKeys(want)...)).
		Select(permissionapi.FieldPermissionID, permissionapi.FieldAPIID).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query permission apis failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("query permission apis failed")
	}
	have := make(map[uint32]map[uint32]struct{})
	for _, row := range rows {
		if row.PermissionID == nil || row.APIID == nil {
			continue
		}
		if have[*row.PermissionID] == nil {
			have[*row.PermissionID] = make(map[uint32]struct{})
		}
		have[*row.PermissionID][*row.APIID] = struct{}{}
	}

	stale, missing := linkDiff(have, want)
	for permID, apiIDs := range stale {
		if _, err = tx.PermissionApi.Delete().
			Where(permissionapi.PermissionIDEQ(permID), permissionapi.APIIDIn(apiIDs...)).
			Exec(ctx); err != nil {
			r.log.Errorf(ctx, "delete permission apis failed: %s", err.Error())
			return permissionV1.ErrorInternalServerError("delete permission apis failed")
		}
	}

	for start := 0; start < len(missing); start += syncBulkSize {
		chunk := missing[start:min(start+syncBulkSize, len(missing))]
		builders := make([]*ent.PermissionApiCreate, 0, len(chunk))
		for _, pair := range chunk {
			builders = append(builders, tx.PermissionApi.Create().
				SetPermissionID(pair[0]).
				SetAPIID(pair[1]).
				SetCreatedBy(operatorUserID).
				SetCreatedAt(now))
		}
		if err = tx.PermissionApi.CreateBulk(builders...).Exec(ctx); err != nil {
			r.log.Errorf(ctx, "batch insert permission apis failed: %s", err.Error())
			return permissionV1.ErrorInternalServerError("batch insert permission apis failed")
		}
	}
	return nil
}
