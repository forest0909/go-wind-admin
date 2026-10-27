// utils_extra_test.go —— utils.go 留存函数的取值与纯函数测试。
//
// 覆盖内容：
//  1. getClientID：X-Client-ID 头与令牌 ClientId 的来源优先级；
//  2. getStatusCode：nil 错误、kratos 错误（code/reason 透传、<400 记成功）、
//     普通错误（500/空 reason/失败）；
//  3. clientIpToLocation / fillGeoLocation：私网 IP（局域网归一）、
//     非法 IP（空结果）；
//  4. fillDeviceInfo 的空 Transport 来源（全空设备信息；平台判定走
//     auditutil.DetectPlatformFromUA）。
//
// 其余取值与纯函数（真实 IP / 请求 ID / 用户名提取 / 私网判定 / 平台
// 启发式 / ECDSA 密钥与 DER 编码）已迁 auditutil 库，由库内测试覆盖。
package logging

import (
	"errors"
	nethttp "net/http"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/auditutil"
	"go-wind-admin/third_party/go-utils/trans"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

// newHeaderOnlyRequest 构造仅带指定头的轻量请求（无 body）。
func newHeaderOnlyRequest(headers map[string]string) *nethttp.Request {
	req, _ := nethttp.NewRequest(nethttp.MethodGet, "/probe", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// TestGetClientID 表驱动验证客户端 ID 的来源：
// X-Client-ID 头优先，其次令牌 ClientId；nil 请求归一空串。
func TestGetClientID(t *testing.T) {
	ut := &authenticationV1.UserTokenPayload{ClientId: trans.Ptr("cli-token")}

	t.Run("nil请求", func(t *testing.T) {
		assert.Empty(t, getClientID(nil, ut))
	})

	t.Run("无来源", func(t *testing.T) {
		assert.Empty(t, getClientID(newHeaderOnlyRequest(nil), nil))
	})

	t.Run("头优先于令牌", func(t *testing.T) {
		req := newHeaderOnlyRequest(map[string]string{HeaderKeyXClientIP: "cli-hdr"})
		assert.Equal(t, "cli-hdr", getClientID(req, ut))
	})

	t.Run("令牌来源", func(t *testing.T) {
		assert.Equal(t, "cli-token", getClientID(newHeaderOnlyRequest(nil), ut))
	})
}

// TestGetStatusCode 表驱动验证错误到状态三元组（code/reason/success）的映射：
// nil → 200/空/成功；kratos 错误透传 code 与 reason、<400 记成功；
// 普通错误按 500/空 reason/失败处理。
func TestGetStatusCode(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCode   uint32
		wantReason string
		wantOK     bool
	}{
		{"nil错误", nil, 200, "", true},
		{"kratos客户端错误", kerrors.New(403, "TEST_FORBIDDEN", "forbidden for test"), 403, "TEST_FORBIDDEN", false},
		{"kratos重定向记成功", kerrors.New(302, "TEST_FOUND", "moved"), 302, "TEST_FOUND", true},
		{"kratos信息类记成功", kerrors.New(199, "TEST_INFO", "info"), 199, "TEST_INFO", true},
		{"普通错误", errors.New("plain boom"), 500, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, reason, ok := getStatusCode(tc.err)
			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantReason, reason)
			assert.Equal(t, tc.wantOK, ok)
		})
	}
}

// TestClientIpToLocation 验证地理解析结果的两个分支：
// 私网 IP → 内建局域网结果；非法 IP → nil。
func TestClientIpToLocation(t *testing.T) {
	res := clientIpToLocation("127.0.0.1")
	require.NotNil(t, res)
	assert.Equal(t, "局域网", res.Country)
	assert.Equal(t, "局域网", res.Province)
	assert.Equal(t, "局域网", res.City)

	assert.Nil(t, clientIpToLocation("not-an-ip"))
}

// TestFillGeoLocation 验证地理位置填充：非法/空 IP 得到全空结构体
// （不 panic、不 nil）；私网 IP 填内建局域网字段。
func TestFillGeoLocation(t *testing.T) {
	for _, ip := range []string{"", "not-an-ip"} {
		info := fillGeoLocation(ip)
		require.NotNil(t, info, "地理位置结构必须非 nil")
		assert.Empty(t, info.GetCountryCode())
		assert.Empty(t, info.GetProvince())
		assert.Empty(t, info.GetCity())
		assert.Empty(t, info.GetIsp())
	}

	info := fillGeoLocation("127.0.0.1")
	require.NotNil(t, info)
	assert.Equal(t, "局域网", info.GetCountryCode())
	assert.Equal(t, "局域网", info.GetProvince())
	assert.Equal(t, "局域网", info.GetCity())
}

// TestFillDeviceInfoEmptyTransport 验证空 Transport（无请求头可读）时
// 设备信息为全空解析：UA 空、设备类型 OTHER、平台 Other、ClientId 空。
func TestFillDeviceInfoEmptyTransport(t *testing.T) {
	info := fillDeviceInfo(&khttp.Transport{}, nil)
	require.NotNil(t, info)
	assert.Empty(t, info.GetUserAgent())
	assert.Empty(t, info.GetClientName())
	assert.Equal(t, auditV1.DeviceInfo_OTHER, info.GetDeviceType())
	assert.Empty(t, info.GetBrowserName())
	assert.Empty(t, info.GetBrowserVersion())
	assert.Empty(t, info.GetOsName())
	assert.Empty(t, info.GetOsVersion())
	assert.Equal(t, auditutil.PlatformOther, info.GetPlatform())
	assert.Empty(t, info.GetClientId())
}
