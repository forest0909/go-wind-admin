package constants

import (
	"testing"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

func TestIsTenantManagerPermissionCode(t *testing.T) {
	cases := map[string]bool{
		SystemAccessBackendPermissionCode: true,
		SystemPlatformAdminPermissionCode: false,
		SystemTenantManagerPermissionCode: false,
		"user:view":                       true,
		"user:create":                     true,
		"role:edit":                       true,
		"permission:view":                 true,
		"permission-group:view":           true,
		"permission:dir":                  true,
		"dict:create":                     true,
		"online-session:create":           true,
		"my-tenant-usage:view":            true,
		"tenant-branding:create":          true,
		"permission:edit":                 false,
		"permission-group:delete":         false,
		"api:view":                        false,
		"menu:create":                     false,
		"code:view":                       false,
		"tenant:view":                     false,
		"tenant:dir":                      false,
		"member:view":                     false,
		"plan:view":                       false,
		"plan-quota:edit":                 false,
		"plan-module:create":              false,
		"config:view":                     false,
		"language:view":                   false,
		"":                                false,
	}
	for code, want := range cases {
		if got := IsTenantManagerPermissionCode(code); got != want {
			t.Errorf("IsTenantManagerPermissionCode(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestIsPlatformOnlyApi(t *testing.T) {
	cases := []struct {
		service, method string
		want            bool
	}{
		{"UserService", "POST", false},
		{"RoleService", "PUT", false},
		{"TenantService", "GET", true},
		{"MenuService", "GET", true},
		{"ApiService", "POST", true},
		{"ConfigService", "GET", true},
		{"PermissionService", "GET", false},
		{"PermissionService", "POST", true},
		{"PermissionGroupService", "DELETE", true},
		{"LanguageService", "get", false},
		{"LanguageService", "PUT", true},
	}
	for _, c := range cases {
		if got := IsPlatformOnlyApi(c.service, c.method); got != c.want {
			t.Errorf("IsPlatformOnlyApi(%q, %q) = %v, want %v", c.service, c.method, got, c.want)
		}
	}
}

func TestMenuPathToBusinessModule(t *testing.T) {
	cases := map[string]identityV1.Module{
		"/dashboard/analytics":       identityV1.Module_DASHBOARD,
		"/opm/users":                 identityV1.Module_OPM,
		"/system/dict":               identityV1.Module_DICT,
		"/system/files":              identityV1.Module_FILE,
		"/system/tasks":              identityV1.Module_TASK,
		"/system/online-sessions":    identityV1.Module_SYSTEM,
		"/system":                    identityV1.Module_SYSTEM,
		"/systemx":                   identityV1.Module_MODULE_UNSPECIFIED,
		"/internal-message/messages": identityV1.Module_INTERNAL_MESSAGE,
		"/log/login-audit-logs":      identityV1.Module_LOG,
		"/permission/roles":          identityV1.Module_PERMISSION,
		"/tenant/plans":              identityV1.Module_TENANT,
		"/custom":                    identityV1.Module_MODULE_UNSPECIFIED,
	}
	for path, want := range cases {
		if got := MenuPathToBusinessModule(path); got != want {
			t.Errorf("MenuPathToBusinessModule(%q) = %v, want %v", path, got, want)
		}
	}
}
