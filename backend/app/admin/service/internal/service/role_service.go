package service

import (
	"context"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/aggregator"
	"go-wind-admin/third_party/go-utils/stringcase"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/authorizer"
	"go-wind-admin/third_party/go-utils/sliceutil"
)

type RoleService struct {
	adminV1.RoleServiceHTTPServer

	log *bLogger.Helper

	authorizer *authorizer.Authorizer

	roleRepo     *data.RoleRepo
	tenantRepo   *data.TenantRepo
	userRoleRepo *data.UserRoleRepo

	authenticator *data.Authenticator
}

func NewRoleService(
	ctx *bootstrap.Context,
	authorizer *authorizer.Authorizer,
	roleRepo *data.RoleRepo,
	tenantRepo *data.TenantRepo,
	userRoleRepo *data.UserRoleRepo,
	authenticator *data.Authenticator,
) *RoleService {
	svc := &RoleService{
		log:           ctx.NewLoggerHelper("role/service/admin-service"),
		authorizer:    authorizer,
		roleRepo:      roleRepo,
		tenantRepo:    tenantRepo,
		userRoleRepo:  userRoleRepo,
		authenticator: authenticator,
	}

	svc.init()

	return svc
}

func (s *RoleService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.roleRepo.Count(ctx, nil); count == 0 {
		_ = s.createDefaultRoles(ctx)
	}
}

func (s *RoleService) extractRelationIDs(
	roles []*permissionV1.Role,
	tenantSet aggregator.ResourceMap[uint32, *identityV1.Tenant],
) {
	for _, p := range roles {
		if p.GetTenantId() > 0 {
			tenantSet[p.GetTenantId()] = nil
		}
	}
}

func (s *RoleService) fetchRelationInfo(
	ctx context.Context,
	tenantSet aggregator.ResourceMap[uint32, *identityV1.Tenant],
) error {
	if len(tenantSet) > 0 {
		tenantIds := make([]uint32, 0, len(tenantSet))
		for id := range tenantSet {
			tenantIds = append(tenantIds, id)
		}

		tenants, err := s.tenantRepo.ListTenantsByIds(ctx, tenantIds)
		if err != nil {
			s.log.Errorf(context.Background(), "query tenants err: %v", err)
			return err
		}

		for _, tenant := range tenants {
			tenantSet[tenant.GetId()] = tenant
		}
	}

	return nil
}

func (s *RoleService) bindRelations(
	roles []*permissionV1.Role,
	tenantSet aggregator.ResourceMap[uint32, *identityV1.Tenant],
) {
	aggregator.Populate(
		roles,
		tenantSet,
		func(ou *permissionV1.Role) uint32 { return ou.GetTenantId() },
		func(ou *permissionV1.Role, r *identityV1.Tenant) {
			ou.TenantName = r.Name
		},
	)
}

func (s *RoleService) enrichRelations(ctx context.Context, roles []*permissionV1.Role) error {
	var tenantSet = make(aggregator.ResourceMap[uint32, *identityV1.Tenant])
	s.extractRelationIDs(roles, tenantSet)
	if err := s.fetchRelationInfo(ctx, tenantSet); err != nil {
		return err
	}
	s.bindRelations(roles, tenantSet)
	return nil
}

func (s *RoleService) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListRoleResponse, error) {
	resp, err := s.roleRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	_ = s.enrichRelations(ctx, resp.Items)

	return resp, nil
}

func (s *RoleService) Count(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.CountRoleResponse, error) {
	count, err := s.roleRepo.Count(ctx, req)
	if err != nil {
		return nil, err
	}

	return &permissionV1.CountRoleResponse{
		Count: uint64(count),
	}, nil
}

func (s *RoleService) Get(ctx context.Context, req *permissionV1.GetRoleRequest) (*permissionV1.Role, error) {
	resp, err := s.roleRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	fakeItems := []*permissionV1.Role{resp}
	_ = s.enrichRelations(ctx, fakeItems)

	return resp, nil
}

func (s *RoleService) Create(ctx context.Context, req *permissionV1.CreateRoleRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if operator.GetTenantId() > 0 && req.Data.GetType() != permissionV1.Role_TENANT {
		req.Data.Type = trans.Ptr(permissionV1.Role_TENANT)
	}

	if err = s.roleRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleService) Update(ctx context.Context, req *permissionV1.UpdateRoleRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		s.log.Errorf(ctx, "get operator from context error: %v", err)
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if operator.GetTenantId() > 0 && req.Data.GetType() != permissionV1.Role_TENANT {
		req.Data.Type = trans.Ptr(permissionV1.Role_TENANT)
	}

	r, err := s.roleRepo.Get(ctx, &permissionV1.GetRoleRequest{
		QueryBy: &permissionV1.GetRoleRequest_Id{
			Id: req.GetId(),
		},
	})
	if err != nil {
		return nil, err
	}

	// 非系统管理员禁止修改系统角色
	if r.GetType() == permissionV1.Role_SYSTEM && operator.GetTenantId() > 0 {
		return nil, adminV1.ErrorForbidden("no permission to update system role")
	}

	// Get 会填权限、组织单元和字段权限；填充失败时只记日志，这里补查。
	// 查不到就不能判断值有没有变，直接失败，不把空集当成旧授权。
	if err = s.roleRepo.EnsureGrantSnapshot(ctx, r); err != nil {
		s.log.Errorf(ctx, "load role grant snapshot failed: %v", err)
		return nil, err
	}

	// 保护角色字段不可修改。GetUpdateMask() 可能是 nil，不能直接读 Paths。
	if r.GetIsProtected() {
		if mask := req.GetUpdateMask(); mask != nil && len(mask.GetPaths()) > 0 {
			mask.Paths = sliceutil.FilterBlacklist(mask.GetPaths(), []string{
				"is_protected",
				"type",
				"status",
				"code",
			})
		} else {
			req.Data.IsProtected = nil
			req.Data.Type = nil
			req.Data.Status = nil
			req.Data.Code = nil
		}
	}

	// 在 roleRepo.Update 改写 mask 之前，按仓库实际会写入的值比较。
	// 页面保存会带上未改动的权限和数据范围，值相同则不吊销。
	revokeHolders := roleUpdateChangesGrant(r, req)

	if err = s.roleRepo.Update(ctx, req); err != nil {
		s.log.Errorf(ctx, "update role error: %v", err)
		return nil, err
	}

	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	if revokeHolders {
		s.revokeRoleHolders(ctx, req.GetId())
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleService) Delete(ctx context.Context, req *permissionV1.DeleteRoleRequest) (*emptypb.Empty, error) {
	// sys_user_roles.role_id 没有外键级联。先记下持有人，删除成功后再吊销。
	// 列举失败只记日志：角色已经要删，不能因为列不出持有人就把删除判失败，
	// 但这时也不知道该踢谁。
	holderIDs, listErr := s.listRoleHolderIDs(ctx, req.GetId())

	if err := s.roleRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	if listErr != nil {
		s.log.Errorf(ctx, "list role holders before delete failed for role [%d]: %v", req.GetId(), listErr)
	} else {
		s.revokeUsers(ctx, holderIDs)
	}

	return &emptypb.Empty{}, nil
}

// roleUpdateChangesGrant 比较更新前的角色和这次请求，判断仓库会不会改写授权。
// 调用点必须在受保护字段剥离之后、roleRepo.Update 改写 mask 之前。
//
// 只有同时满足三点才算授权变更：
//  1. 字段出现在 mask 里。mask 为 nil 时标量视为全部在列。空 mask 不是 nil：
//     Update 会先追加 updated_by，仓库只写审计字段。
//  2. 按 role_repo.Update 的写入条件，这次请求真的会写该字段。
//  3. 写入后的值和旧值不同。集合忽略顺序和重复。
//
// permissions / org_units / field_permissions 在 mask 为 nil 时仓库不替换。
// org_units 不在 mask 里时保留原集合（页面只在 SELECTED_UNITS 档提交该字段）。
// data_scope 为 nil 或 UNSPECIFIED 时回调跳过，不写。
// tenant_id 是不可变列，更新回调不写非空值；mask 把它置空时空值路径会 SET NULL，这才算变更。
func roleUpdateChangesGrant(old *permissionV1.Role, req *permissionV1.UpdateRoleRequest) bool {
	if old == nil || req.GetData() == nil {
		return false
	}
	data := req.GetData()
	mask := req.GetUpdateMask()
	return roleStatusWouldChange(old, data, mask) ||
		roleDataScopeWouldChange(old, data, mask) ||
		roleCodeWouldChange(old, data, mask) ||
		roleTypeWouldChange(old, data, mask) ||
		roleTenantWouldChange(old, data, mask) ||
		rolePermissionsWouldChange(old, data, mask) ||
		roleOrgUnitsWouldChange(old, data, mask) ||
		roleFieldPermissionsWouldChange(old, data, mask)
}

// roleMaskCovers 表示这一列会进入 UpdateOne 的标量写入。
// nil mask 不裁剪 DTO，回调里的非空标量都会写。非 nil mask 只认列出的路径；
// camelCase 在仓库里会被规范成 snake_case，这里用同一套转换。
func roleMaskCovers(mask *fieldmaskpb.FieldMask, protoName string) bool {
	if mask == nil {
		return true
	}
	for _, p := range mask.GetPaths() {
		if p == protoName || stringcase.ToSnakeCase(p) == protoName {
			return true
		}
	}
	return false
}

// roleAssocInMask 表示关联会被整体替换。mask 为 nil 时 role_repo.Update 不替换。
func roleAssocInMask(mask *fieldmaskpb.FieldMask, protoName string) bool {
	if mask == nil {
		return false
	}
	return roleMaskCovers(mask, protoName)
}

func roleStatusWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleMaskCovers(mask, "status") {
		return false
	}
	if data.Status == nil {
		// nil mask 不会走空值 SET NULL；非 nil mask 里出现该路径且值为空才会清空列。
		return mask != nil && old.Status != nil
	}
	if old.Status == nil {
		return true
	}
	return *old.Status != *data.Status
}

func roleDataScopeWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleMaskCovers(mask, "data_scope") {
		return false
	}
	// 回调遇到 nil 或 UNSPECIFIED 就跳过。UNSPECIFIED 是已设置的零值，不会 SET NULL。
	if data.DataScope == nil {
		return mask != nil && old.DataScope != nil
	}
	if *data.DataScope == identityV1.DataScope_DATA_SCOPE_UNSPECIFIED {
		return false
	}
	if old.DataScope == nil || *old.DataScope == identityV1.DataScope_DATA_SCOPE_UNSPECIFIED {
		return true
	}
	return *old.DataScope != *data.DataScope
}

func roleCodeWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleMaskCovers(mask, "code") {
		return false
	}
	if data.Code == nil {
		return mask != nil && old.Code != nil
	}
	return old.GetCode() != data.GetCode()
}

func roleTypeWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleMaskCovers(mask, "type") {
		return false
	}
	if data.Type == nil {
		return mask != nil && old.Type != nil
	}
	if old.Type == nil {
		return true
	}
	return *old.Type != *data.Type
}

func roleTenantWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	// 更新回调没有 SetNillableTenantID，列本身也是 Immutable，非空新值不会落库。
	if mask == nil || data.TenantId != nil || !roleMaskCovers(mask, "tenant_id") {
		return false
	}
	return old.TenantId != nil
}

func rolePermissionsWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleAssocInMask(mask, "permissions") {
		return false
	}
	return !grantIDSetEqual(old.GetPermissions(), data.GetPermissions())
}

func roleOrgUnitsWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleAssocInMask(mask, "org_units") {
		return false
	}
	return !grantIDSetEqual(old.GetOrgUnits(), data.GetOrgUnits())
}

func roleFieldPermissionsWouldChange(old, data *permissionV1.Role, mask *fieldmaskpb.FieldMask) bool {
	if !roleAssocInMask(mask, "field_permissions") {
		return false
	}
	return !fieldPermissionSetEqual(old.GetFieldPermissions(), data.GetFieldPermissions())
}

func grantIDSetEqual(a, b []uint32) bool {
	sa := grantIDSet(a)
	sb := grantIDSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for id := range sa {
		if _, ok := sb[id]; !ok {
			return false
		}
	}
	return true
}

func grantIDSet(ids []uint32) map[uint32]struct{} {
	set := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func fieldPermissionSetEqual(a, b []*permissionV1.RoleFieldPermission) bool {
	left := normalizeFieldPermissions(a)
	right := normalizeFieldPermissions(b)
	if len(left) != len(right) {
		return false
	}
	for resource, fields := range left {
		other, ok := right[resource]
		if !ok || len(other) != len(fields) {
			return false
		}
		for field := range fields {
			if _, ok := other[field]; !ok {
				return false
			}
		}
	}
	return true
}

// normalizeFieldPermissions 按 resource 归并。hiddenFields 当成集合，
// 空列表和该资源不出现是同一件事，重复资源的字段取并集。
func normalizeFieldPermissions(entries []*permissionV1.RoleFieldPermission) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		resource := entry.GetResource()
		if resource == "" {
			continue
		}
		for _, field := range entry.GetHiddenFields() {
			if field == "" {
				continue
			}
			if out[resource] == nil {
				out[resource] = make(map[string]struct{})
			}
			out[resource][field] = struct{}{}
		}
	}
	return out
}

func (s *RoleService) revokeRoleHolders(ctx context.Context, roleID uint32) {
	ids, err := s.listRoleHolderIDs(ctx, roleID)
	if err != nil {
		s.log.Errorf(ctx, "list role holders failed for role [%d]: %v", roleID, err)
		return
	}
	s.revokeUsers(ctx, ids)
}

func (s *RoleService) listRoleHolderIDs(ctx context.Context, roleID uint32) ([]uint32, error) {
	if roleID == 0 || s.userRoleRepo == nil {
		if s.userRoleRepo == nil {
			s.log.Errorf(ctx, "user role repo is nil, cannot list holders of role [%d]", roleID)
			return nil, adminV1.ErrorInternalServerError("user role repo is nil")
		}
		return nil, nil
	}
	return s.userRoleRepo.ListUserIDsByRoleIDs(ctx, []uint32{roleID}, false)
}

func (s *RoleService) revokeUsers(ctx context.Context, userIDs []uint32) {
	if len(userIDs) == 0 {
		return
	}
	if s.authenticator == nil {
		s.log.Errorf(ctx, "authenticator is nil, skip revoke for %d role holders", len(userIDs))
		return
	}
	seen := make(map[uint32]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		// 操作者自己持有这个角色时同样吊销。吊销失败只记日志。
		if rerr := s.authenticator.RevokeUserTokenAllClientTypes(ctx, id); rerr != nil {
			s.log.Errorf(ctx, "revoke tokens after role grant change failed for user [%d]: %v", id, rerr)
		}
	}
}

func (s *RoleService) GetRoleCodesByRoleIds(ctx context.Context, req *permissionV1.GetRoleCodesByRoleIdsRequest) (*permissionV1.GetRoleCodesByRoleIdsResponse, error) {
	ids, err := s.roleRepo.ListRoleCodesByRoleIds(ctx, req.GetRoleIds())
	if err != nil {
		return nil, err
	}

	return &permissionV1.GetRoleCodesByRoleIdsResponse{
		RoleCodes: ids,
	}, nil
}

func (s *RoleService) GetRolesByRoleCodes(ctx context.Context, req *permissionV1.GetRolesByRoleCodesRequest) (*permissionV1.ListRoleResponse, error) {
	roles, err := s.roleRepo.ListRolesByRoleCodes(ctx, req.GetRoleCodes())
	if err != nil {
		return nil, err
	}

	return &permissionV1.ListRoleResponse{
		Items: roles,
		Total: uint64(len(roles)),
	}, nil
}

func (s *RoleService) GetRolesByRoleIds(ctx context.Context, req *permissionV1.GetRolesByRoleIdsRequest) (*permissionV1.ListRoleResponse, error) {
	roles, err := s.roleRepo.ListRolesByRoleIds(ctx, req.GetRoleIds())
	if err != nil {
		return nil, err
	}

	return &permissionV1.ListRoleResponse{
		Items: roles,
		Total: uint64(len(roles)),
	}, nil
}

// createDefaultRoles 创建默认角色(包括超级管理员)
func (s *RoleService) createDefaultRoles(ctx context.Context) error {
	var err error

	for _, d := range constants.DefaultRoles {
		err = s.roleRepo.Create(ctx, &permissionV1.CreateRoleRequest{
			Data: d,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
