package service

import (
	"context"

	"go-wind-admin/pkg/mailtext"
)

// TransactionMailer 验证码邮件文案入口。
// 通知模板表已删除，文案固定走 pkg/mailtext。结构体留着，是为了以后接回可配置文案时调用方不用改。
type TransactionMailer struct{}

// renderPwdResetCode 找回密码验证码邮件。
func renderPwdResetCode(ctx context.Context, _ *TransactionMailer, code string) (string, string) {
	return mailtext.PasswordResetCode(ctx, code)
}

// renderContactBindCode 邮箱换绑验证码邮件。
func renderContactBindCode(ctx context.Context, _ *TransactionMailer, code string) (string, string) {
	return mailtext.ContactBindCode(ctx, code)
}
