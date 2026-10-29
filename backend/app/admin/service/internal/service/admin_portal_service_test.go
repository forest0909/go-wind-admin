package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	entMenu "go-wind-admin/app/admin/service/internal/data/ent/menu"
	entPermission "go-wind-admin/app/admin/service/internal/data/ent/permission"
	entRole "go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"go-wind-admin/app/admin/service/cmd/server/assets"
)

func TestMenuListToQueryString(t *testing.T) {
	type args struct {
		menus []uint32
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Single menu ID",
			args: args{menus: []uint32{1}},
			want: `{"id__in":"[\"1\"]","status":"ON","type__not":"BUTTON"}`,
		},
		{
			name: "Multiple menu IDs",
			args: args{menus: []uint32{1, 2, 3}},
			want: `{"id__in":"[\"1\", \"2\", \"3\"]","status":"ON","type__not":"BUTTON"}`,
		},
		{
			name: "No menu IDs",
			args: args{menus: []uint32{}},
			want: `{"id__in":"[]","status":"ON","type__not":"BUTTON"}`,
		},
		{
			name: "Large menu IDs",
			args: args{menus: []uint32{1234567890, 987654321}},
			want: `{"id__in":"[\"1234567890\", \"987654321\"]","status":"ON","type__not":"BUTTON"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AdminPortalService{}
			if got := s.menuListToQueryString(tt.args.menus, false); got != tt.want {
				t.Errorf("menuListToQueryString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenAPI(t *testing.T) {
	// 加载 OpenAPI V3 文档
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(assets.OpenApiData)
	if err != nil {
		t.Fatalf("加载 OpenAPI 文档失败: %v", err)
	}

	if doc == nil {
		t.Fatal("OpenAPI 文档为空")
	}
	if doc.Paths == nil {
		t.Fatal("OpenAPI 文档的路径为空")
	}

	// 遍历所有路径和操作
	for path, pathItem := range doc.Paths.Map() {
		fmt.Printf("路径: %s\n", path)
		for method, operation := range pathItem.Operations() {
			fmt.Printf("  方法: %s\n", method)
			if operation.Summary != "" {
				fmt.Printf("    摘要: %s\n", operation.Summary)
			}
			if operation.Description != "" {
				fmt.Printf("    描述: %s\n", operation.Description)
			}
			if len(operation.Tags) > 0 {
				tag := doc.Tags.Get(operation.Tags[0])
				fmt.Printf("    服务: %s %s\n", tag.Name, tag.Description)
			}
		}
	}
}

// TestGetMyPermissionCode 平台管理员取全量启用编码，普通用户只取启用角色绑定编码。
func TestGetMyPermissionCode(t *testing.T) {
	ec := enttest.NewEntClientForTest(t)
	client := ec.Client()
	ctx := enttest.NewSystemContext(context.Background())
	bound, err := client.Permission.Create().SetName("bound").SetCode("user:edit").SetStatus(entPermission.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Permission.Create().SetName("unbound").SetCode("role:create").SetStatus(entPermission.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Permission.Create().SetName("disabled").SetCode("user:delete").SetStatus(entPermission.StatusOff).Save(ctx)
	require.NoError(t, err)
	role, err := client.Role.Create().SetName("ordinary").SetCode("ordinary").SetStatus(entRole.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(bound.ID).Save(ctx)
	require.NoError(t, err)
	svc := &AdminPortalService{
		permissionRepo: data.NewPermissionRepoForTest(ec),
		roleRepo:       data.NewRoleRepoForTest(ec),
		userRepo:       &userServiceUserRepoStub{usersByID: map[uint32]*identityV1.User{7: {RoleIds: []uint32{role.ID}}}},
	}
	for _, platform := range []bool{true, false} {
		t.Run(fmt.Sprintf("platform=%v", platform), func(t *testing.T) {
			opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7, IsPlatformAdmin: trans.Ptr(platform), HiddenFields: []string{"User.email"}})
			resp, err := svc.GetMyPermissionCode(opCtx, &emptypb.Empty{})
			require.NoError(t, err)
			expected := []string{"user:edit"}
			if platform {
				expected = append(expected, "role:create")
			}
			require.ElementsMatch(t, expected, resp.Codes)
			require.Equal(t, []string{"User.email"}, resp.HiddenFields)
		})
	}
}

// TestGetNavigation 验证平台全量与普通角色祖先补全 / Platform and ancestor grants.
func TestGetNavigation(t *testing.T) {
	ec := enttest.NewEntClientForTest(t)
	client := ec.Client()
	ctx := enttest.NewSystemContext(context.Background())
	root, err := client.Menu.Create().SetName("root").SetPath("/root").SetType(entMenu.TypeCatalog).SetStatus(entMenu.StatusOn).Save(ctx)
	require.NoError(t, err)
	catalog, err := client.Menu.Create().SetName("catalog").SetPath("catalog").SetParentID(root.ID).SetType(entMenu.TypeCatalog).SetStatus(entMenu.StatusOn).Save(ctx)
	require.NoError(t, err)
	page, err := client.Menu.Create().SetName("page").SetPath("page").SetParentID(catalog.ID).SetType(entMenu.TypeMenu).SetStatus(entMenu.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Menu.Create().SetName("sibling").SetPath("sibling").SetParentID(catalog.ID).SetType(entMenu.TypeMenu).SetStatus(entMenu.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Menu.Create().SetName("button").SetPath("button").SetParentID(catalog.ID).SetType(entMenu.TypeButton).SetStatus(entMenu.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.Menu.Create().SetName("disabled").SetPath("disabled").SetParentID(catalog.ID).SetType(entMenu.TypeMenu).SetStatus(entMenu.StatusOff).Save(ctx)
	require.NoError(t, err)
	permission, err := client.Permission.Create().SetName("view").SetCode("page:view").SetStatus(entPermission.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.PermissionMenu.Create().SetPermissionID(permission.ID).SetMenuID(page.ID).Save(ctx)
	require.NoError(t, err)
	role, err := client.Role.Create().SetName("reader").SetCode("reader").SetStatus(entRole.StatusOn).Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(permission.ID).Save(ctx)
	require.NoError(t, err)
	svc := &AdminPortalService{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		menuRepo: data.NewMenuRepoForTest(ec), roleRepo: data.NewRoleRepoForTest(ec),
		userRepo: &userServiceUserRepoStub{usersByID: map[uint32]*identityV1.User{7: {RoleIds: []uint32{role.ID}}, 8: {}}},
	}
	for _, tc := range []struct {
		name     string
		uid      uint32
		platform bool
		want     []string
	}{
		{"platform", 99, true, []string{"root", "catalog", "page", "sibling"}},
		{"child-only", 7, false, []string{"root", "catalog", "page"}},
		{"no-grants", 8, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: tc.uid, IsPlatformAdmin: trans.Ptr(tc.platform)})
			resp, err := svc.GetNavigation(opCtx, &emptypb.Empty{})
			require.NoError(t, err)
			var names []string
			var visit func([]*permissionV1.MenuRouteItem)
			visit = func(items []*permissionV1.MenuRouteItem) {
				for _, m := range items {
					names = append(names, m.GetName())
					visit(m.Children)
				}
			}
			visit(resp.Items)
			require.ElementsMatch(t, tc.want, names)
			if tc.uid == 7 {
				require.Equal(t, "catalog", resp.Items[0].Children[0].GetName())
			}
		})
	}
}
