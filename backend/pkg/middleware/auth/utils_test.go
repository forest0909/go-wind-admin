package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	authz "github.com/tx7do/kratos-authz/middleware"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

// TestProcessAuthz_ProjectIsTenantId 鉴权声明的域必须是令牌里的租户 ID：
// casbin 策略按角色所属租户写域（平台为 "0"），域为空时引擎按 "*" 匹配会全部拒绝。
func TestProcessAuthz_ProjectIsTenantId(t *testing.T) {
	cases := []struct {
		name     string
		tenantId *uint32
		want     string
	}{
		{"platform user without tenant id", nil, "0"},
		{"platform user with tenant id 0", trans.Ptr(uint32(0)), "0"},
		{"tenant user", trans.Ptr(uint32(5)), "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := &authenticationV1.UserTokenPayload{TenantId: tc.tenantId, Roles: []string{"r1"}}
			ctx, err := processAuthz(context.Background(), &fakeTransporter{hdr: fakeHeader{}, op: "/fake.Test/Op"}, payload)
			require.NoError(t, err)

			claims, ok := authz.FromContext(ctx)
			require.True(t, ok)
			require.NotNil(t, claims.Project)
			require.Equal(t, tc.want, string(*claims.Project))
		})
	}
}
