package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/authorizer"
	authzEngine "go-wind-admin/third_party/kratos-authz/engine"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

func TestNewAuthorizerEngineConfig(t *testing.T) {
	cfg, err := NewAuthorizerEngineConfig(&conf.Bootstrap{Authz: &conf.Authorization{Type: "casbin"}})
	require.NoError(t, err)
	require.Equal(t, "casbin", cfg.Type)

	// 缺省、拼错、noop、opa 都必须报错，不能静默退回放行一切的 noop
	for _, bad := range []*conf.Bootstrap{
		nil,
		{},
		{Authz: &conf.Authorization{Type: ""}},
		{Authz: &conf.Authorization{Type: "noop"}},
		{Authz: &conf.Authorization{Type: "opa"}},
		{Authz: &conf.Authorization{Type: "casbn"}},
	} {
		_, err := NewAuthorizerEngineConfig(bad)
		require.Error(t, err, "config %v must be rejected", bad)
	}
}

type staticPolicyProvider struct{ data authorizer.PermissionDataMap }

func (p staticPolicyProvider) ProvideModels(string) authorizer.ModelDataMap { return nil }
func (p staticPolicyProvider) ProvidePolicies(context.Context) (authorizer.PermissionDataMap, error) {
	return p.data, nil
}

// TestCasbinTenantDomain 钉住 casbin 的域语义：策略按角色租户写域，
// 请求必须带同一租户 ID 才放行；同编码角色在不同租户各自生效、互不串租户。
func TestCasbinTenantDomain(t *testing.T) {
	ctx := context.Background()
	a := authorizer.NewAuthorizer(ctx, bLogger.NopLogger(), &authorizer.EngineConfig{Type: "casbin"}, staticPolicyProvider{
		data: authorizer.PermissionDataMap{
			"super": {{Domain: "0", Path: "/admin/v1/users", Method: "GET"}},
			// 两个租户由模板复制出的同编码角色
			"tenant_manager": {
				{Domain: "5", Path: "/admin/v1/users", Method: "GET"},
				{Domain: "6", Path: "/admin/v1/roles", Method: "GET"},
			},
		},
	})
	require.NoError(t, a.ResetPolicies(ctx))
	e := a.Engine()

	check := func(sub, method, path, dom string) bool {
		ok, err := e.IsAuthorized(ctx, authzEngine.Subject(sub), authzEngine.Action(method), authzEngine.Resource(path), authzEngine.Project(dom))
		require.NoError(t, err)
		return ok
	}

	require.True(t, check("super", "GET", "/admin/v1/users", "0"))
	require.False(t, check("super", "GET", "/admin/v1/users", ""), "empty domain falls back to '*' and must not match tenant-scoped policies")
	require.False(t, check("super", "POST", "/admin/v1/users", "0"))

	require.True(t, check("tenant_manager", "GET", "/admin/v1/users", "5"))
	require.True(t, check("tenant_manager", "GET", "/admin/v1/roles", "6"))
	require.False(t, check("tenant_manager", "GET", "/admin/v1/roles", "5"), "tenant 5 must not inherit tenant 6's grant")
	require.False(t, check("tenant_manager", "GET", "/admin/v1/users", "0"))
}

// TestCasbinPlatformAdminWildcard 平台管理员的通配策略放行所有路径和方法，但只限平台域。
func TestCasbinPlatformAdminWildcard(t *testing.T) {
	ctx := context.Background()
	a := authorizer.NewAuthorizer(ctx, bLogger.NopLogger(), &authorizer.EngineConfig{Type: "casbin"}, staticPolicyProvider{
		data: authorizer.PermissionDataMap{
			"platform:admin": {{Domain: "0", Path: "/*", Method: "ANY"}},
		},
	})
	require.NoError(t, a.ResetPolicies(ctx))
	e := a.Engine()

	for _, c := range []struct {
		method, path, dom string
		want              bool
	}{
		{"GET", "/admin/v1/users", "0", true},
		{"DELETE", "/admin/v1/roles/{id}", "0", true},
		{"POST", "/admin/v1/apis/sync", "0", true},
		{"GET", "/admin/v1/users", "5", false},
	} {
		ok, err := e.IsAuthorized(ctx, "platform:admin", authzEngine.Action(c.method), authzEngine.Resource(c.path), authzEngine.Project(c.dom))
		require.NoError(t, err)
		require.Equal(t, c.want, ok, "%s %s dom=%s", c.method, c.path, c.dom)
	}
}
