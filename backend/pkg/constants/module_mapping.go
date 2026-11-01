package constants

import (
	"strings"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

// ServiceTagToBusinessModule 将 OpenAPI tag 名（gRPC 服务名）映射到业务功能模块。
// 用于 sys_apis.business_module 的回填，供套餐模块白名单过滤使用。
// 新增服务时必须在此登记，否则其 API 的 business_module 将为 UNSPECIFIED，
// 租户白名单过滤会把 UNSPECIFIED 视为"不在任何白名单内"而拒绝访问。
//
// 反过来说：登记成哪个模块，就等于把该服务的全部端点交给那个模块的白名单。
var ServiceTagToBusinessModule = map[string]identityV1.Module{
	"AdminPortalService": identityV1.Module_DASHBOARD,
	"DashboardService":   identityV1.Module_DASHBOARD,

	"UserService":        identityV1.Module_OPM,
	"OrgUnitService":     identityV1.Module_OPM,
	"PositionService":    identityV1.Module_OPM,
	"UserProfileService": identityV1.Module_OPM,
	"RoleService":        identityV1.Module_OPM,

	"MenuService":            identityV1.Module_PERMISSION,
	"ApiService":             identityV1.Module_PERMISSION,
	"PermissionService":      identityV1.Module_PERMISSION,
	"PermissionGroupService": identityV1.Module_PERMISSION,

	"DictTypeService":      identityV1.Module_DICT,
	"DictEntryService":     identityV1.Module_DICT,
	"LanguageService":      identityV1.Module_SYSTEM,
	"FileService":          identityV1.Module_FILE,
	"FileTransferService":  identityV1.Module_FILE,
	"TaskService":          identityV1.Module_TASK,
	"MyTenantUsageService": identityV1.Module_TENANT,
	"ConfigService":        identityV1.Module_SYSTEM,

	"TenantService":    identityV1.Module_TENANT,
	"PlanService":      identityV1.Module_TENANT,
	"PlanQuotaService": identityV1.Module_TENANT,

	"LoginAuditLogService":     identityV1.Module_LOG,
	"OperationAuditLogService": identityV1.Module_LOG,
	"OnlineSessionService":     identityV1.Module_SYSTEM,

	"InternalMessageService":          identityV1.Module_INTERNAL_MESSAGE,
	"InternalMessageCategoryService":  identityV1.Module_INTERNAL_MESSAGE,
	"InternalMessageRecipientService": identityV1.Module_INTERNAL_MESSAGE,

	"AuthenticationService": identityV1.Module_DASHBOARD,
}

// menuPathModules 菜单路由前缀对应的业务模块，粒度和上面服务的映射一致：
// 套餐白名单按这里决定侧栏显示哪些页面。种子菜单的 module 字段有的为空、
// 字典/文件/任务又都记成 SYSTEM，跟套餐里拆开的 DICT/FILE/TASK 对不上，所以按路由判断。
// 按顺序取第一个命中的，所以更长的前缀要排在前面。
var menuPathModules = []struct {
	prefix string
	module identityV1.Module
}{
	{"/system/dict", identityV1.Module_DICT},
	{"/system/files", identityV1.Module_FILE},
	{"/system/tasks", identityV1.Module_TASK},
	{"/system", identityV1.Module_SYSTEM},
	{"/dashboard", identityV1.Module_DASHBOARD},
	{"/opm", identityV1.Module_OPM},
	{"/permission", identityV1.Module_PERMISSION},
	{"/internal-message", identityV1.Module_INTERNAL_MESSAGE},
	{"/log", identityV1.Module_LOG},
	{"/tenant", identityV1.Module_TENANT},
}

// MenuPathToBusinessModule 按完整路由路径找所属业务模块，找不到返回 UNSPECIFIED。
func MenuPathToBusinessModule(path string) identityV1.Module {
	for _, m := range menuPathModules {
		if path == m.prefix || strings.HasPrefix(path, m.prefix+"/") {
			return m.module
		}
	}
	return identityV1.Module_MODULE_UNSPECIFIED
}
