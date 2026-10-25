package data

import (
	"context"
	"fmt"
	"strconv"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/cmd/server/assets"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/authorizer"
	"go-wind-admin/pkg/constants"
)

// AuthzEngineCasbin 唯一受支持的接口级鉴权引擎。
const AuthzEngineCasbin = "casbin"

// NewAuthorizerEngineConfig 校验 authz 配置并返回引擎配置。
// 只接受 casbin：authorizer 对未知类型会静默退回放行一切的 noop，
// 配置缺失时引擎为 nil、authz 中间件同样不拦截，两种情况都必须在启动期报错。
func NewAuthorizerEngineConfig(cfg *conf.Bootstrap) (*authorizer.EngineConfig, error) {
	if cfg == nil || cfg.Authz == nil {
		return nil, fmt.Errorf("authz config is missing: authz.type must be %q", AuthzEngineCasbin)
	}
	if t := cfg.Authz.GetType(); t != AuthzEngineCasbin {
		return nil, fmt.Errorf("unsupported authz.type %q: only %q is supported", t, AuthzEngineCasbin)
	}
	return &authorizer.EngineConfig{Type: AuthzEngineCasbin}, nil
}

// AuthorizerProvider 权限数据提供者
type AuthorizerProvider struct {
	log *bLogger.Helper

	roleRepo *RoleRepo
	apiRepo  *ApiRepo
}

func NewAuthorizerProvider(
	ctx *bootstrap.Context,
	roleRepo *RoleRepo,
	apiRepo *ApiRepo,
) authorizer.Provider {
	return &AuthorizerProvider{
		log:      ctx.NewLoggerHelper("authorizer-data-provider/data/admin-service"),
		roleRepo: roleRepo,
		apiRepo:  apiRepo,
	}
}

// ProvideModels 提供模型数据
func (p *AuthorizerProvider) ProvideModels(engineName string) authorizer.ModelDataMap {
	switch engineName {
	case "casbin":
		return make(map[string][]byte)
	case "opa":
		return map[string][]byte{
			"rbac.rego": assets.OpaRbacRego,
		}
	}
	return nil
}

// ProvidePolicies 提供策略数据
func (p *AuthorizerProvider) ProvidePolicies(_ context.Context) (authorizer.PermissionDataMap, error) {
	// 策略装载需要全量角色/权限数据，统一以 SystemContext 跑，忽略调用方 ctx 的 viewer
	ctx := viewer.WithSystemContext(context.Background())

	roles, err := p.roleRepo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	if err != nil {
		p.log.Errorf(context.Background(), "failed to list roles: %v", err)
		return nil, err
	}

	result := make(authorizer.PermissionDataMap)
	var apiIDs []uint32
	var apis []*permissionV1.Api
	for _, role := range roles.Items {
		//p.log.Infof(context.Background(), _, "processing role: %s", role.GetCode())
		if role == nil {
			continue
		}
		if role.GetCode() == "" {
			continue
		}
		if constants.IsTemplateRoleCode(role.GetCode()) {
			continue
		}

		apiIDs, err = p.roleRepo.GetRolePermissionApiIDs(ctx, role.GetId())
		if err != nil {
			p.log.Errorf(context.Background(), "failed to get role [%d] permission api ids: %v", role.GetId(), err)
			continue
		}

		// 角色未绑定任何 API 时直接跳过：GetApiByIDs 对空集合返回 400，
		// 逐角色刷 ERROR 日志纯属误导。
		if len(apiIDs) == 0 {
			continue
		}

		apis, err = p.apiRepo.GetApiByIDs(ctx, apiIDs)
		if err != nil {
			p.log.Errorf(context.Background(), "failed to list apis by ids: %v", err)
			continue
		}

		var authorizerDataArray authorizer.PermissionDataArray
		for _, api := range apis {
			if api == nil {
				continue
			}
			if api.GetPath() == "" || api.GetMethod() == "" {
				continue
			}

			var data = authorizer.PermissionData{
				Domain: strconv.FormatUint(uint64(role.GetTenantId()), 10),
				Path:   api.GetPath(),
				Method: api.GetMethod(),
			}
			authorizerDataArray = append(authorizerDataArray, data)
		}
		// 角色编码只在租户内唯一（uix_sys_roles_tenant_code），各租户由模板复制出的
		// 角色编码相同；必须追加而非覆盖，否则只剩最后遍历到的租户的策略。
		// 每条策略自带租户域，追加不会串租户。
		if len(authorizerDataArray) > 0 {
			result[role.GetCode()] = append(result[role.GetCode()], authorizerDataArray...)
		}
	}

	return result, nil
}
