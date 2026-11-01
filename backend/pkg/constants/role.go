package constants

const (
	// RoleCodeSpilt 角色代码分隔符
	RoleCodeSpilt = ":"

	// PlatformRoleCodePrefix 平台角色代码前缀
	PlatformRoleCodePrefix = "platform" + RoleCodeSpilt

	// PlatformAdminRoleCode 平台管理员角色代码
	PlatformAdminRoleCode = PlatformRoleCodePrefix + "admin"

	// DefaultPlatformAdminRoleName 平台管理员角色默认名称
	DefaultPlatformAdminRoleName = "平台管理员"
)

func HasRoleCodePrefix(roleCode string, prefix string) bool {
	return len(roleCode) >= len(prefix) && roleCode[0:len(prefix)] == prefix
}
