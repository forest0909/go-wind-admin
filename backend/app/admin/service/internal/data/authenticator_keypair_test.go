package data

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

func newTestRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// 只用环境变量换私钥、yaml 仍带旧公钥时，应该从新私钥派生公钥，签发的令牌能验过。
// 修复前：新私钥签发、yaml 旧公钥校验，令牌全部验签失败。
func TestApplyJwtKeyOverrides_PrivateKeyOnlyDerivesPublicKey(t *testing.T) {
	newPriv := newTestRSAPrivateKeyPEM(t)
	t.Setenv("GWA_AUTH_JWT_PRIVATE_KEY", newPriv)
	t.Setenv("GWA_AUTH_JWT_PUBLIC_KEY", "")

	jwtCfg := &conf.Authentication_Jwt{
		Method:     "RS256",
		PrivateKey: ptrString(testRSAPrivateKey),
		PublicKey:  ptrString(testRSAPublicKey),
	}
	jwtCfg, err := applyJwtKeyOverrides(bLogger.NewHelper(bLogger.NopLogger()), jwtCfg)
	if err != nil {
		t.Fatalf("applyJwtKeyOverrides: %v", err)
	}
	if jwtCfg.PublicKey != nil {
		t.Fatalf("stale yaml public key should be cleared when only private key is overridden")
	}

	auth, err := newAdminAuthenticator(jwtCfg)
	if err != nil {
		t.Fatalf("newAdminAuthenticator: %v", err)
	}
	if err = selfCheckSignVerify(auth); err != nil {
		t.Fatalf("token signed by new private key must verify: %v", err)
	}
}

// 两个环境变量都给时照用，不派生。
func TestApplyJwtKeyOverrides_BothKeysKept(t *testing.T) {
	priv := newTestRSAPrivateKeyPEM(t)
	pub := "-----BEGIN PUBLIC KEY-----\nnot-checked-here\n-----END PUBLIC KEY-----"
	t.Setenv("GWA_AUTH_JWT_PRIVATE_KEY", priv)
	t.Setenv("GWA_AUTH_JWT_PUBLIC_KEY", pub)

	jwtCfg, err := applyJwtKeyOverrides(bLogger.NewHelper(bLogger.NopLogger()), &conf.Authentication_Jwt{Method: "RS256"})
	if err != nil {
		t.Fatalf("applyJwtKeyOverrides: %v", err)
	}
	if jwtCfg.GetPublicKey() != pub || jwtCfg.GetPrivateKey() != priv {
		t.Fatalf("both env keys should be applied as-is")
	}
}

// 通过环境变量注入的是仓库公开的示例私钥（configs/auth.yaml 里那把）时拒绝启动，换行宽度不同也要认出来。
// 修复前：只打告警，部署照常用公开私钥签令牌。
func TestApplyJwtKeyOverrides_EnvSampleKeyRejected(t *testing.T) {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(testRSAPrivateKey,
		"-----BEGIN PRIVATE KEY-----"), "-----END PRIVATE KEY-----"))
	oneLine := "-----BEGIN PRIVATE KEY-----\n" + strings.Join(strings.Fields(body), "") + "\n-----END PRIVATE KEY-----\n"

	for name, key := range map[string]string{"as-in-yaml": testRSAPrivateKey, "rewrapped": oneLine} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GWA_AUTH_JWT_PRIVATE_KEY", key)
			t.Setenv("GWA_AUTH_JWT_PUBLIC_KEY", "")
			_, err := applyJwtKeyOverrides(bLogger.NewHelper(bLogger.NopLogger()),
				&conf.Authentication_Jwt{Method: "RS256"})
			if err == nil {
				t.Fatalf("sample private key injected via GWA_AUTH_JWT_PRIVATE_KEY must be rejected")
			}
		})
	}
}

// 本地开发：没设环境变量、只从 auth.yaml 读到示例私钥，照常启动（只告警）。
func TestApplyJwtKeyOverrides_YamlSampleKeyAllowed(t *testing.T) {
	t.Setenv("GWA_AUTH_JWT_PRIVATE_KEY", "")
	t.Setenv("GWA_AUTH_JWT_PUBLIC_KEY", "")

	jwtCfg, err := applyJwtKeyOverrides(bLogger.NewHelper(bLogger.NopLogger()), &conf.Authentication_Jwt{
		Method:     "RS256",
		PrivateKey: ptrString(testRSAPrivateKey),
		PublicKey:  ptrString(testRSAPublicKey),
	})
	if err != nil {
		t.Fatalf("sample key from yaml must stay allowed for local dev: %v", err)
	}
	if jwtCfg.GetPrivateKey() != testRSAPrivateKey {
		t.Fatalf("yaml key should be kept")
	}
}

// 公私钥不配对时启动期自检直接失败。
func TestNewAdminAuthenticator_KeyMismatchFailsFast(t *testing.T) {
	jwtCfg := &conf.Authentication_Jwt{
		Method:     "RS256",
		PrivateKey: ptrString(newTestRSAPrivateKeyPEM(t)),
		PublicKey:  ptrString(testRSAPublicKey),
	}
	_, err := newAdminAuthenticator(jwtCfg)
	if err == nil || !strings.Contains(err.Error(), "self-check") {
		t.Fatalf("expected self-check error for mismatched key pair, got %v", err)
	}
}
