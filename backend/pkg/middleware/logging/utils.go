package logging

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/third_party/go-utils/auditutil"
	"go-wind-admin/third_party/go-utils/geoip"
	"go-wind-admin/third_party/go-utils/geoip/geolite"
	"go-wind-admin/third_party/go-utils/jwtutil"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"github.com/mileusna/useragent"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"

	"go-wind-admin/pkg/jwt"
)

var ipClient, _ = geolite.NewClient()

// extractAuthToken 从JWT Token中提取用户信息
func extractAuthToken(htr *http.Transport) *authenticationV1.UserTokenPayload {
	authToken := htr.RequestHeader().Get(HeaderKeyAuthorization)
	if len(authToken) == 0 {
		return nil
	}

	jwtToken := strings.TrimPrefix(authToken, "Bearer ")

	claims, err := jwtutil.ParseJWTPayload(jwtToken)
	if err != nil {
		bLogger.GetLogger().Error(context.Background(), fmt.Sprintf("extractAuthToken ParseJWTPayload failed: %v", err))
		return nil
	}

	ut, err := jwt.NewUserTokenPayloadWithJwtMapClaims(claims)
	if err != nil {
		bLogger.GetLogger().Error(context.Background(), fmt.Sprintf("extractAuthToken NewUserTokenPayloadWithJwtMapClaims failed: %v", err))
		return nil
	}

	return ut
}

// getClientID 获取客户端ID
func getClientID(request *http.Request, userToken *authenticationV1.UserTokenPayload) string {
	if request == nil {
		return ""
	}

	// 我们可以自定义一个Header叫做：X-Client-ID。
	xci := request.Header.Get(HeaderKeyXClientIP)
	if xci != "" {
		return xci
	}

	// 从JWT Token中获取ClientID也是可行的。
	if userToken != nil {
		return userToken.GetClientId()
	}

	return ""
}

// getStatusCode 状态码
func getStatusCode(err error) (uint32, string, bool) {
	// 1. 信息响应 (100–199)
	// 2. 成功响应 (200–299)
	// 3. 重定向消息 (300–399)
	// 4. 客户端错误响应 (400–499)
	// 5. 服務端错误响应 (500–599)
	if se := errors.FromError(err); se != nil {
		return uint32(se.Code), se.Reason, se.Code < 400
	} else {
		return 200, "", true
	}
}

// clientIpToLocation 获取客户端IP的地理位置
func clientIpToLocation(ip string) *geoip.Result {
	res, err := ipClient.Query(ip)
	if err != nil {
		return nil
	}
	return &res
}

// fillDeviceInfo 填写设备信息
func fillDeviceInfo(htr *http.Transport, ut *authenticationV1.UserTokenPayload) (info *auditV1.DeviceInfo) {
	info = &auditV1.DeviceInfo{}

	userAgent := htr.RequestHeader().Get(HeaderKeyUserAgent)
	ua := useragent.Parse(userAgent)
	info.UserAgent = trans.Ptr(ua.String)

	var deviceName string
	if ua.Device != "" {
		deviceName = ua.Device
	} else {
		if ua.Desktop {
			deviceName = "PC"
		}
	}
	info.ClientName = trans.Ptr(deviceName)

	if ua.Desktop {
		info.DeviceType = trans.Ptr(auditV1.DeviceInfo_DESKTOP)
	} else if ua.Tablet {
		info.DeviceType = trans.Ptr(auditV1.DeviceInfo_TABLET)
	} else if ua.Mobile {
		info.DeviceType = trans.Ptr(auditV1.DeviceInfo_MOBILE)
	} else if ua.Bot {
		info.DeviceType = trans.Ptr(auditV1.DeviceInfo_BOT)
	} else {
		info.DeviceType = trans.Ptr(auditV1.DeviceInfo_OTHER)
	}

	info.BrowserVersion = trans.Ptr(ua.Version)
	info.BrowserName = trans.Ptr(ua.Name)

	info.OsName = trans.Ptr(ua.OS)
	info.OsVersion = trans.Ptr(ua.OSVersion)

	info.Platform = trans.Ptr(auditutil.DetectPlatformFromUA(userAgent))

	info.ClientId = trans.Ptr(getClientID(htr.Request(), ut))

	return
}

// fillGeoLocation 填写地理位置信息
func fillGeoLocation(clientIp string) (info *auditV1.GeoLocation) {
	info = &auditV1.GeoLocation{}

	result := clientIpToLocation(clientIp)
	if result == nil {
		return
	}

	info.CountryCode = trans.Ptr(result.Country)
	info.Province = trans.Ptr(result.Province)
	info.City = trans.Ptr(result.City)
	info.Isp = trans.Ptr(result.ISP)

	return
}
