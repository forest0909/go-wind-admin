// PermissionService 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标：
//   - init() 播种链：权限组缺失时默认权限首条即中止（外键违约）；
//     权限组齐备时首条（无关联资源）落库、第二条（关联不存在的
//     api/menu）中止；非空表二次 init 不重复播种。
//   - syncWithOpenAPI 空路径分支（含既定空壳测试 TestPermissionService_syncWithOpenAPI_EmptyPaths）：
//     无 paths 键的文档 → "paths is nil" 内部错误且不落行；
//     空 paths 对象 → 同步空集成功；真实内嵌文档 → 接口表播种（SyncApis 全链路）。
//   - SyncPermissions 菜单派生 + 接口派生：目录菜单成组、子菜单成权限、
//     菜单路径一级段决定归属模块、接口代码前缀匹配模块后生成接口派生权限、
//     权限-组关联（GroupID）与权限-菜单/权限-接口关联行落库、未分类默认组恒建。
//   - List/Get 的租户范围限定：租户操作人无任何可见权限 → 空列表 / Forbidden；
//     平台操作人全量可见 + GroupName 富集（组存在时回填组名）。
//   - Create/Update/Delete：参数校验、操作人盖章、单字段掩码更新、删除清空。
//
// 跳过项：真实 Casbin 策略引擎（authorizer 一律 noop 引擎 + 桩 provider）、
// 跨实例策略重载广播。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/cmd/server/assets"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	entPermission "go-wind-admin/app/admin/service/internal/data/ent/permission"
	entPermissionApi "go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/utils/converter"
	crudViewer "go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/authorizer"
	goconv "go-wind-admin/third_party/go-utils/converter"
)

// newPermissionServiceForTest 白盒构造 PermissionService，逐字段对齐
// NewPermissionService 的装配（log 用 NopLogger helper；除 PermissionRepo 外
// 的 repo 均用 testkit 构造器；PermissionRepo 无 testkit 导出，按生产构造器 +
// NopLogger 的 bootstrap 上下文构造，唯一差异仍是日志；converter 与生产一致）。
// init() 不在构造时调用，由用例按需触发。
func newPermissionServiceForTest(t *testing.T) (*PermissionService, *ent.Client) {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	svc := &PermissionService{
		log:                     bLogger.NewHelper(bLogger.NopLogger()),
		permissionRepo:          data.NewPermissionRepoForTest(entClient),
		permissionGroupRepo:     data.NewPermissionGroupRepoForTest(entClient),
		menuRepo:                data.NewMenuRepoForTest(entClient),
		apiRepo:                 data.NewApiRepoForTest(entClient),
		roleRepo:                data.NewRoleRepoForTest(entClient),
		authorizer:              authorizer.NewAuthorizer(context.Background(), bLogger.NopLogger(), &authorizer.EngineConfig{Type: "noop"}, stubAuthzProvider{}),
		menuPermissionConverter: converter.NewMenuPermissionConverter(),
		apiPermissionConverter:  goconv.NewApiPermissionConverter().WithResourceAliases(apiResourceAliases),
	}
	return svc, entClient.Client()
}

// seedPermissionGroups 落 count 条占位权限组（自动 ID 从 1 顺次递增）。
func seedPermissionGroups(t *testing.T, svc *PermissionService, ctx context.Context, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		_, err := svc.permissionGroupRepo.Create(ctx, &permissionV1.CreatePermissionGroupRequest{
			Data: &permissionV1.PermissionGroup{
				Name:      trans.Ptr("PermSvcGroup"),
				Module:    trans.Ptr("perm_svc_group"),
				SortOrder: trans.Ptr(uint32(i)),
				Status:    permissionV1.PermissionGroup_ON.Enum(),
			},
		})
		require.NoError(t, err, "落占位权限组 %d 应成功", i)
	}
}

// TestPermissionService_InitSeeding 验证 init() 的默认权限播种：
// 空表播种全部默认权限（关联资源缺失不中止——关联表无外键强制、
// CleanNotExist 与批量插入对缺失资源静默无操作），非空表二次 init 不重复播种。
func TestPermissionService_InitSeeding(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())

	svc.init()
	cnt, err := client.Permission.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(constants.DefaultPermissions), int(cnt),
		"init() 空表应把全部默认权限落库")

	svc.init()
	cnt2, err := client.Permission.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, cnt, cnt2, "非空表二次 init 不应重复播种")
}

// TestPermissionService_InitSkipsSyncWhenNoApisOrMenus 验证 init() 的
// SyncPermissions 触发条件：api 表或菜单表为空时（此例两表皆空）
// 即使权限表为空播种后也不触发同步（menusCount==0 分支）。
func TestPermissionService_InitSkipsSyncWhenNoApisOrMenus(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	seedPermissionGroups(t, svc, ctx, 5)

	svc.init()

	groupCnt, err := client.PermissionGroup.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, groupCnt, "占位组应保持 5 条（SyncPermissions 未触发，未追加未分类组）")
}

// TestPermissionService_InitSyncsPermissions 启动期 init() 在菜单和接口都有数据时同步权限点：
// 没有登录用户也要能跑（以前 SyncPermissions 取不到操作人直接失败，错误被吞掉）。
func TestPermissionService_InitSyncsPermissions(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())

	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("BootCatalog"),
			Path:   trans.Ptr("/bootgrp"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
		},
	}))
	catalog, err := client.Menu.Query().Only(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:     trans.Ptr("BootWidgets"),
			Path:     trans.Ptr("widgets"),
			ParentId: &catalog.ID,
			Type:     permissionV1.Menu_MENU.Enum(),
			Status:   permissionV1.Menu_ON.Enum(),
		},
	}))
	require.NoError(t, svc.apiRepo.Create(ctx, &permissionV1.CreateApiRequest{
		Data: &permissionV1.Api{
			Path:   trans.Ptr("/admin/v1/widgets/{id}"),
			Method: trans.Ptr("PUT"),
			Scope:  permissionV1.Api_ADMIN.Enum(),
		},
	}))

	svc.init()

	for _, code := range []string{"widget:view", "widget:edit"} {
		exists, err := client.Permission.Query().Where(entPermission.CodeEQ(code)).Exist(ctx)
		require.NoError(t, err)
		require.True(t, exists, "启动期同步应生成 %s", code)
	}
}

// TestPermissionService_syncWithOpenAPI_EmptyPaths（原空壳测试的实装）：
// 无 paths 键的文档 → "paths is nil" 内部错误且接口表不落行；
// 空 paths 对象 → 同步空集成功不落行；真实内嵌文档 → SyncApis 播种接口表。
func TestPermissionService_syncWithOpenAPI_EmptyPaths(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	apiSvc := newApiServiceForTest(t, entClient)
	ctx := enttest.NewSystemContext(context.Background())

	orig := assets.OpenApiData
	defer func() { assets.OpenApiData = orig }()

	// 无 paths 键：加载成功但 Paths==nil → 内部错误
	assets.OpenApiData = []byte("openapi: 3.0.0\ninfo:\n  title: empty-paths-test\n  version: \"1.0\"\n")
	err := apiSvc.syncWithOpenAPI(ctx)
	require.Error(t, err, "无 paths 键的文档应触发 paths is nil 内部错误")
	require.True(t, adminV1.IsInternalServerError(err))
	require.ErrorContains(t, err, "paths is nil", "应命中 Paths==nil 分支")
	cnt, qerr := entClient.Client().Api.Query().Count(ctx)
	require.NoError(t, qerr)
	require.Zero(t, cnt, "失败分支不应落任何接口行")

	// 空 paths 对象：同步空集成功
	assets.OpenApiData = []byte("openapi: 3.0.0\ninfo:\n  title: empty-paths-test\n  version: \"1.0\"\npaths: {}\n")
	require.NoError(t, apiSvc.syncWithOpenAPI(ctx), "空 paths 对象应同步空集成功")
	cnt, qerr = entClient.Client().Api.Query().Count(ctx)
	require.NoError(t, qerr)
	require.Zero(t, cnt, "空集同步不应落行")

	// 真实内嵌文档：SyncApis 全链路播种接口表
	assets.OpenApiData = orig
	_, err = apiSvc.SyncApis(ctx, &emptypb.Empty{})
	require.NoError(t, err, "真实文档的 SyncApis 应成功")
	cnt, qerr = entClient.Client().Api.Query().Count(ctx)
	require.NoError(t, qerr)
	require.Greater(t, cnt, 0, "SyncApis 应从内嵌文档播种接口表")
	listResp, lerr := apiSvc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, lerr)
	require.Equal(t, uint64(cnt), listResp.GetTotal(), "List 应返回同步后的全量行")
	require.Len(t, listResp.GetItems(), cnt)
}

// TestPermissionService_SyncPermissions_MenuAndApiDerived 验证权限同步的
// 菜单派生与接口派生：目录菜单生成组、子菜单生成权限、一级路径段决定归属
// 模块、接口代码经前缀匹配模块后生成接口派生权限；关联行（权限-组、
// 权限-菜单、权限-接口）落库；未分类默认组恒建。
func TestPermissionService_SyncPermissions_MenuAndApiDerived(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	// 目录菜单（根）与页面菜单（子）
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("PermSvc 目录菜单"),
			Path:   trans.Ptr("/bizgrp"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
		},
	}))
	rootRows, err := client.Menu.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rootRows, 1)
	rootID := rootRows[0].ID
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:     trans.Ptr("PermSvc 子菜单"),
			Path:     trans.Ptr("childpg"),
			ParentId: &rootID,
			Type:     permissionV1.Menu_MENU.Enum(),
			Status:   permissionV1.Menu_ON.Enum(),
		},
	}))

	// 接口行：资源段与目录菜单一级段同前缀（状态缺省即 ON）
	require.NoError(t, svc.apiRepo.Create(ctx, &permissionV1.CreateApiRequest{
		Data: &permissionV1.Api{
			Path:   trans.Ptr("/api/v1/bizgrp/entries"),
			Method: trans.Ptr("GET"),
			Scope:  permissionV1.Api_ADMIN.Enum(),
		},
	}))

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err, "SyncPermissions 应成功")

	// 权限组：未分类默认组 + 目录菜单一级段派生组
	groups, err := client.PermissionGroup.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 2, "应恰好出现未分类组与目录菜单派生组")
	modules := map[string]uint32{}
	var uncategorizedName string
	for _, g := range groups {
		require.NotNil(t, g.Module)
		require.NotNil(t, g.Name)
		modules[*g.Module] = g.ID
		if *g.Module == "uncategorized" {
			uncategorizedName = *g.Name
		}
	}
	require.Len(t, modules, 2, "两个组的模块标识应互不相同")
	require.Equal(t, "未分类", uncategorizedName, "未分类组名应为固定文案")
	bizGroupID, hasBiz := modules["bizgrp"]
	require.True(t, hasBiz, "目录菜单一级段应派生 bizgrp 组")

	// 权限：菜单派生两条（目录/子页面）+ 接口派生一条，全部归组 bizgrp
	perms, err := client.Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, perms, 3, "应落两条菜单派生权限与一条接口派生权限")
	codes := map[string]bool{}
	for _, p := range perms {
		require.NotNil(t, p.Code, "派生权限应带代码")
		require.NotNil(t, p.GroupID, "派生权限应归组")
		require.Equal(t, bizGroupID, *p.GroupID, "派生权限应归入同模块组")
		codes[*p.Code] = true
	}
	require.True(t, codes["bizgrp:dir"], "目录菜单应派生 <一级段>:dir 代码权限（转换器钉死语义）")
	require.True(t, codes["childpg:view"], "子页面菜单应派生 <子段>:view 代码权限（首段丢弃语义）")

	// 关联行：权限-菜单两条、权限-接口一条（接口派生权限关联到接口行）
	menuLinks, err := client.PermissionMenu.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, menuLinks, "两条菜单派生权限应各建一条权限-菜单关联")
	apiLinks, err := client.PermissionApi.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, apiLinks, "接口派生权限应建一条权限-接口关联")
}

// TestPermissionService_SyncPermissions_DuplicateCodeDeduped 验证不同层级的菜单
// 推导出同一权限码时（如菜单同步 MERGE 后新旧路径形态并存），同步不撞 code
// 唯一约束整批失败，且同码只落一条、保留条目关联到来源菜单。
func TestPermissionService_SyncPermissions_DuplicateCodeDeduped(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	// 扁平旧菜单（id 小）：/profile 独段成码 profile
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("旧-个人中心"),
			Path:   trans.Ptr("/profile"),
			Type:   permissionV1.Menu_MENU.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
		},
	}))
	// 嵌套新菜单（id 大）：/opm 下 profile 子段，同码 profile
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("组织目录"),
			Path:   trans.Ptr("/opm"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
		},
	}))
	rows, err := client.Menu.Query().All(ctx)
	require.NoError(t, err)
	var catalogID uint32
	for _, r := range rows {
		if r.Path != nil && *r.Path == "/opm" {
			catalogID = r.ID
		}
	}
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:     trans.Ptr("新-个人中心"),
			Path:     trans.Ptr("profile"),
			ParentId: &catalogID,
			Type:     permissionV1.Menu_MENU.Enum(),
			Status:   permissionV1.Menu_ON.Enum(),
		},
	}))

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err, "重复权限码应被去重而不是整批插入失败")

	perms, err := client.Permission.Query().All(ctx)
	require.NoError(t, err)
	var matched []*ent.Permission
	for _, p := range perms {
		if p.Code != nil && *p.Code == "profile:view" {
			matched = append(matched, p)
		}
	}
	require.Len(t, matched, 1, "同码权限应只落一条")
	require.NotNil(t, matched[0].Name)
	menuLinks, err := client.PermissionMenu.Query().Count(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, menuLinks, 1, "保留的权限应关联到其来源菜单")
}

// TestPermissionService_SyncPermissions_MergeKeepsIDs 验证重复同步按编码增量合并：
// 已有权限点 ID 不变（角色绑定继续指向有效权限）、同步生成的旧英文名换成菜单标题、
// 手工改过的名称与手工新建的权限点保持不动。
func TestPermissionService_SyncPermissions_MergeKeepsIDs(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("MergeCatalog"),
			Path:   trans.Ptr("/mergegrp"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
			Meta:   &permissionV1.MenuMeta{Title: trans.Ptr("menu.mergegrp.moduleName")},
		},
	}))
	catalog, err := client.Menu.Query().Only(ctx)
	require.NoError(t, err)
	for _, m := range []struct{ name, path, title string }{
		{"MergeUsers", "users", "menu.mergegrp.users"},
		{"MergeRoles", "roles", "menu.mergegrp.roles"},
	} {
		require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
			Data: &permissionV1.Menu{
				Name:     trans.Ptr(m.name),
				Path:     trans.Ptr(m.path),
				ParentId: &catalog.ID,
				Type:     permissionV1.Menu_MENU.Enum(),
				Status:   permissionV1.Menu_ON.Enum(),
				Meta:     &permissionV1.MenuMeta{Title: trans.Ptr(m.title)},
			},
		}))
	}

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err)

	byCode := func() map[string]*ent.Permission {
		rows, qerr := client.Permission.Query().All(ctx)
		require.NoError(t, qerr)
		m := map[string]*ent.Permission{}
		for _, r := range rows {
			m[*r.Code] = r
		}
		return m
	}
	first := byCode()
	require.Equal(t, "menu.mergegrp.users", *first["user:view"].Name, "名称应取菜单标题")

	// 模拟旧版同步留下的英文名、管理员手改的名称、手工权限点和角色绑定
	_, err = client.Permission.UpdateOneID(first["user:view"].ID).SetName("MergeUsers").Save(ctx)
	require.NoError(t, err)
	_, err = client.Permission.UpdateOneID(first["role:view"].ID).SetName("角色页").Save(ctx)
	require.NoError(t, err)
	_, err = client.Permission.Create().SetName("手工权限").SetCode("manual:keep").Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(1).SetPermissionID(first["user:view"].ID).Save(ctx)
	require.NoError(t, err)
	// 多余的菜单关联：重复同步应删掉它
	_, err = client.PermissionMenu.Create().SetPermissionID(first["user:view"].ID).SetMenuID(9999).Save(ctx)
	require.NoError(t, err)

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err, "重复同步应成功")

	second := byCode()
	require.Len(t, second, len(first)+1, "不应新增或删除同步权限，手工权限保留")
	for code, p := range first {
		require.Equal(t, p.ID, second[code].ID, "权限 %s 的 ID 应保持不变", code)
	}
	require.Equal(t, "menu.mergegrp.users", *second["user:view"].Name, "旧英文名应改为菜单标题")
	require.Equal(t, "角色页", *second["role:view"].Name, "手工改过的名称应保留")
	require.Contains(t, second, "manual:keep", "手工权限点不应被删")

	bound, err := client.RolePermission.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, first["user:view"].ID, *bound.PermissionID, "角色绑定应仍指向原权限点")

	groups, err := client.PermissionGroup.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 2, "分组按模块合并，不应重复创建")
	menuLinks, err := client.PermissionMenu.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, menuLinks, "菜单关联应与菜单一一对应：多余的被删、不重复插入")
}

// TestPermissionService_SyncPermissions_ActionsAttachToPage 验证接口派生的操作权限点
// 挂到同资源页面下：名称为「页面标题#动作」、与页面同组；找不到页面的用「模块描述#动作」。
func TestPermissionService_SyncPermissions_ActionsAttachToPage(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("ActCatalog"),
			Path:   trans.Ptr("/actgrp"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
			Meta:   &permissionV1.MenuMeta{Title: trans.Ptr("menu.actgrp.moduleName")},
		},
	}))
	catalog, err := client.Menu.Query().Only(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:     trans.Ptr("ActWidgets"),
			Path:     trans.Ptr("widgets"),
			ParentId: &catalog.ID,
			Type:     permissionV1.Menu_MENU.Enum(),
			Status:   permissionV1.Menu_ON.Enum(),
			Meta:     &permissionV1.MenuMeta{Title: trans.Ptr("menu.actgrp.widgets")},
		},
	}))
	for _, api := range []struct{ method, path, desc string }{
		{"PUT", "/admin/v1/widgets/{id}", "小部件管理服务"},
		{"DELETE", "/admin/v1/gadgets/{id}", "小工具管理服务"},
	} {
		require.NoError(t, svc.apiRepo.Create(ctx, &permissionV1.CreateApiRequest{
			Data: &permissionV1.Api{
				Path:              trans.Ptr(api.path),
				Method:            trans.Ptr(api.method),
				ModuleDescription: trans.Ptr(api.desc),
				Scope:             permissionV1.Api_ADMIN.Enum(),
			},
		}))
	}

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err)

	rows, err := client.Permission.Query().All(ctx)
	require.NoError(t, err)
	byCode := map[string]*ent.Permission{}
	for _, r := range rows {
		byCode[*r.Code] = r
	}
	page, edit, orphan := byCode["widget:view"], byCode["widget:edit"], byCode["gadget:delete"]
	require.NotNil(t, page, "页面权限点应存在")
	require.NotNil(t, edit, "操作权限点应存在")
	require.NotNil(t, orphan, "无页面的操作权限点应存在")
	require.Equal(t, "menu.actgrp.widgets#edit", *edit.Name, "操作名称应为页面标题#动作")
	require.Equal(t, *page.GroupID, *edit.GroupID, "操作应与页面同组")
	require.Equal(t, "小工具管理#delete", *orphan.Name, "无页面时用去掉「服务」的模块描述#动作")
}

// TestPermissionService_SyncPermissions_InternalMessageSplit 站内消息的接口按别名拆成
// message/category/inbox，挂到各自页面下；旧 internal-message:* 上的角色授权转给新编码后旧点删除。
func TestPermissionService_SyncPermissions_InternalMessageSplit(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
		Data: &permissionV1.Menu{
			Name:   trans.Ptr("InternalMessageManagement"),
			Path:   trans.Ptr("/internal-message"),
			Type:   permissionV1.Menu_CATALOG.Enum(),
			Status: permissionV1.Menu_ON.Enum(),
		},
	}))
	catalog, err := client.Menu.Query().Only(ctx)
	require.NoError(t, err)
	for _, path := range []string{"messages", "categories", "inbox"} {
		require.NoError(t, svc.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{
			Data: &permissionV1.Menu{
				Name:     trans.Ptr(path),
				Path:     trans.Ptr(path),
				ParentId: &catalog.ID,
				Type:     permissionV1.Menu_MENU.Enum(),
				Status:   permissionV1.Menu_ON.Enum(),
			},
		}))
	}
	for _, api := range []struct{ method, path string }{
		{"GET", "/admin/v1/internal-message/messages"},
		{"PUT", "/admin/v1/internal-message/messages/{id}"},
		{"DELETE", "/admin/v1/internal-message/messages/{id}"},
		{"POST", "/admin/v1/internal-message/send"},
		{"POST", "/admin/v1/internal-message/revoke"},
		{"GET", "/admin/v1/internal-message/categories"},
		{"POST", "/admin/v1/internal-message/categories"},
		{"PUT", "/admin/v1/internal-message/categories/{id}"},
		{"DELETE", "/admin/v1/internal-message/categories/{id}"},
		{"GET", "/admin/v1/internal-message/inbox"},
		{"POST", "/admin/v1/internal-message/inbox/delete"},
		{"POST", "/admin/v1/internal-message/read"},
		{"POST", "/admin/v1/internal-message/status"},
	} {
		require.NoError(t, svc.apiRepo.Create(ctx, &permissionV1.CreateApiRequest{
			Data: &permissionV1.Api{
				Path:   trans.Ptr(api.path),
				Method: trans.Ptr(api.method),
				Scope:  permissionV1.Api_ADMIN.Enum(),
			},
		}))
	}

	// 旧版同步留下的合并编码，一个角色勾了其中的 edit
	oldEdit, err := client.Permission.Create().SetName("InternalMessageEdit").SetCode("internal-message:edit").Save(ctx)
	require.NoError(t, err)
	_, err = client.Permission.Create().SetName("InternalMessageView").SetCode("internal-message:view").Save(ctx)
	require.NoError(t, err)
	role, err := client.Role.Create().SetName("im").SetCode("im").Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(role.ID).SetPermissionID(oldEdit.ID).Save(ctx)
	require.NoError(t, err)

	_, err = svc.SyncPermissions(opCtx, &emptypb.Empty{})
	require.NoError(t, err)

	rows, err := client.Permission.Query().All(ctx)
	require.NoError(t, err)
	byCode := map[string]*ent.Permission{}
	for _, r := range rows {
		byCode[*r.Code] = r
	}
	for _, code := range []string{
		"message:view", "message:create", "message:edit", "message:delete",
		"category:view", "category:create", "category:edit", "category:delete",
		"inbox:view", "inbox:edit", "inbox:delete",
	} {
		require.NotNil(t, byCode[code], "应生成 %s", code)
	}
	for code := range byCode {
		require.NotContains(t, []string{"internal-message:view", "internal-message:create", "internal-message:edit", "internal-message:delete"}, code, "旧合并编码应被删除")
	}
	viewApis, err := client.PermissionApi.Query().Where(entPermissionApi.PermissionIDEQ(byCode["message:view"].ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, viewApis, "消息列表接口应挂到菜单派生的 message:view")

	grantedIDs, err := svc.roleRepo.ListPermissionIDsByRoleIDs(ctx, []uint32{role.ID})
	require.NoError(t, err)
	var granted []string
	for _, r := range rows {
		for _, id := range grantedIDs {
			if r.ID == id {
				granted = append(granted, *r.Code)
			}
		}
	}
	require.ElementsMatch(t, []string{"message:edit", "category:edit"}, granted, "旧授权应转给承接编码")
}

// TestPermissionService_ListAndGetTenantScoping 验证 List/Get 的租户范围限定
// 与平台侧全量可见 + 组名富集。
func TestPermissionService_ListAndGetTenantScoping(t *testing.T) {
	svc, _ := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	seedPermissionGroups(t, svc, ctx, 5)
	svc.init()
	platformCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})
	// 租户操作人：查看器须与令牌租户一致（生产由认证中间件成对注入）
	tenantCtx := auth.NewContext(
		crudViewer.WithContext(ctx, crudViewer.NewUserContext(8, 42, 0, "", nil)),
		&authenticationV1.UserTokenPayload{
			UserId: 8, TenantId: trans.Ptr(uint32(42)), Roles: []string{"perm_svc_nonexistent_role"},
		})

	// 平台操作人：全量可见 + 组名富集（组行存在时每条带组引用的权限都回填组名）
	listResp, err := svc.List(platformCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(constants.DefaultPermissions)), listResp.GetTotal(),
		"平台操作人应看到全部播种的默认权限")
	require.Len(t, listResp.GetItems(), len(constants.DefaultPermissions))
	for _, item := range listResp.GetItems() {
		require.Equal(t, "PermSvcGroup", item.GetGroupName(),
			"组存在时组名应经富集回填（占位组的名称）")
	}
	item := listResp.GetItems()[0]

	// 租户操作人（租户查看器 + 匹配令牌）：无任何可见权限 → 空列表
	tenantList, err := svc.List(tenantCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Zero(t, tenantList.GetTotal(), "租户操作人无可见权限时应返回空列表")
	require.Empty(t, tenantList.GetItems())

	// 平台查看器 + 租户令牌声明（查看器与令牌租户不一致）：
	// 按码查角色 ID 的租户闸门按查看器判定，拒绝平台上下文的按码查询
	mismatchCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{
		UserId: 8, TenantId: trans.Ptr(uint32(42)), Roles: []string{"perm_svc_nonexistent_role"},
	})
	_, err = svc.List(mismatchCtx, &paginationV1.PagingRequest{})
	require.Error(t, err, "平台查看器下的按码查角色 ID 应被租户闸门拒绝")
	require.True(t, permissionV1.IsBadRequest(err))

	// Get：平台命中；租户 Forbidden；不存在 ID 报错
	got, err := svc.Get(platformCtx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Id{Id: item.GetId()},
	})
	require.NoError(t, err, "平台操作人按已存在 ID 查询应命中")
	require.NotEmpty(t, got.GetGroupName(), "单条 Get 同样应做组名富集")

	_, err = svc.Get(tenantCtx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Id{Id: item.GetId()},
	})
	require.Error(t, err, "租户操作人访问范围外权限应 Forbidden")
	require.True(t, adminV1.IsForbidden(err))

	_, err = svc.Get(platformCtx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Id{Id: 9999999},
	})
	require.Error(t, err, "不存在的权限 ID 应报错")
}

// TestPermissionService_CreateUpdateDelete 验证权限 CRUD：参数校验、
// 操作人盖章、单字段掩码更新（掩码外保持原值）、删除清空。
func TestPermissionService_CreateUpdateDelete(t *testing.T) {
	svc, client := newPermissionServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &permissionV1.CreatePermissionRequest{})
	require.Error(t, err, "nil Data 应拒绝")
	require.True(t, adminV1.IsBadRequest(err))

	_, err = svc.Create(opCtx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name:        trans.Ptr("PermSvc 测试权限"),
			Code:        trans.Ptr("perm_svc_test_code"),
			Description: trans.Ptr("初版描述"),
			Status:      permissionV1.Permission_ON.Enum(),
		},
	})
	require.NoError(t, err, "创建应成功")
	rows, err := client.Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	rowID := rows[0].ID
	require.Equal(t, uint32(7), *rows[0].CreatedBy, "创建行应盖入操作人 ID")
	require.Equal(t, "perm_svc_test_code", *rows[0].Code, "代码应按请求落库")

	// 单字段掩码更新：description 更新，name 保持
	_, err = svc.Update(opCtx, &permissionV1.UpdatePermissionRequest{
		Id:         rowID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
		Data:       &permissionV1.Permission{Description: trans.Ptr("更新后的描述")},
	})
	require.NoError(t, err, "单字段掩码更新应成功")
	after, err := client.Permission.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "更新后的描述", *after.Description, "掩码内字段 description 应被更新")
	require.Equal(t, "PermSvc 测试权限", *after.Name, "掩码外字段 name 应保持原值")
	require.Equal(t, uint32(7), *after.UpdatedBy, "更新行应盖入操作人 ID")

	_, err = svc.Delete(ctx, &permissionV1.DeletePermissionRequest{
		QueryBy: &permissionV1.DeletePermissionRequest_Id{Id: rowID},
	})
	require.NoError(t, err, "删除应成功")
	cnt, err := client.Permission.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, cnt, "删除后权限表应清空")
}
