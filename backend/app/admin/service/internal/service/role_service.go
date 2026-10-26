package service

import (
	"context"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/aggregator"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

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

	// 保护角色字段不可修改
	if r.GetIsProtected() {
		if len(req.GetUpdateMask().Paths) > 0 {
			req.GetUpdateMask().Paths = sliceutil.FilterBlacklist(req.GetUpdateMask().Paths, []string{
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

	// 在 roleRepo.Update 改写 mask 之前判断。无 mask 视为全量更新。
	// 只改名称、描述、排序以及 updated_by 这类字段时不吊销。
	revokeHolders := roleUpdateChangesGrant(req)

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

// roleUpdateChangesGrant 判断这次更新会不会改变角色授予的权限、数据范围、字段权限或状态。
// 无 mask 或空 paths 视为全量更新。名称、描述、排序和审计字段不吊销。
// 调用点要在 roleRepo.Update 之前：仓库会从 mask 里拿掉 permissions 等关联路径。
func roleUpdateChangesGrant(req *permissionV1.UpdateRoleRequest) bool {
	fm := req.GetUpdateMask()
	if fm == nil || len(fm.GetPaths()) == 0 {
		return true
	}
	for _, p := range fm.GetPaths() {
		if !roleUpdatePathIsCosmetic(p) {
			return true
		}
	}
	return false
}

func roleUpdatePathIsCosmetic(path string) bool {
	switch path {
	case "name", "description", "sort_order", "sortOrder",
		"updated_by", "updatedBy",
		"created_by", "createdBy",
		"deleted_by", "deletedBy",
		"created_at", "updated_at", "deleted_at",
		"createdAt", "updatedAt", "deletedAt",
		"id", "tenant_name", "tenantName",
		"is_protected", "isProtected",
		"remark":
		return true
	default:
		return false
	}
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
