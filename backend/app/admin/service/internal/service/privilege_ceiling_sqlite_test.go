package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/pkg/constants"
)

func (e *userServiceEnv) seedRoleWithPerms(t *testing.T, code string, perms []uint32) uint32 {
	t.Helper()
	require.NoError(t, e.svc.roleRepo.Create(e.ctx, &permissionV1.CreateRoleRequest{Data: &permissionV1.Role{
		Name: trans.Ptr("上限 " + code), Code: trans.Ptr(code),
		Status: permissionV1.Role_ON.Enum(), Type: permissionV1.Role_SYSTEM.Enum(),
		Permissions: perms,
	}}))
	resp, err := e.svc.roleRepo.List(e.ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	require.NoError(t, err)
	for _, it := range resp.GetItems() {
		if it.GetCode() == code {
			return it.GetId()
		}
	}
	t.Fatalf("role %s not found", code)
	return 0
}

// a1 F4：持有 user:edit 的人不能把超出自己权限的角色分给自己或别人。
func TestUserServiceSqlite_PrivilegeCeilingOnRoleAssignment(t *testing.T) {
	e := newUserServiceForTest(t)
	hrRole := e.seedRoleWithPerms(t, "CEIL_HR", []uint32{11, 22})
	subRole := e.seedRoleWithPerms(t, "CEIL_SUB", []uint32{11})
	bigRole := e.seedRoleWithPerms(t, "CEIL_BIG", []uint32{11, 22, 33})
	// 超级管理员角色的特权来自角色码，本身不挂权限点
	superRole := e.seedRoleWithPerms(t, constants.PlatformAdminRoleCode, nil)

	const hr = uint32(9101)
	e.registerOperator(hr)
	e.stub.roleIDsByUser = map[uint32][]uint32{hr: {hrRole}}
	hrOp := opCtx(e.ctx, hr, 0, false)
	pw := trans.Ptr(encryptLoginPassword(t, constants.DefaultUserPassword))

	// Create：子集可以，超集和超级管理员角色不行
	_, err := e.svc.Create(hrOp, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ceil-a"), RoleIds: []uint32{subRole}}, Password: pw})
	require.NoError(t, err)
	_, err = e.svc.Create(hrOp, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ceil-b"), RoleIds: []uint32{bigRole}}, Password: pw})
	require.True(t, adminV1.IsForbidden(err), "超出操作人权限的角色应 403，got %v", err)
	_, err = e.svc.Create(hrOp, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ceil-c"), RoleIds: []uint32{superRole}}, Password: pw})
	require.True(t, adminV1.IsForbidden(err), "超级管理员角色只能由超级管理员分配，got %v", err)

	// Update：给自己加大角色 / 超级管理员角色都拒绝，且不落库
	before := len(e.stub.updates)
	e.stub.usersByID[hr] = &identityV1.User{Id: trans.Ptr(hr), Username: trans.Ptr("hr")}
	for _, rid := range []uint32{bigRole, superRole} {
		_, err = e.svc.Update(hrOp, &identityV1.UpdateUserRequest{
			Id: hr, Data: &identityV1.User{RoleIds: []uint32{hrRole, rid}},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"role_ids"}},
		})
		require.True(t, adminV1.IsForbidden(err), "自我抬权应 403，got %v", err)
	}
	require.Len(t, e.stub.updates, before, "被拒绝的更新不应写库")

	// 页面保存会带上目标用户原有的大角色：原样保留不算分配，只校验新加的
	const target = uint32(9102)
	e.stub.usersByID[target] = &identityV1.User{Id: trans.Ptr(target)}
	e.stub.roleIDsByUser[target] = []uint32{bigRole}
	_, err = e.svc.Update(hrOp, &identityV1.UpdateUserRequest{
		Id: target, Data: &identityV1.User{RoleIds: []uint32{bigRole, subRole}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"role_ids"}},
	})
	require.NoError(t, err)

	// 超级管理员不受限
	_, err = e.svc.Create(opCtx(e.ctx, hr, 0, true), &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr("ceil-d"), RoleIds: []uint32{bigRole, superRole}}, Password: pw})
	require.NoError(t, err)
}

// a1 F4：持有 role:edit 的人不能给角色勾自己没有的权限点，也不能用超级管理员角色码。
func TestRoleServiceSqlite_PrivilegeCeilingOnPermissions(t *testing.T) {
	e := newRoleGrantEnv(t)
	own := e.createFullRole(t, &permissionV1.Role{
		Name: trans.Ptr("操作人角色"), Code: trans.Ptr("CEIL_OWN"),
		Status: permissionV1.Role_ON.Enum(), Type: permissionV1.Role_TENANT.Enum(),
		Permissions: []uint32{11, 22}, TenantId: trans.Ptr(uint32(0)),
	})
	e.bindHolder(t, own)
	other := e.createFullRole(t, &permissionV1.Role{
		Name: trans.Ptr("别的角色"), Code: trans.Ptr("CEIL_OTHER"),
		Status: permissionV1.Role_ON.Enum(), Type: permissionV1.Role_TENANT.Enum(),
		Permissions: []uint32{44}, TenantId: trans.Ptr(uint32(0)),
	})
	op := opCtx(e.sys, roleGrantHolderID, 0, false)

	mk := func(code string, perms []uint32) *permissionV1.CreateRoleRequest {
		return &permissionV1.CreateRoleRequest{Data: &permissionV1.Role{
			Name: trans.Ptr(code), Code: trans.Ptr(code), Status: permissionV1.Role_ON.Enum(),
			Type: permissionV1.Role_TENANT.Enum(), Permissions: perms,
		}}
	}
	_, err := e.svc.Create(op, mk("CEIL_NEW_OK", []uint32{11}))
	require.NoError(t, err)
	_, err = e.svc.Create(op, mk("CEIL_NEW_BIG", []uint32{11, 33}))
	require.True(t, adminV1.IsForbidden(err), "got %v", err)
	_, err = e.svc.Create(op, mk(constants.PlatformAdminRoleCode, nil))
	require.True(t, adminV1.IsForbidden(err), "got %v", err)

	// 给自己的角色加权限点：拒绝
	_, err = e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: own, Data: &permissionV1.Role{Permissions: []uint32{11, 22, 33}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"permissions"}},
	})
	require.True(t, adminV1.IsForbidden(err), "got %v", err)
	require.ElementsMatch(t, []uint32{11, 22}, e.mustGetRole(t, own).GetPermissions())

	// 把角色码改成超级管理员：拒绝
	_, err = e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: own, Data: &permissionV1.Role{Code: trans.Ptr(constants.PlatformAdminRoleCode)},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"code"}},
	})
	require.True(t, adminV1.IsForbidden(err), "got %v", err)

	// 别的角色已有 44（操作人没有）：原样保留、再加 11 可以
	_, err = e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: other, Data: &permissionV1.Role{Permissions: []uint32{44, 11}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"permissions"}},
	})
	require.NoError(t, err)

	// 超级管理员不受限
	_, err = e.svc.Create(opCtx(e.sys, roleGrantHolderID, 0, true), mk("CEIL_SUPER", []uint32{99}))
	require.NoError(t, err)
}
