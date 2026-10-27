package authorizer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// stubProvider 空模型/空策略桩。
type stubProvider struct{}

func (stubProvider) ProvideModels(string) ModelDataMap { return nil }

func (stubProvider) ProvidePolicies(context.Context) (PermissionDataMap, error) {
	return PermissionDataMap{}, nil
}

// policiesProvider 返回固定策略与模型的桩。
type policiesProvider struct{ stubProvider }

func (policiesProvider) ProvidePolicies(context.Context) (PermissionDataMap, error) {
	return PermissionDataMap{
		"admin": []PermissionData{{Path: "/api/v1/users", Method: "GET"}},
	}, nil
}

func (policiesProvider) ProvideModels(string) ModelDataMap {
	return ModelDataMap{"rbac.rego": []byte("package rbac\n\nallow = true\n")}
}

func TestNewAuthorizer_NilConfigKeepsEngineNil(t *testing.T) {
	a := NewAuthorizer(context.Background(), bLogger.NopLogger(), nil, stubProvider{})
	require.NotNil(t, a)
	assert.Nil(t, a.Engine(), "未配置授权时引擎必须保持 nil，不得静默降级为放行")
	assert.Error(t, a.ResetPolicies(context.Background()), "引擎未初始化时重载策略必须报错")
}

func TestNewAuthorizer_NoopEngine(t *testing.T) {
	a := NewAuthorizer(context.Background(), bLogger.NopLogger(), &EngineConfig{Type: "noop"}, stubProvider{})
	require.NotNil(t, a)
	require.NotNil(t, a.Engine())
	assert.Equal(t, "noop", a.Engine().Name())
	assert.NoError(t, a.ResetPolicies(context.Background()), "noop 引擎无需装载策略")
}

func TestNewAuthorizer_EmptyTypeDoesNotFallBackToNoop(t *testing.T) {
	a := NewAuthorizer(context.Background(), bLogger.NopLogger(), &EngineConfig{}, stubProvider{})
	assert.Nil(t, a.Engine(), "空类型不得退回放行一切的 noop")
}

func TestNewAuthorizer_UnsupportedEnginesStayNil(t *testing.T) {
	for _, typ := range []string{"opa", "zanzibar", "casbinn"} {
		a := NewAuthorizer(context.Background(), bLogger.NopLogger(), &EngineConfig{Type: typ}, policiesProvider{})
		assert.Nil(t, a.Engine(), "unsupported engine %q must not initialize", typ)
	}
}

func TestNewAuthorizer_NilLoggerAndContext(t *testing.T) {
	assert.NotPanics(t, func() {
		a := NewAuthorizer(nil, nil, &EngineConfig{Type: "noop"}, stubProvider{})
		assert.NotNil(t, a.Engine())
	})
}

func TestResetPolicies_ProviderError(t *testing.T) {
	a := NewAuthorizer(context.Background(), bLogger.NopLogger(), &EngineConfig{Type: "casbin"}, errProvider{})
	require.NotNil(t, a.Engine())
	assert.Error(t, a.ResetPolicies(context.Background()))
}

type errProvider struct{}

func (errProvider) ProvideModels(string) ModelDataMap { return nil }

func (errProvider) ProvidePolicies(context.Context) (PermissionDataMap, error) {
	return nil, errors.New("provider failure")
}
