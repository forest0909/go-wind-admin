package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/cmd/server/assets"
	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/utils/converter"
	goconv "go-wind-admin/third_party/go-utils/converter"
)

// reactSrcDir 前端源码目录（相对本包）/ React sources relative to this package.
const reactSrcDir = "../../../../../../frontend/admin/react/src"

var (
	buttonCodeRe   = regexp.MustCompile(`\bcode:\s*'([a-z0-9-]+:[a-z]+)'`)
	accessButtonRe = regexp.MustCompile(`(?s)<AccessButton\b[^>]*?\bresource="([a-z0-9-]+)"[^>]*?\baction="([a-z]+)"`)
)

// actionsWithoutPage 允许没有同名页面的操作资源及原因。这些操作在角色页平铺在模块组下，能授权但不挂页面。
// 新业务别往这里加，先让页面路径和接口资源名对上；实在是一个页面里的子资源才登记，并写清原因。
var actionsWithoutPage = map[string]string{
	"tenant":           "租户模块菜单已隐藏；页面路径是 members（member:view），接口是 /tenants",
	"permission":       "权限点页（codes，code:view）已从菜单隐藏，只给平台管理员排查用",
	"permission-group": "权限点页里的分组子列表",
	"plan-quota":       "套餐页（plan:view）里的配额子列表；租户模块菜单已隐藏",
}

// TestPermissionCodeConsistency 前端按钮编码、接口派生编码、菜单派生页面编码三方对齐。
// 编码全靠路径推导，写错不会编译报错，只会让按钮在角色页挂不到页面下、或者谁都授不了权；
// 平台管理员放行一切，用 admin 自测也看不出来，所以在这里卡住。
func TestPermissionCodeConsistency(t *testing.T) {
	if _, err := os.Stat(reactSrcDir); err != nil {
		t.Skipf("前端源码不在 %s，跳过: %v", reactSrcDir, err)
	}

	apiCodes := openAPIPermissionCodes(t)
	pageCodes := defaultMenuPageCodes()
	buttons := frontendButtonCodes(t)
	require.NotEmpty(t, buttons, "没扫到任何按钮编码，检查正则是否还匹配 TableActions/AccessButton 的写法")

	var problems []string
	for _, code := range sortedKeys(buttons) {
		where := strings.Join(buttons[code], ", ")
		if !apiCodes[code] {
			problems = append(problems, code+" ("+where+")：openapi.yaml 里没有接口能生成这个编码，"+
				"检查资源名/动作；接口挂在模块前缀下时在 apiResourceAliases 登记")
			continue
		}
		resource := code[:strings.LastIndex(code, ":")]
		if _, ok := actionsWithoutPage[resource]; !ok && !pageCodes[resource+":view"] {
			problems = append(problems, code+" ("+where+")：没有菜单派生出 "+resource+":view，"+
				"角色页里这个操作挂不到页面下；检查 constants.DefaultMenus 的页面路径（去掉首段、转单数后应为 "+resource+"）")
		}
	}
	for resource := range actionsWithoutPage {
		if pageCodes[resource+":view"] {
			problems = append(problems, resource+"：已经有 "+resource+":view 页面，从 actionsWithoutPage 删掉")
		}
	}
	require.Empty(t, problems, "权限编码对不上：\n%s", strings.Join(problems, "\n"))
}

// openAPIPermissionCodes 用同步时的同一套转换器从内嵌 openapi.yaml 推出全部接口编码
func openAPIPermissionCodes(t *testing.T) map[string]bool {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(assets.OpenApiData)
	require.NoError(t, err)
	conv := goconv.NewApiPermissionConverter().WithResourceAliases(apiResourceAliases)
	codes := make(map[string]bool)
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			if code := conv.ConvertCodeByPath(method, path); code != "" {
				codes[code] = true
			}
		}
	}
	return codes
}

// defaultMenuPageCodes 默认菜单派生的页面编码（与 SyncPermissions 同一套路径拼接和转换）
func defaultMenuPageCodes() map[string]bool {
	menus := make([]*permissionV1.Menu, 0, len(constants.DefaultMenus))
	for _, m := range constants.DefaultMenus {
		menus = append(menus, proto.Clone(m).(*permissionV1.Menu))
	}
	conv := converter.NewMenuPermissionConverter()
	conv.ComposeMenuPaths(menus)
	codes := make(map[string]bool)
	for _, m := range menus {
		if code := conv.ConvertCode(m.GetPath(), m.GetName(), m.GetType()); code != "" {
			codes[code] = true
		}
	}
	return codes
}

// frontendButtonCodes 扫前端页面里 TableActions 的 code 和 AccessButton 的 resource/action，编码 → 出现位置
func frontendButtonCodes(t *testing.T) map[string][]string {
	t.Helper()
	found := make(map[string][]string)
	err := filepath.WalkDir(reactSrcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "generated" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".ts") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(reactSrcDir, path)
		for _, m := range buttonCodeRe.FindAllStringSubmatch(string(content), -1) {
			found[m[1]] = append(found[m[1]], rel)
		}
		for _, m := range accessButtonRe.FindAllStringSubmatch(string(content), -1) {
			code := m[1] + ":" + m[2]
			found[code] = append(found[code], rel)
		}
		return nil
	})
	require.NoError(t, err)
	return found
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
