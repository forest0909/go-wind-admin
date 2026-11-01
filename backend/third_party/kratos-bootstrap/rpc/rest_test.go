package rpc

import (
	"testing"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

// 底层 http.Server 必须带读超时和空闲超时，且不设写超时（下载、导出要长时间写）。
func TestCreateRestServer_SetsSocketTimeouts(t *testing.T) {
	srv, err := CreateRestServer(&conf.Bootstrap{Server: &conf.Server{Rest: &conf.Server_REST{Addr: "127.0.0.1:0"}}})
	if err != nil {
		t.Fatal(err)
	}
	if srv.ReadHeaderTimeout != restReadHeaderTimeout || srv.ReadTimeout != restReadTimeout || srv.IdleTimeout != restIdleTimeout {
		t.Fatalf("unexpected timeouts: header=%v read=%v idle=%v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout must stay 0, got %v", srv.WriteTimeout)
	}
}
