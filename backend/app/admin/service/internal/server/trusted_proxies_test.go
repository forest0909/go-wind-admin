package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-wind-admin/third_party/go-utils/netutil"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

func clientIPVia(peer, xff string) string {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = peer + ":5555"
	req.Header.Set("X-Forwarded-For", xff)
	return netutil.ClientIPFromRequest(req)
}

func TestApplyTrustedProxies(t *testing.T) {
	t.Cleanup(func() {
		netutil.SetTrustedProxies([]string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "::1/128", "fc00::/7"})
	})

	// 不配置：保持默认，私网对端的 XFF 照常采信
	applyTrustedProxies(&conf.Server_REST{})
	if got := clientIPVia("10.1.2.3", "1.1.1.1"); got != "1.1.1.1" {
		t.Fatalf("默认应信任私网代理, got %s", got)
	}

	// 配置为单个代理：其他私网对端伪造的 XFF 被忽略
	applyTrustedProxies(&conf.Server_REST{TrustedProxies: &conf.Server_REST_TrustedProxies{Cidrs: []string{"172.18.0.2"}}})
	if got := clientIPVia("10.1.2.3", "1.1.1.1"); got != "10.1.2.3" {
		t.Fatalf("非可信对端应忽略 XFF, got %s", got)
	}
	if got := clientIPVia("172.18.0.2", "1.1.1.1"); got != "1.1.1.1" {
		t.Fatalf("可信代理的 XFF 应采信, got %s", got)
	}

	// 配置了但为空：谁都不信
	applyTrustedProxies(&conf.Server_REST{TrustedProxies: &conf.Server_REST_TrustedProxies{}})
	if got := clientIPVia("127.0.0.1", "1.1.1.1"); got != "127.0.0.1" {
		t.Fatalf("空列表应不信任任何代理, got %s", got)
	}
}
