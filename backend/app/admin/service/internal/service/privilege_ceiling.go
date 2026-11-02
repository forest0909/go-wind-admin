package service

import (
	"context"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/constants"
)

// 授权上限：非超级管理员分配角色、给角色勾权限点时，只能给出自己已经拥有的权限点，
// 不能借 user:edit / role:edit 给自己或别人抬权。超级管理员（platform:admin）不受限。
//
// 操作人的权限集按库里当前启用的角色现查，不信令牌里的角色快照。

// operatorRoleIDsFunc 列出操作人当前持有的角色 ID。
type operatorRoleIDsFunc func(ctx context.Context, userID uint32) ([]uint32, error)

func operatorPermissionSet(ctx context.Context, roleRepo *data.RoleRepo, listRoles operatorRoleIDsFunc, userID uint32) (map[uint32]struct{}, error) {
	roleIDs, err := listRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	roleIDs, err = roleRepo.EnabledRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	set := make(map[uint32]struct{})
	if len(roleIDs) == 0 {
		return set, nil
	}
	permIDs, err := roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range permIDs {
		set[id] = struct{}{}
	}
	return set, nil
}

// ensurePermissionsWithinOperator 要求 permissionIDs ⊆ 操作人的权限集。
func ensurePermissionsWithinOperator(ctx context.Context, roleRepo *data.RoleRepo, listRoles operatorRoleIDsFunc,
	operator *authenticationV1.UserTokenPayload, permissionIDs []uint32) error {
	if operator.GetIsPlatformAdmin() || len(permissionIDs) == 0 {
		return nil
	}
	owned, err := operatorPermissionSet(ctx, roleRepo, listRoles, operator.GetUserId())
	if err != nil {
		return err
	}
	for _, id := range permissionIDs {
		if _, ok := owned[id]; !ok {
			return adminV1.ErrorForbidden("cannot grant permissions you do not have")
		}
	}
	return nil
}

// ensureRolesWithinOperator 要求每个角色的权限点都在操作人的权限集里；
// 超级管理员角色的特权来自角色码而不是权限点，只有超级管理员能分配。
func ensureRolesWithinOperator(ctx context.Context, roleRepo *data.RoleRepo, listRoles operatorRoleIDsFunc,
	operator *authenticationV1.UserTokenPayload, roleIDs []uint32) error {
	if operator.GetIsPlatformAdmin() || len(roleIDs) == 0 {
		return nil
	}
	codes, err := roleRepo.ListRoleCodesByRoleIds(ctx, roleIDs)
	if err != nil {
		return err
	}
	for _, code := range codes {
		if code == constants.PlatformAdminRoleCode {
			return adminV1.ErrorForbidden("only super admin can assign the super admin role")
		}
	}
	permIDs, err := roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return err
	}
	if err = ensurePermissionsWithinOperator(ctx, roleRepo, listRoles, operator, permIDs); err != nil {
		return err
	}

	// 数据范围同样不能超出操作人：权限点相同、范围更大的角色也是抬权。
	roles, err := roleRepo.ListRolesByRoleIds(ctx, roleIDs)
	if err != nil {
		return err
	}
	scopes := make([]identityV1.DataScope, 0, len(roles))
	for _, role := range roles {
		scopes = append(scopes, role.GetDataScope())
	}
	return ensureDataScopesWithinOperator(ctx, roleRepo, listRoles, operator, scopes...)
}

// dataScopeRank 数据范围宽度：SELF < UNIT_ONLY < UNIT_AND_CHILD < ALL。
// SELECTED_UNITS（自定义单元集）和未知值无法与上面这条序列比较，返回 -1，按最保守处理。
func dataScopeRank(scope identityV1.DataScope) int {
	switch scope {
	case identityV1.DataScope_SELF:
		return 1
	case identityV1.DataScope_UNIT_ONLY:
		return 2
	case identityV1.DataScope_UNIT_AND_CHILD:
		return 3
	case identityV1.DataScope_ALL:
		return 4
	default:
		return -1
	}
}

// operatorMaxDataScopeRank 操作人当前启用角色里最宽的数据范围。
func operatorMaxDataScopeRank(ctx context.Context, roleRepo *data.RoleRepo, listRoles operatorRoleIDsFunc, userID uint32) (int, error) {
	roleIDs, err := listRoles(ctx, userID)
	if err != nil {
		return 0, err
	}
	roleIDs, err = roleRepo.EnabledRoleIDs(ctx, roleIDs)
	if err != nil {
		return 0, err
	}
	if len(roleIDs) == 0 {
		return 0, nil
	}
	roles, err := roleRepo.ListRolesByRoleIds(ctx, roleIDs)
	if err != nil {
		return 0, err
	}
	maxRank := 0
	for _, role := range roles {
		if r := dataScopeRank(role.GetDataScope()); r > maxRank {
			maxRank = r
		}
	}
	return maxRank, nil
}

// ensureDataScopesWithinOperator 要求每个数据范围都不宽于操作人自己的最宽范围。
// SELECTED_UNITS / 未知值只有操作人本身是 ALL 才能授出；UNSPECIFIED 不授出任何范围、SELF 只含本人，放行。
func ensureDataScopesWithinOperator(ctx context.Context, roleRepo *data.RoleRepo, listRoles operatorRoleIDsFunc,
	operator *authenticationV1.UserTokenPayload, scopes ...identityV1.DataScope) error {
	if operator.GetIsPlatformAdmin() {
		return nil
	}
	var want []identityV1.DataScope
	for _, sc := range scopes {
		// SELF 是最窄的范围，只看本人数据，不构成抬权，总是可以授出。
		if sc != identityV1.DataScope_DATA_SCOPE_UNSPECIFIED && sc != identityV1.DataScope_SELF {
			want = append(want, sc)
		}
	}
	if len(want) == 0 {
		return nil
	}
	maxRank, err := operatorMaxDataScopeRank(ctx, roleRepo, listRoles, operator.GetUserId())
	if err != nil {
		return err
	}
	allRank := dataScopeRank(identityV1.DataScope_ALL)
	for _, sc := range want {
		r := dataScopeRank(sc)
		if r < 0 {
			if maxRank < allRank {
				return adminV1.ErrorForbidden("cannot grant a data scope broader than your own")
			}
			continue
		}
		if r > maxRank {
			return adminV1.ErrorForbidden("cannot grant a data scope broader than your own")
		}
	}
	return nil
}

// newlyAdded 返回 want 里不在 have 中的元素。
func newlyAdded(want, have []uint32) []uint32 {
	h := make(map[uint32]struct{}, len(have))
	for _, id := range have {
		h[id] = struct{}{}
	}
	var out []uint32
	for _, id := range want {
		if _, ok := h[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}
