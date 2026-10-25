package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadOutboundConfigMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadOutboundConfig(dir)
	require.NoError(t, err)
	require.Empty(t, cfg.SMTP.Host)
	require.Empty(t, cfg.SMS.Provider)
}

func TestLoadOutboundConfigReadsFields(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`
smtp:
  host: smtp.example.com
  port: 465
  username: bot
  password: secret
  from: bot@example.com
  from_name: GoWind
  tls: SSL
sms:
  provider: ALIYUN
  access_key_id: id
  access_secret: sec
  sign_name: sign
  template_code: SMS_1
  app_id: ""
  region: ""
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mail.yaml"), body, 0o600))

	cfg, err := LoadOutboundConfig(dir)
	require.NoError(t, err)
	require.Equal(t, "smtp.example.com", cfg.SMTP.Host)
	require.Equal(t, uint32(465), cfg.SMTP.Port)
	require.Equal(t, "secret", cfg.SMTP.Password)
	require.Equal(t, "SSL", cfg.SMTP.TlsMode)
	require.Equal(t, "ALIYUN", cfg.SMS.Provider)
	require.Equal(t, "SMS_1", cfg.SMS.TemplateCode)
}

func TestLoadOutboundConfigRejectsBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mail.yaml"), []byte("smtp: [\n"), 0o600))
	_, err := LoadOutboundConfig(dir)
	require.Error(t, err)
}

func TestSendMailNotConfigured(t *testing.T) {
	sender := &OutboundSender{}
	err := sender.SendMail(t.Context(), "a@example.com", "title", "body")
	require.ErrorIs(t, err, ErrOutboundNotConfigured)
}

func TestSendSMSNotConfigured(t *testing.T) {
	sender := &OutboundSender{}
	err := sender.SendSMS(t.Context(), "13800000000", "123456")
	require.ErrorIs(t, err, ErrOutboundNotConfigured)
}
