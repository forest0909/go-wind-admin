package auth

import "github.com/go-kratos/kratos/v2/errors"

const (
	reason string = "UNAUTHORIZED"
)

var (
	ErrWrongContext        = errors.Unauthorized(reason, "wrong context for middleware")
	ErrMissingBearerToken  = errors.Unauthorized(reason, "missing bearer token")
	ErrValidateTokenFailed = errors.Unauthorized(reason, "validate token failed")

	ErrMissingJwtToken       = errors.Unauthorized(reason, "no jwt token in context")
	ErrExtractUserInfoFailed = errors.Unauthorized(reason, "extract user info failed")
	ErrExtractSubjectFailed  = errors.Unauthorized(reason, "extract subject failed")
	ErrAccessTokenExpired    = errors.Unauthorized(reason, "access token expired")
	ErrInvalidRequest        = errors.Unauthorized(reason, "invalid request")
	// ErrLegacyTenantToken 单租户：令牌的 tid 不是 0（升级前旧租户签发的会话），一律拒绝。
	// 不把 tid 改写成 0：那会把旧租户身份变成平台视图。
	ErrLegacyTenantToken = errors.Unauthorized(reason, "token of a removed tenant is no longer accepted")

	ErrAccessTokenCheckerNotConfigured = errors.Unauthorized(reason, "access token checker is not configured")
)
