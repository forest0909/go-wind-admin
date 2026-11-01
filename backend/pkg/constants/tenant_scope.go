package constants

import "strings"

// 租户能用哪些功能。平台和租户共用一套菜单、接口和权限点，这里划出只属于平台的那部分。
// 划分依据：对应的表没有租户列（接口、菜单、权限点、套餐、租户、系统配置、语言），
// 租户改了就是改全平台的数据。

// platformOnlyServices 租户角色完全拿不到的服务（OpenAPI tag，即 sys_apis.module）。
var platformOnlyServices = map[string]bool{
	"ApiService":        true,
	"MenuService":       true,
	"TenantService":     true,
	"PlanService":       true,
	"PlanQuotaService":  true,
	"PlanModuleService": true,
	"ConfigService":     true,
}

// platformWriteServices 租户角色只能读的服务：角色抽屉要列出权限树，字典页要列出语言。
var platformWriteServices = map[string]bool{
	"PermissionService":      true,
	"PermissionGroupService": true,
	"LanguageService":        true,
}

// IsPlatformOnlyApi 判断租户域的角色能不能拿到这个接口。
// 授权策略装载时用它兜底：哪怕平台管理员把相关权限点勾给了租户角色，租户也调不到。
func IsPlatformOnlyApi(service, method string) bool {
	if platformOnlyServices[service] {
		return true
	}
	if platformWriteServices[service] {
		switch strings.ToUpper(method) {
		case "GET", "HEAD", "OPTIONS":
			return false
		}
		return true
	}
	return false
}

// tenantBaselineServices 进入系统本身要用的服务，不受套餐模块白名单限制：
// 取菜单和权限码、登录登出和刷新令牌、读写自己的资料、看自己租户的用量。
// 它们按 ServiceTagToBusinessModule 归在仪表盘、组织人员等模块下，套餐不勾这些模块时，
// 租户连菜单都拉不出来（AdminPortalService 归仪表盘），或者登录后取不到自己（/me 归组织人员）。
var tenantBaselineServices = map[string]bool{
	"AdminPortalService":    true,
	"AuthenticationService": true,
	"UserProfileService":    true,
	"MyTenantUsageService":  true,
}

// IsTenantBaselineService 判断服务是否对所有租户开放（不看套餐白名单）。
func IsTenantBaselineService(service string) bool {
	return tenantBaselineServices[service]
}

// tenantExcludedPermissionResources 不授给租户管理员的权限点，按编码第一段（资源）匹配。
// 这些页面全是平台功能：接口、菜单、权限点页、租户、套餐、系统配置、语言。
var tenantExcludedPermissionResources = map[string]bool{
	"api":         true,
	"menu":        true,
	"code":        true,
	"tenant":      true,
	"member":      true,
	"plan":        true,
	"plan-quota":  true,
	"plan-module": true,
	"config":      true,
	"language":    true,
}

// tenantExcludedPermissionCodes 权限点和权限组只给查看（角色抽屉的权限树要用），不给增删改。
var tenantExcludedPermissionCodes = map[string]bool{
	"permission:create":       true,
	"permission:edit":         true,
	"permission:delete":       true,
	"permission-group:create": true,
	"permission-group:edit":   true,
	"permission-group:delete": true,
}

// IsTenantManagerPermissionCode 判断权限点是否授给租户管理员。
// 系统内置的 sys:* 权限点里，租户管理员只拿「访问后台」，其余是平台身份标记。
func IsTenantManagerPermissionCode(code string) bool {
	if code == "" {
		return false
	}
	if strings.HasPrefix(code, "sys:") {
		return code == SystemAccessBackendPermissionCode
	}
	if tenantExcludedPermissionCodes[code] {
		return false
	}
	resource, _, _ := strings.Cut(code, ":")
	return !tenantExcludedPermissionResources[resource]
}
