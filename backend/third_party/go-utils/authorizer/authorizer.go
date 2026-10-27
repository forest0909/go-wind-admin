package authorizer

import (
	"context"
	"fmt"

	authzEngine "go-wind-admin/third_party/kratos-authz/engine"
	"go-wind-admin/third_party/kratos-authz/engine/casbin"
	"go-wind-admin/third_party/kratos-authz/engine/noop"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// EngineConfig 引擎选择配置。
type EngineConfig struct {
	// Type 引擎类型：只支持 "casbin"。显式 "noop" 仍可用于测试。
	// 空串、拼错的类型、opa、zanzibar 不再退回放行一切的 noop，引擎保持 nil。
	Type string
}

// Authorizer 权限管理器：按配置选择授权引擎，从 Provider 拉取
// 角色→(path,method) 策略并按引擎格式装载。
type Authorizer struct {
	log *bLogger.Helper

	engine   authzEngine.Engine
	provider Provider
}

// NewAuthorizer 创建权限管理器。
//
// cfg 为 nil 表示"未配置授权"，此时引擎保持 nil（与配置缺失语义一致，
// 调用方应在使用 Engine() 前自行判空）——不静默降级为放行的 noop。
func NewAuthorizer(
	ctx context.Context,
	log bLogger.Logger,
	cfg *EngineConfig,
	provider Provider,
) *Authorizer {
	if ctx == nil {
		ctx = context.Background()
	}
	if log == nil {
		log = bLogger.GetLogger()
	}

	a := &Authorizer{
		log:      bLogger.NewHelper(log.With("module", "authorizer")),
		provider: provider,
	}

	a.init(ctx, cfg)

	return a
}

func (a *Authorizer) init(ctx context.Context, cfg *EngineConfig) {
	if cfg == nil {
		a.log.Warn(ctx, "authorization config is nil")
		return
	}
	a.engine = a.newEngine(ctx, cfg)
}

func (a *Authorizer) Engine() authzEngine.Engine {
	return a.engine
}

// ResetPolicies 重置策略
func (a *Authorizer) ResetPolicies(ctx context.Context) error {
	if a.engine == nil {
		return fmt.Errorf("authorization engine is not initialized")
	}

	result, err := a.provider.ProvidePolicies(ctx)
	if err != nil {
		a.log.Errorf(ctx, "provide authorizer data error: %v", err)
		return err
	}

	var policies authzEngine.PolicyMap

	switch a.engine.Name() {
	case "casbin":
		if policies, err = a.generateCasbinPolicies(result); err != nil {
			a.log.Errorf(ctx, "generate casbin policies error: %v", err)
			return err
		}

	case "noop":
		return nil

	default:
		err = fmt.Errorf("unknown engine name: %s", a.engine.Name())
		a.log.Warnf(ctx, "%s", err.Error())
		return err
	}

	if err = a.engine.SetPolicies(ctx, policies, nil); err != nil {
		a.log.Errorf(ctx, "set policies error: %v", err)
		return err
	}

	a.log.Infof(ctx, "reloaded policy rules [%d] successfully for engine: %s", len(policies), a.engine.Name())

	return nil
}

// generateCasbinPolicies 生成 Casbin 策略
func (a *Authorizer) generateCasbinPolicies(data PermissionDataMap) (authzEngine.PolicyMap, error) {
	var rules []casbin.PolicyRule

	for roleCode, aRules := range data {
		for _, api := range aRules {
			rules = append(rules, casbin.PolicyRule{
				PType: "p",
				V0:    roleCode,
				V1:    api.Path,
				V2:    api.Method,
				V3:    api.Domain,
			})
		}
	}

	policies := authzEngine.PolicyMap{
		"policies": rules,
		"projects": authzEngine.MakeProjects(),
	}

	return policies, nil
}

// newEngine 创建权限引擎。
// 未知类型、空类型、opa、zanzibar 不再退回放行一切的 noop：记错误并返回 nil，
// 由启动装配把它当成初始化失败。显式 "noop" 保留给测试。
func (a *Authorizer) newEngine(ctx context.Context, cfg *EngineConfig) authzEngine.Engine {
	switch cfg.Type {
	case "casbin":
		return a.newEngineCasbin(ctx)
	case "noop":
		return a.newEngineNoop(ctx)
	default:
		a.log.Errorf(ctx, "unsupported authz engine %q: only casbin is supported", cfg.Type)
		return nil
	}
}

// newEngineNoop 创建 Noop 引擎
func (a *Authorizer) newEngineNoop(ctx context.Context) authzEngine.Engine {
	state, err := noop.NewEngine(ctx)
	if err != nil {
		a.log.Errorf(ctx, "new noop engine error: %v", err)
		return nil
	}
	return state
}

// newEngineCasbin 创建 Casbin 引擎
func (a *Authorizer) newEngineCasbin(ctx context.Context) authzEngine.Engine {
	state, err := casbin.NewEngine(ctx)
	if err != nil {
		a.log.Errorf(ctx, "init casbin engine error: %v", err)
		return nil
	}
	return state
}
