package server

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-kratos/kratos/v2/transport/http"
)

type routeWalker interface {
	WalkRoute(fn http.WalkRouteFunc) error
}

// openAPIOperationsWithoutRoute 返回内嵌 openapi 里有、但 HTTP 服务器上没注册路由的
// 接口（"METHOD /path"，已排序）。
//
// 新模块漏了 rest_server.go 的注册调用时编译照过：openapi 照样进 Api 表、权限点照样生成，
// 请求却是 404。启动时比一次，把这种情况打到错误日志里。
func openAPIOperationsWithoutRoute(walker routeWalker, openapiData []byte) ([]string, error) {
	doc, err := openapi3.NewLoader().LoadFromData(openapiData)
	if err != nil {
		return nil, fmt.Errorf("load openapi: %w", err)
	}

	registered := make(map[string]bool)
	if err = walker.WalkRoute(func(info http.RouteInfo) error {
		registered[routeKey(info.Method, info.Path)] = true
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk routes: %w", err)
	}

	var missing []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			key := routeKey(method, path)
			if !registered[key] {
				missing = append(missing, key)
			}
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// pathParam 匹配路径参数。openapi 用 json 名（{typeName}），kratos 路由用 proto 名（{type_name}），
// 比较时一律归一成 {}。
var pathParam = regexp.MustCompile(`\{[^}]*\}`)

func routeKey(method, path string) string {
	return strings.ToUpper(method) + " /" + pathParam.ReplaceAllString(strings.TrimPrefix(path, "/"), "{}")
}
