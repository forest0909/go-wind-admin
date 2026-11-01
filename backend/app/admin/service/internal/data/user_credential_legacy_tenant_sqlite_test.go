package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/trans"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// TestUserCredentialRepoSqlite_LegacyTenantDuplicateUsername 旧库里另一个租户（5）也有用户名 admin：
// 租户 0 的改密和重置只命中租户 0 的凭证，遗留凭证不受影响。
// 改动前（只有 tid>0 时才加租户谓词）两次调用都 500 "query one data failed"（.Only 命中两行）。
func TestUserCredentialRepoSqlite_LegacyTenantDuplicateUsername(t *testing.T) {
	repo := newUserCredentialRepoSqlite(t)
	sys := enttest.NewSystemContext(context.Background())
	const oldPw = "Str0ng!Passw0rd#42"
	for _, c := range []struct{ uid, tid uint32 }{{1, 0}, {501, 5}} {
		require.NoError(t, repo.Create(sys, &authenticationV1.CreateUserCredentialRequest{Data: &authenticationV1.UserCredential{
			UserId: trans.Ptr(c.uid), TenantId: trans.Ptr(c.tid),
			IdentityType: authenticationV1.UserCredential_USERNAME.Enum(), Identifier: trans.Ptr("admin"),
			CredentialType: authenticationV1.UserCredential_PASSWORD_HASH.Enum(), Credential: trans.Ptr(oldPw),
			Status: authenticationV1.UserCredential_ENABLED.Enum(),
		}}))
	}
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(1, 0, 0, "", nil))

	require.NoError(t, repo.ChangeCredential(ctx, &authenticationV1.ChangeCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin",
		OldCredential: oldPw, NewCredential: "N3w!Passw0rd#77",
	}), "租户 0 的 admin 应能改密")
	uid, err := repo.FindUserCredential(sys, 0, authenticationV1.UserCredential_USERNAME, "admin", "N3w!Passw0rd#77", false)
	require.NoError(t, err)
	require.Equal(t, uint32(1), uid)

	require.NoError(t, repo.ResetCredential(ctx, &authenticationV1.ResetCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin", NewCredential: "R3set!Passw0rd#99",
	}), "租户 0 的 admin 应能被重置")
	_, err = repo.FindUserCredential(sys, 0, authenticationV1.UserCredential_USERNAME, "admin", "R3set!Passw0rd#99", false)
	require.NoError(t, err)

	// 遗留租户 5 的凭证保持原口令
	uid, err = repo.FindUserCredential(sys, 5, authenticationV1.UserCredential_USERNAME, "admin", oldPw, false)
	require.NoError(t, err, "遗留凭证不应被一并修改")
	require.Equal(t, uint32(501), uid)
}
