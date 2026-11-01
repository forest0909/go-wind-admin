package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/third_party/go-utils/trans"
)

// TestFilterMenuTreeByModules 子页面按路由归属模块过滤，子项剔光的目录一并去掉；
// 种子里 module 为空的仪表盘页、记成 SYSTEM 的字典页都按路由判断。
func TestFilterMenuTreeByModules(t *testing.T) {
	menu := func(path string, module *identityV1.Module, children ...*permissionV1.Menu) *permissionV1.Menu {
		return &permissionV1.Menu{Path: trans.Ptr(path), Module: module, Children: children}
	}
	system := trans.Ptr(identityV1.Module_SYSTEM)
	tree := []*permissionV1.Menu{
		menu("/dashboard", nil, menu("analytics", nil), menu("workspace", nil)),
		menu("/opm", nil, menu("users", trans.Ptr(identityV1.Module_OPM))),
		menu("/system", nil, menu("dict", system), menu("online-sessions", system)),
		menu("/custom", nil, menu("page", nil)),
	}

	got := filterMenuTreeByModules(tree, "", map[identityV1.Module]bool{
		identityV1.Module_OPM:  true,
		identityV1.Module_DICT: true,
	})

	paths := map[string][]string{}
	for _, m := range got {
		for _, c := range m.Children {
			paths[m.GetPath()] = append(paths[m.GetPath()], c.GetPath())
		}
	}
	require.Equal(t, map[string][]string{
		"/opm":    {"users"},
		"/system": {"dict"},
		"/custom": {"page"},
	}, paths)
}
