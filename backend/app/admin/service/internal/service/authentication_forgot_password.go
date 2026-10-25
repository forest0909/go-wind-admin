package service

import (
	"context"
	"fmt"
	"github.com/tx7do/go-crud/viewer"
	"strings"
	"time"

	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"go-wind-admin/pkg/mailtext"

	"github.com/tx7do/go-utils/notify/sms"
)

// generateVCode 生成 6 位数字验证码。
// 使用纳秒时间取模：验证码仅用于一次性校验，安全强度由
// 「单次有效 + 10 分钟 TTL + 服务端比对」保证，不依赖密码学随机。
func generateVCode() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

// resetIdentityTypeOf 按.identifier 的形态选凭证类型：像手机号（无 @ 且能归一）→ PHONE 走短信，
// 其余（邮箱/账号名）→ EMAIL 维持既有行为。防枚举口径两边一致：查无此人一律静默 200。
func resetIdentityTypeOf(identifier string) authenticationV1.UserCredential_IdentityType {
	if !strings.Contains(identifier, "@") {
		if _, _, err := sms.NormalizePhoneNumber(identifier); err == nil {
			return authenticationV1.UserCredential_PHONE
		}
	}
	return authenticationV1.UserCredential_EMAIL
}

// ForgotPassword 忘记密码：向 identifier（已绑定的邮箱凭证或手机号凭证）发送
// 重置验证码。邮箱和短信都读 configs/mail.yaml。
// 用户不存在时同样返回成功，防止通过接口枚举有效账号。
func (s *AuthenticationService) ForgotPassword(ctx context.Context, req *authenticationV1.ForgotPasswordRequest) (*emptypb.Empty, error) {
	ctx = viewer.WithSystemContext(ctx)
	identifier := strings.TrimSpace(req.GetIdentifier())
	if identifier == "" {
		return nil, authenticationV1.ErrorBadRequest("identifier is required")
	}

	identityType := resetIdentityTypeOf(identifier)
	cred, credErr := s.userCredentialRepo.GetByIdentifier(ctx, &authenticationV1.GetUserCredentialByIdentifierRequest{
		IdentityType: identityType,
		Identifier:   identifier,
	})
	userId := cred.GetUserId()
	if credErr != nil || userId == 0 {
		s.log.Infof(ctx, "forgot-password: no %s credential for [%s] (silent ok)", identityType.String(), identifier)
		return &emptypb.Empty{}, nil
	}

	code := generateVCode()
	if err := s.vcodeCache.Save("reset_password", identifier, code, 10*time.Minute); err != nil {
		return nil, authenticationV1.ErrorInternalServerError("save verification code failed")
	}

	// 文案语言：优先收件用户的偏好 locale（个人中心设置），未设置回落请求
	// Accept-Language（mailtext.Resolve 的兜底链）。
	if user, uErr := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{Id: userId},
	}); uErr == nil && user.GetLocale() != "" {
		if loc, ok := mailtext.LocaleOfTag(user.GetLocale()); ok {
			ctx = mailtext.WithLocale(ctx, loc)
		}
	}

	var sendErr error
	if identityType == authenticationV1.UserCredential_PHONE {
		sendErr = s.outbound.SendSMS(ctx, identifier, code)
	} else {
		title, content := renderPwdResetCode(ctx, s.mailer, code)
		sendErr = s.outbound.SendMail(ctx, identifier, title, content)
	}
	if sendErr != nil {
		s.log.Errorf(ctx, "forgot-password: send code to [%s] failed: %s", identifier, sendErr.Error())
		if errors.Is(sendErr, ErrOutboundNotConfigured) {
			if identityType == authenticationV1.UserCredential_PHONE {
				return nil, authenticationV1.ErrorInternalServerError("sms channel is not configured")
			}
			return nil, authenticationV1.ErrorInternalServerError("email channel is not configured")
		}
		return nil, authenticationV1.ErrorInternalServerError("send verification message failed")
	}

	s.log.Infof(ctx, "forgot-password: reset code sent to [%s] for user [%d]", identifier, userId)
	return &emptypb.Empty{}, nil
}

// ResetPasswordByCode 凭邮箱验证码重置密码（免鉴权）。
// 校验通过后重置密码（密码策略/加密在 repo 层处理）并吊销该用户全部会话。
func (s *AuthenticationService) ResetPasswordByCode(ctx context.Context, req *authenticationV1.ResetPasswordByCodeRequest) (*emptypb.Empty, error) {
	ctx = viewer.WithSystemContext(ctx)
	identifier := strings.TrimSpace(req.GetIdentifier())
	code := strings.TrimSpace(req.GetCode())
	newPassword := req.GetNewPassword()
	if identifier == "" || code == "" || newPassword == "" {
		return nil, authenticationV1.ErrorBadRequest("identifier, code and new password are required")
	}

	if !s.vcodeCache.Verify("reset_password", identifier, code) {
		return nil, authenticationV1.ErrorBadRequest("invalid or expired verification code")
	}

	cred, credErr := s.userCredentialRepo.GetByIdentifier(ctx, &authenticationV1.GetUserCredentialByIdentifierRequest{
		IdentityType: resetIdentityTypeOf(identifier),
		Identifier:   identifier,
	})
	if credErr != nil || cred.GetUserId() == 0 {
		return nil, authenticationV1.ErrorBadRequest("invalid or expired verification code")
	}
	userId := cred.GetUserId()

	user, err := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{Id: userId},
	})
	if err != nil {
		return nil, err
	}

	if err = s.userCredentialRepo.ResetCredential(ctx, &authenticationV1.ResetCredentialRequest{
		IdentityType:  authenticationV1.UserCredential_USERNAME,
		Identifier:    user.GetUsername(),
		NewCredential: newPassword,
		NeedDecrypt:   true,
	}); err != nil {
		s.log.Errorf(ctx, "reset password by code failed for user [%d]: %s", userId, err.Error())
		return nil, err
	}

	if err = s.authenticator.RevokeUserTokenAllClientTypes(ctx, userId); err != nil {
		s.log.Errorf(ctx, "revoke tokens after reset-by-code failed for user [%d]: %s", userId, err.Error())
	}

	s.log.Infof(ctx, "password reset by code done for user [%d]", userId)
	return &emptypb.Empty{}, nil
}
