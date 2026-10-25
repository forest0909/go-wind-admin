// options_test.go —— options.go 全部 Option setter 的表驱动测试。
//
// 覆盖内容：每个 With* 必须把值写入 options 的对应字段（写入函数注入、
// 登录/登出操作名单覆盖、ECDSA 密钥注入）。行为层的覆盖在
// logging_server_test.go 验证，此处做结构层断言。
package logging

import (
	"context"
	"testing"

	"crypto/ecdsa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/auditutil"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
)

// TestOptionSetters 表驱动验证 setter 的字段落位。
func TestOptionSetters(t *testing.T) {
	key, _, err := auditutil.GenerateECDSAKeyPair()
	require.NoError(t, err)

	fLogin := func(ctx context.Context, d *auditV1.LoginAuditLog) error { return nil }
	fOp := func(ctx context.Context, d *auditV1.OperationAuditLog) error { return nil }

	cases := []struct {
		name   string
		opt    Option
		verify func(o *options) bool
	}{
		{
			"WithWriteLoginLogFunc",
			WithWriteLoginLogFunc(fLogin),
			func(o *options) bool { return o.writeLoginLogFunc != nil },
		},
		{
			"WithWriteOperationAuditLogFunc",
			WithWriteOperationAuditLogFunc(fOp),
			func(o *options) bool { return o.writeOperationAuditLogFunc != nil },
		},
		{
			"WithLoginOperation覆盖名单",
			WithLoginOperation("a", "b"),
			func(o *options) bool {
				return len(o.loginOperations) == 2 && o.loginOperations[0] == "a" && o.loginOperations[1] == "b"
			},
		},
		{
			"WithLoginOperation空变参清空名单",
			WithLoginOperation(),
			func(o *options) bool { return len(o.loginOperations) == 0 },
		},
		{
			"WithLogoutOperation",
			WithLogoutOperation("custom-logout"),
			func(o *options) bool { return o.logoutOperation == "custom-logout" },
		},
		{
			"WithECPrivateKey",
			WithECPrivateKey(key),
			func(o *options) bool { return o.ecPrivateKey == key },
		},
		{
			"WithECPublicKey",
			WithECPublicKey(&key.PublicKey),
			func(o *options) bool {
				var want *ecdsa.PublicKey = &key.PublicKey
				return o.ecPublicKey == want
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var o options
			tc.opt(&o)
			assert.True(t, tc.verify(&o), "setter 必须写入对应字段")
		})
	}
}
