package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"go-wind-admin/third_party/go-utils/aggregator"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/stringcase"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/utils/converter"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/authorizer"
	goconv "go-wind-admin/third_party/go-utils/converter"
)

type PermissionService struct {
	adminV1.PermissionServiceHTTPServer

	log *bLogger.Helper

	permissionRepo      *data.PermissionRepo
	permissionGroupRepo *data.PermissionGroupRepo

	menuRepo *data.MenuRepo
	apiRepo  *data.ApiRepo

	roleRepo *data.RoleRepo

	authorizer *authorizer.Authorizer

	menuPermissionConverter *converter.MenuPermissionConverter
	apiPermissionConverter  *goconv.ApiPermissionConverter
}

func NewPermissionService(
	ctx *bootstrap.Context,
	permissionRepo *data.PermissionRepo,
	permissionGroupRepo *data.PermissionGroupRepo,
	menuRepo *data.MenuRepo,
	apiRepo *data.ApiRepo,
	roleRepo *data.RoleRepo,
	authorizer *authorizer.Authorizer,
) *PermissionService {
	svc := &PermissionService{
		log:                     ctx.NewLoggerHelper("permission/service/admin-service"),
		permissionRepo:          permissionRepo,
		permissionGroupRepo:     permissionGroupRepo,
		menuRepo:                menuRepo,
		apiRepo:                 apiRepo,
		roleRepo:                roleRepo,
		authorizer:              authorizer,
		menuPermissionConverter: converter.NewMenuPermissionConverter(),
		apiPermissionConverter:  goconv.NewApiPermissionConverter().WithResourceAliases(apiResourceAliases),
	}

	svc.init()

	return svc
}

func (s *PermissionService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.permissionRepo.Count(ctx, nil); count.Count == 0 {
		_ = s.createDefaultPermissions(ctx)
	}

	// 每次启动都按当前菜单和接口同步一次权限点，部署后不必再手点同步。
	// 同步按编码合并、不删手工数据，权限点没变化时只有查询。
	// SyncPermissions 要从令牌取操作人，启动期没有登录用户，用 0 代表系统。
	apiCount, _ := s.apiRepo.Count(ctx, nil)
	menusCount, _ := s.menuRepo.Count(ctx, []func(s *sql.Selector){})
	if apiCount.Count > 0 && menusCount > 0 {
		sysCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 0})
		if _, err := s.SyncPermissions(sysCtx, &emptypb.Empty{}); err != nil {
			s.log.Errorf(ctx, "启动期权限点同步失败: %v", err)
		}
	}
}

func (s *PermissionService) extractRelationIDs(
	permissions []*permissionV1.Permission,
	groupSet aggregator.ResourceMap[uint32, *permissionV1.PermissionGroup],
) {
	for _, p := range permissions {
		if p.GetGroupId() > 0 {
			groupSet[p.GetGroupId()] = nil
		}
	}
}

func (s *PermissionService) fetchRelationInfo(
	ctx context.Context,
	groupSet aggregator.ResourceMap[uint32, *permissionV1.PermissionGroup],
) error {
	if len(groupSet) > 0 {
		groupIds := make([]uint32, 0, len(groupSet))
		for id := range groupSet {
			groupIds = append(groupIds, id)
		}

		groups, err := s.permissionGroupRepo.ListByIDs(ctx, groupIds)
		if err != nil {
			s.log.Errorf(context.Background(), "query permission group err: %v", err)
			return err
		}

		for _, g := range groups {
			groupSet[g.GetId()] = g
		}
	}

	return nil
}

func (s *PermissionService) bindRelations(
	permissions []*permissionV1.Permission,
	groupSet aggregator.ResourceMap[uint32, *permissionV1.PermissionGroup],
) {
	aggregator.Populate(
		permissions,
		groupSet,
		func(ou *permissionV1.Permission) uint32 { return ou.GetGroupId() },
		func(ou *permissionV1.Permission, g *permissionV1.PermissionGroup) {
			ou.GroupName = g.Name
		},
	)
}

func (s *PermissionService) enrichRelations(ctx context.Context, permissions []*permissionV1.Permission) error {
	var groupSet = make(aggregator.ResourceMap[uint32, *permissionV1.PermissionGroup])
	s.extractRelationIDs(permissions, groupSet)
	if err := s.fetchRelationInfo(ctx, groupSet); err != nil {
		return err
	}
	s.bindRelations(permissions, groupSet)
	return nil
}

func (s *PermissionService) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListPermissionResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	var limitPermissionIDs []uint32
	if operator.GetTenantId() > 0 {
		limitPermissionIDs, err = s.roleRepo.ListPermissionIDsByRoleCodes(ctx, operator.GetRoles())
		if err != nil {
			return nil, err
		}

		// 没有任何 permission 可访问，直接返回空列表
		if len(limitPermissionIDs) == 0 {
			return &permissionV1.ListPermissionResponse{
				Items: []*permissionV1.Permission{},
				Total: 0,
			}, nil
		}
	}

	resp, err := s.permissionRepo.List(ctx, req, limitPermissionIDs)
	if err != nil {
		return nil, err
	}

	_ = s.enrichRelations(ctx, resp.Items)

	return resp, nil
}

func (s *PermissionService) Count(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.CountPermissionResponse, error) {
	return s.permissionRepo.Count(ctx, req)
}

func (s *PermissionService) Get(ctx context.Context, req *permissionV1.GetPermissionRequest) (*permissionV1.Permission, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := s.permissionRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	if operator.GetTenantId() > 0 {
		permissionIDs, err := s.roleRepo.ListPermissionIDsByRoleCodes(ctx, operator.GetRoles())
		if err != nil {
			return nil, err
		}

		found := false
		for _, pid := range permissionIDs {
			if pid == resp.GetId() {
				found = true
				break
			}
		}
		if !found {
			return nil, adminV1.ErrorForbidden("no access to the permission")
		}
	}

	fakeItems := []*permissionV1.Permission{resp}
	_ = s.enrichRelations(ctx, fakeItems)

	return resp, nil
}

func (s *PermissionService) Create(ctx context.Context, req *permissionV1.CreatePermissionRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.permissionRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PermissionService) Update(ctx context.Context, req *permissionV1.UpdatePermissionRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.permissionRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PermissionService) Delete(ctx context.Context, req *permissionV1.DeletePermissionRequest) (*emptypb.Empty, error) {
	if err := s.permissionRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// appendAPis 为权限追加对应的 API 资源 ID 列表
func (s *PermissionService) appendAPis(
	ctx context.Context,
	permissions *[]*permissionV1.Permission,
	mapPermissions *map[string][]*permissionV1.Permission,
	operatorUserId uint32,
) error {
	// 查询所有启用的 API 资源
	apis, err := s.apiRepo.List(ctx, &paginationV1.PagingRequest{
		NoPaging: trans.Ptr(true),
		FilteringType: &paginationV1.PagingRequest_Query{
			Query: `{"status":"ON"}`,
		},
		OrderBy: trans.Ptr("operation"),
	})
	if err != nil {
		return err
	}

	sort.SliceStable(apis.Items, func(i, j int) bool {
		a, b := apis.Items[i], apis.Items[j]
		if a.GetModule() != b.GetModule() {
			return a.GetModule() < b.GetModule()
		}
		if a.GetPath() != b.GetPath() {
			return a.GetPath() > b.GetPath()
		}
		return a.GetOperation() > b.GetOperation()
	})

	type moduleApis struct {
		module      string
		description string
		apis        []uint32
	}

	codes := make(map[string]*moduleApis)
	for _, api := range apis.Items {
		//code := s.apiPermissionConverter.ConvertCodeByOperationID(api.GetOperation())
		code := s.apiPermissionConverter.ConvertCodeByPath(api.GetMethod(), api.GetPath())
		if code == "" {
			continue
		}

		s.log.Debugf(ctx, "appendAPis: processing api [%s] [%s] with code [%s]", api.GetMethod(), api.GetPath(), code)

		if curCode, exist := codes[code]; !exist {
			var module string
			for k, perms := range *mapPermissions {
				if len(perms) == 0 {
					continue
				}

				for _, perm := range perms {
					code1Prefix := strings.Split(perm.GetCode(), ":")[0]
					code2Prefix := strings.Split(code, ":")[0]
					if strings.HasPrefix(code2Prefix, code1Prefix) {
						module = k
						break
					}
				}
			}

			if module == "" {
				module = constants.UncategorizedPermissionGroup
			}

			codes[code] = &moduleApis{
				module:      module,
				description: api.GetModuleDescription(),
				apis:        []uint32{api.GetId()},
			}
		} else {
			curCode.apis = append(curCode.apis, api.GetId())
		}
	}

	for _, perm := range *permissions {
		permIds, exist := codes[perm.GetCode()]
		if exist {
			perm.ApiIds = append(perm.ApiIds, permIds.apis...)
			delete(codes, perm.GetCode())
		}
	}

	//s.log.Debugf(ctx, "appendAPis: unmatched permission codes: %v", codes)

	for code, apiIDs := range codes {
		name := legacyApiPermissionName(code)
		// 有业务模块描述时用「模块描述#动作」，前端把动作翻成中文显示
		if desc := strings.TrimSuffix(strings.TrimSpace(apiIDs.description), "服务"); desc != "" {
			if _, action, ok := splitPermissionCode(code); ok {
				name = desc + permissionActionSep + action
			}
		}

		perm := &permissionV1.Permission{
			Name:      trans.Ptr(name),
			Code:      trans.Ptr(code),
			Status:    trans.Ptr(permissionV1.Permission_ON),
			ApiIds:    apiIDs.apis,
			CreatedBy: trans.Ptr(operatorUserId),
			UpdatedBy: trans.Ptr(operatorUserId),
		}

		*permissions = append(*permissions, perm)

		(*mapPermissions)[apiIDs.module] = append((*mapPermissions)[apiIDs.module], perm)

		//s.log.Debugf(ctx, "appendAPis: create permission for api code: [%s][%s]", apiIDs.module, code)
	}

	return nil
}

// menuPathToModuleName 从菜单路径中提取模块名称
func menuPathToModuleName(menuPath string) string {
	var module string
	pathParts := strings.Split(menuPath, "/")
	if len(pathParts) > 1 {
		module = strings.TrimSpace(pathParts[1])
	}
	if module == "" {
		module = constants.DefaultBizPermissionModule
	}
	return module
}

// apiResourceAliases 带模块前缀的接口路径 → 资源（或完整编码）。
// 默认规则只取路径第一段，站内消息的接口全挂在 internal-message/ 下，
// 会把消息、分类、收件箱合成一个 internal-message 资源，对不上菜单派生的 message/category/inbox 页面。
var apiResourceAliases = map[string]string{
	"internal-message/messages":     "message",
	"internal-message/send":         "message:create",
	"internal-message/revoke":       "message:edit",
	"internal-message/categories":   "category",
	"internal-message/inbox":        "inbox",
	"internal-message/inbox/delete": "inbox:delete",
	"internal-message/read":         "inbox:edit",
	"internal-message/status":       "inbox:edit",
}

// retiredPermissionCodes 接口编码规则调整后不再生成的旧编码 → 承接它的新编码。
// 同步时把旧编码上的角色授权转给新编码，再删掉旧权限点；新编码覆盖旧编码原有的全部接口，
// 已有角色不会因此少权限。
var retiredPermissionCodes = map[string][]string{
	"internal-message:view":   {"message:view", "category:view", "inbox:view"},
	"internal-message:create": {"message:create", "message:edit", "category:create", "inbox:edit", "inbox:delete"},
	"internal-message:edit":   {"message:edit", "category:edit"},
	"internal-message:delete": {"message:delete", "category:delete"},
}

// syncPermissionsTimeout 同步权限的服务端执行上限，与调用方连接解耦
const syncPermissionsTimeout = 5 * time.Minute

// permissionActionSep 操作类权限点名称里「页面/模块名」和「动作」的分隔符，
// 如 menu.permission.menu#edit；前端据此显示成「菜单管理 · 编辑」
const permissionActionSep = "#"

// splitPermissionCode 把 menu:edit 拆成资源 menu 和动作 edit（按最后一个冒号）
func splitPermissionCode(code string) (resource, action string, ok bool) {
	i := strings.LastIndex(code, ":")
	if i <= 0 || i == len(code)-1 {
		return "", "", false
	}
	return code[:i], code[i+1:], true
}

// legacyApiPermissionName 旧版同步给接口派生权限点起的名称（编码转驼峰，如 MenuEdit）
func legacyApiPermissionName(code string) string {
	return stringcase.ToPascalCase(strings.ReplaceAll(code, ":", "_"))
}

// attachActionsToPages 把接口派生的操作权限点（如 menu:edit）挂到同资源的页面权限点（menu:view）下：
// 名称改成「页面标题#动作」，并归入页面所在的权限组，角色页据此按「页面 → 操作」显示。
func attachActionsToPages(
	permissions []*permissionV1.Permission,
	mapPermissions map[string][]*permissionV1.Permission,
	pageModules map[string]string,
) {
	pages := make(map[string]*permissionV1.Permission)
	for _, p := range permissions {
		if len(p.GetMenuIds()) > 0 {
			pages[p.GetCode()] = p
		}
	}

	moved := make(map[*permissionV1.Permission]string)
	for _, p := range permissions {
		if len(p.GetMenuIds()) > 0 {
			continue
		}
		resource, action, ok := splitPermissionCode(p.GetCode())
		if !ok {
			continue
		}
		pageCode := resource + ":view"
		page, ok := pages[pageCode]
		if !ok {
			continue
		}
		p.Name = trans.Ptr(page.GetName() + permissionActionSep + action)
		moved[p] = pageModules[pageCode]
	}
	if len(moved) == 0 {
		return
	}

	for module, perms := range mapPermissions {
		kept := perms[:0]
		for _, p := range perms {
			if _, ok := moved[p]; !ok {
				kept = append(kept, p)
			}
		}
		mapPermissions[module] = kept
	}
	for p, module := range moved {
		mapPermissions[module] = append(mapPermissions[module], p)
	}
}

// menuDisplayName 同步出的权限/分组名称：优先取菜单标题（i18n key，前端按 routes 命名空间翻译），
// 没有标题再退回菜单内部名。
func menuDisplayName(menu *permissionV1.Menu) string {
	if title := strings.TrimSpace(menu.GetMeta().GetTitle()); title != "" {
		return title
	}
	return menu.GetName()
}

// SyncPermissions 同步权限点
//
// 按权限编码增量合并：已有的权限点原地更新（ID 不变，角色上的勾选因此保留），缺的才新建；
// 分组按模块标识合并。不删除任何权限点——手工新建的权限点不受影响，
// 菜单删掉后遗留的权限点需要在权限页手动删除。
func (s *PermissionService) SyncPermissions(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// 同步是幂等的整批事务：调用方断开（如前端超时）不应把写到一半的事务掐掉，
	// 改为独立的超时上限
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), syncPermissionsTimeout)
	defer cancel()

	// 查询所有启用的菜单
	menus, err := s.menuRepo.List(ctx, &paginationV1.PagingRequest{
		NoPaging: trans.Ptr(true),
		FilteringType: &paginationV1.PagingRequest_Query{
			Query: `{"status":"ON"}`,
		},
		OrderBy: trans.Ptr("id desc"),
	}, false)
	if err != nil {
		return nil, err
	}

	s.menuPermissionConverter.ComposeMenuPaths(menus.Items)

	sort.SliceStable(menus.Items, func(i, j int) bool {
		return menus.Items[i].GetParentId() < menus.Items[j].GetParentId()
	})

	var permissionGroups []*permissionV1.PermissionGroup
	var permissions []*permissionV1.Permission
	var mapPermissions = make(map[string][]*permissionV1.Permission)
	// 旧版同步用菜单内部名做名称，记下来用于判断已有条目能否改名
	var legacyGroupNames = make(map[string]string)
	var legacyPermissionNames = make(map[string]string)
	// 菜单派生权限点所在的模块，接口派生的操作权限点据此归入页面所在的组
	var pageModules = make(map[string]string)

	// 不同层级的菜单可能推导出同一个权限码（如菜单同步 MERGE 后新旧路径形态
	// 并存），code 唯一约束会让整批插入失败；同码只保留首个，后续跳过
	var seenPermissionCodes = make(map[string]struct{})

	permissionGroups = append(permissionGroups, &permissionV1.PermissionGroup{
		Name:      trans.Ptr("未分类"),
		Module:    trans.Ptr(constants.UncategorizedPermissionGroup),
		Status:    trans.Ptr(permissionV1.PermissionGroup_ON),
		SortOrder: trans.Ptr(uint32(len(permissionGroups) + 1)),
		CreatedBy: trans.Ptr(operator.UserId),
		UpdatedBy: trans.Ptr(operator.UserId),
	})

	for _, menu := range menus.Items {
		// 权限码仍按菜单内部名推导，保证与已有数据的编码一致
		permissionCode := s.menuPermissionConverter.ConvertCode(menu.GetPath(), menu.GetName(), menu.GetType())
		if permissionCode == "" {
			continue
		}
		if _, exists := seenPermissionCodes[permissionCode]; exists {
			continue
		}
		seenPermissionCodes[permissionCode] = struct{}{}

		name := menuDisplayName(menu)
		module := menuPathToModuleName(menu.GetPath())

		// 以目录类型的菜单作为权限组；同模块只取第一个目录
		if menu.GetType() == permissionV1.Menu_CATALOG {
			if _, exists := legacyGroupNames[module]; !exists {
				legacyGroupNames[module] = menu.GetName()
				permissionGroups = append(permissionGroups, &permissionV1.PermissionGroup{
					Name:      trans.Ptr(name),
					Module:    trans.Ptr(module),
					Status:    trans.Ptr(permissionV1.PermissionGroup_ON),
					SortOrder: trans.Ptr(uint32(len(permissionGroups) + 1)),
					CreatedBy: trans.Ptr(operator.UserId),
					UpdatedBy: trans.Ptr(operator.UserId),
				})
			}
		}

		perm := &permissionV1.Permission{
			Name:      trans.Ptr(name),
			Code:      trans.Ptr(permissionCode),
			Status:    trans.Ptr(permissionV1.Permission_ON),
			MenuIds:   []uint32{menu.GetId()},
			CreatedBy: trans.Ptr(operator.UserId),
			UpdatedBy: trans.Ptr(operator.UserId),
		}
		legacyPermissionNames[permissionCode] = menu.GetName()
		pageModules[permissionCode] = module

		permissions = append(permissions, perm)
		mapPermissions[module] = append(mapPermissions[module], perm)
	}

	// 为权限追加对应的 API 资源 ID 列表
	_ = s.appendAPis(ctx, &permissions, &mapPermissions, operator.UserId)
	attachActionsToPages(permissions, mapPermissions, pageModules)

	groupIDs, err := s.mergeSyncedGroups(ctx, permissionGroups, legacyGroupNames, operator.UserId)
	if err != nil {
		return nil, err
	}

	// 为权限分配权限组 ID
	for module, curPers := range mapPermissions {
		groupID, ok := groupIDs[module]
		if !ok {
			continue
		}
		for _, p := range curPers {
			p.GroupId = trans.Ptr(groupID)
		}
	}

	synced := make([]data.SyncedPermission, 0, len(permissions))
	for _, p := range permissions {
		// 接口派生的权限点没有菜单来源，旧名是编码转驼峰
		legacy, ok := legacyPermissionNames[p.GetCode()]
		if !ok {
			legacy = legacyApiPermissionName(p.GetCode())
		}
		synced = append(synced, data.SyncedPermission{Data: p, LegacyName: legacy})
	}
	if err = s.permissionRepo.MergeSynced(ctx, synced, operator.UserId); err != nil {
		return nil, err
	}
	if err = s.permissionRepo.RetireCodes(ctx, retiredPermissionCodes, operator.UserId); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// mergeSyncedGroups 按模块标识合并同步出的权限组，返回 模块标识 → 组 ID。
// 系统内置组（sys 模块）不参与合并。
func (s *PermissionService) mergeSyncedGroups(
	ctx context.Context,
	groups []*permissionV1.PermissionGroup,
	legacyNames map[string]string,
	operatorUserId uint32,
) (map[string]uint32, error) {
	existing, err := s.permissionGroupRepo.List(ctx, &paginationV1.PagingRequest{
		NoPaging: trans.Ptr(true),
		OrderBy:  trans.Ptr("id"),
	}, false)
	if err != nil {
		return nil, err
	}

	existingByModule := make(map[string]*permissionV1.PermissionGroup)
	for _, g := range existing.Items {
		if g.GetModule() == constants.SystemPermissionModule {
			continue
		}
		if _, ok := existingByModule[g.GetModule()]; !ok {
			existingByModule[g.GetModule()] = g
		}
	}

	groupIDs := make(map[string]uint32)
	var toCreate []*permissionV1.PermissionGroup
	for _, g := range groups {
		cur, ok := existingByModule[g.GetModule()]
		if !ok {
			toCreate = append(toCreate, g)
			continue
		}

		groupIDs[g.GetModule()] = cur.GetId()
		if !data.ShouldRenameSynced(cur.GetName(), legacyNames[g.GetModule()], g.GetName()) {
			continue
		}
		if err = s.permissionGroupRepo.Update(ctx, &permissionV1.UpdatePermissionGroupRequest{
			Id: cur.GetId(),
			Data: &permissionV1.PermissionGroup{
				Name:      g.Name,
				UpdatedBy: trans.Ptr(operatorUserId),
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name", "updated_by"}},
		}); err != nil {
			return nil, err
		}
	}

	if len(toCreate) > 0 {
		created, err := s.permissionGroupRepo.BatchCreate(ctx, toCreate)
		if err != nil {
			s.log.Errorf(ctx, "batch create permission groups failed: %s", err.Error())
			return nil, err
		}
		for _, g := range created {
			groupIDs[g.GetModule()] = g.GetId()
		}
	}

	return groupIDs, nil
}

func (s *PermissionService) createDefaultPermissions(ctx context.Context) error {
	var err error

	for _, d := range constants.DefaultPermissions {
		if err = s.permissionRepo.Create(ctx, &permissionV1.CreatePermissionRequest{
			Data: d,
		}); err != nil {
			s.log.Errorf(ctx, "create default permission %s failed: %v", d.GetCode(), err)
			return err
		}
	}

	return nil
}
