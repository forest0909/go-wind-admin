package service

import (
	"context"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-utils/fieldperm"
)

// fieldPermissionRoleResource 字段权限的资源名（proto 消息简单名）。
const fieldPermissionRoleResource = "Role"

// FieldPermissionRoleServiceServer 字段权限装饰器（Role 资源）：
// 依据令牌 hfs claim 对角色管理的读写两侧做字段裁剪，语义与 User 装饰器一致。
//
// 首个受控字段：permissions（角色的权限集）。受限角色看不到某角色绑定了哪些
// 权限、也无法增删该集合——权限集是权限体系的"元权限"，比单个业务字段更敏感。
type FieldPermissionRoleServiceServer struct {
	adminV1.UnsafeRoleServiceServer
	inner adminV1.RoleServiceHTTPServer
}

// NewFieldPermissionRoleServiceServer 包装角色管理服务。
func NewFieldPermissionRoleServiceServer(inner adminV1.RoleServiceHTTPServer) adminV1.RoleServiceServer {
	return &FieldPermissionRoleServiceServer{inner: inner}
}

// hiddenFields 取当前请求的 Role 资源隐藏字段集；取不到令牌或无配置返回 nil（不裁剪）。
func (s *FieldPermissionRoleServiceServer) hiddenFields(ctx context.Context) map[string]struct{} {
	tokenPayload, err := auth.FromContext(ctx)
	if err != nil || tokenPayload == nil {
		return nil
	}
	return fieldperm.HiddenFieldsOf(tokenPayload.GetHiddenFields(), fieldPermissionRoleResource)
}

func (s *FieldPermissionRoleServiceServer) List(ctx context.Context, in *paginationV1.PagingRequest) (*permissionV1.ListRoleResponse, error) {
	res, err := s.inner.List(ctx, in)
	if err != nil {
		return res, err
	}
	if hidden := s.hiddenFields(ctx); !fieldperm.IsEmpty(hidden) && res != nil {
		fieldperm.ApplyReadMaskList(res.GetItems(), hidden)
	}
	return res, nil
}

func (s *FieldPermissionRoleServiceServer) Get(ctx context.Context, in *permissionV1.GetRoleRequest) (*permissionV1.Role, error) {
	res, err := s.inner.Get(ctx, in)
	if err != nil {
		return res, err
	}
	fieldperm.ApplyReadMask(res, s.hiddenFields(ctx))
	return res, nil
}

func (s *FieldPermissionRoleServiceServer) Create(ctx context.Context, in *permissionV1.CreateRoleRequest) (*emptypb.Empty, error) {
	fieldperm.StripWriteFields(in.GetData(), nil, s.hiddenFields(ctx))
	return s.inner.Create(ctx, in)
}

func (s *FieldPermissionRoleServiceServer) Update(ctx context.Context, in *permissionV1.UpdateRoleRequest) (*emptypb.Empty, error) {
	fieldperm.StripWriteFields(in.GetData(), in.GetUpdateMask(), s.hiddenFields(ctx))
	return s.inner.Update(ctx, in)
}

func (s *FieldPermissionRoleServiceServer) Delete(ctx context.Context, in *permissionV1.DeleteRoleRequest) (*emptypb.Empty, error) {
	return s.inner.Delete(ctx, in)
}
