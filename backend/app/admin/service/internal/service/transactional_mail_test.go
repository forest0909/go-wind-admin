package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/pkg/mailtext"
)

func TestRenderPwdResetCodeUsesMailtext(t *testing.T) {
	ctx := context.Background()
	title, content := renderPwdResetCode(ctx, &TransactionMailer{}, "ABC123")
	wantTitle, wantContent := mailtext.PasswordResetCode(ctx, "ABC123")
	require.Equal(t, wantTitle, title)
	require.Equal(t, wantContent, content)
	require.Contains(t, content, "ABC123")
}

func TestRenderContactBindCodeUsesMailtext(t *testing.T) {
	ctx := mailtext.WithLocale(context.Background(), mailtext.LocaleEnUS)
	title, content := renderContactBindCode(ctx, nil, "XYZ789")
	wantTitle, wantContent := mailtext.ContactBindCode(ctx, "XYZ789")
	require.Equal(t, wantTitle, title)
	require.Equal(t, wantContent, content)
	require.Contains(t, content, "XYZ789")
}
