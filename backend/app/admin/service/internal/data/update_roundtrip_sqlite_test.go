package data

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	configV1 "go-wind-admin/api/gen/go/config/service/v1"
	dictV1 "go-wind-admin/api/gen/go/dict/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	internalMessageV1 "go-wind-admin/api/gen/go/internal_message/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
	taskV1 "go-wind-admin/api/gen/go/task/service/v1"

	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/third_party/go-utils/password"
)

// 这组测试守的是"Update 的 set 回调是逐字段手写的"：updateMask 里带了、回调里漏接的字段，
// 接口照样 200，值却没写进去。新加字段只改 Create 不改 Update 是最常见的漏法。
//
// 做法：建一行 → 读回 → 把 DTO 上每个可写的标量/枚举/时间字段改成和当前值不同的值、
// 全部放进 updateMask → Update → 读回逐字段比对。
//
// 新增字段时这里会自动覆盖到：除非字段是 repeated / 嵌套消息（关联 ID 列表、子树等，
// 不经 set 回调写入），否则必须能 round-trip；确实不能经 Update 改的，在用例的 skip 里
// 写明原因。

// roundTripAlwaysSkip 所有资源共有、不经 Update 由调用方写入的字段。
var roundTripAlwaysSkip = map[string]string{
	"id":         "主键",
	"created_by": "创建时写入",
	"created_at": "创建时写入",
	"updated_at": "repo 取当前时间",
	"deleted_by": "软删除字段",
	"deleted_at": "软删除字段",
	"tenant_id":  "ent 层 Immutable",
}

type roundTripCase struct {
	name string
	// create 建一行，返回主键。
	create func(t *testing.T, ctx context.Context) uint32
	get    func(ctx context.Context, id uint32) (proto.Message, error)
	update func(ctx context.Context, id uint32, data proto.Message, mask *fieldmaskpb.FieldMask) error
	// skip 本资源不能经 Update 改的字段 → 原因。
	skip map[string]string
	// values 某些字段必须取特定合法值（外键等），在 create 之后求值。
	values func() map[string]any
}

func runUpdateRoundTrip(t *testing.T, ctx context.Context, tc roundTripCase) {
	t.Helper()
	id := tc.create(t, ctx)

	before, err := tc.get(ctx, id)
	require.NoError(t, err, "读回新建行")

	var values map[string]any
	if tc.values != nil {
		values = tc.values()
	}
	data := before.ProtoReflect().New().Interface()
	var paths []string
	fields := before.ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())
		if _, ok := roundTripAlwaysSkip[name]; ok {
			continue
		}
		if _, ok := tc.skip[name]; ok {
			continue
		}
		if fd.IsList() && fd.Kind() == protoreflect.StringKind {
			// repeated string（JSON 列）也走 set 回调，一并覆盖
		} else if fd.IsList() || fd.IsMap() || fd.ContainingOneof() != nil && !fd.HasOptionalKeyword() ||
			(fd.Kind() == protoreflect.MessageKind && fd.Message().FullName() != "google.protobuf.Timestamp") {
			t.Fatalf("%s.%s 是 %v 字段，round-trip 覆盖不到：在用例 skip 里写明它怎么写入", tc.name, name, fd.Kind())
		}
		setDifferent(t, data, fd, before.ProtoReflect().Get(fd), values)
		paths = append(paths, name)
	}
	sort.Strings(paths)

	err = tc.update(ctx, id, data, &fieldmaskpb.FieldMask{Paths: paths})
	require.NoError(t, err, "Update 带全部字段")

	after, err := tc.get(ctx, id)
	require.NoError(t, err, "读回更新后的行")

	var notWritten []string
	for _, p := range paths {
		fd := fields.ByName(protoreflect.Name(p))
		want := data.ProtoReflect().Get(fd)
		got := after.ProtoReflect().Get(fd)
		if !valueEqual(fd, want, got) {
			notWritten = append(notWritten, fmt.Sprintf("%s(want %v, got %v)", p, fmtValue(fd, want), fmtValue(fd, got)))
		}
	}
	if len(notWritten) > 0 {
		t.Errorf("%s.Update 返回成功但没写入: %s", tc.name, strings.Join(notWritten, ", "))
	}

	// 第二步：mask 只带一个字段（有 updated_by 就用它——Service 层每次都会追加），
	// DTO 里其余字段全填上新值，mask 之外的字段必须保持不变。回调里用 SetX(GetX())
	// 而不是 SetNillableX 时，被 mask 过滤成零值的字段会被写成空。
	if len(paths) == 0 {
		return
	}
	only := paths[0]
	for _, p := range paths {
		if p == "updated_by" {
			only = p
		}
	}
	probe := after.ProtoReflect().New().Interface()
	for _, p := range paths {
		fd := fields.ByName(protoreflect.Name(p))
		setDifferent(t, probe, fd, after.ProtoReflect().Get(fd), values)
	}
	partialMask := &fieldmaskpb.FieldMask{Paths: []string{only}}
	err = tc.update(ctx, id, probe, partialMask)
	require.NoError(t, err, "Update 只带 updated_by")

	final, err := tc.get(ctx, id)
	require.NoError(t, err, "读回部分更新后的行")
	var clobbered []string
	for _, p := range paths {
		if p == only {
			continue
		}
		fd := fields.ByName(protoreflect.Name(p))
		if !valueEqual(fd, after.ProtoReflect().Get(fd), final.ProtoReflect().Get(fd)) {
			clobbered = append(clobbered, fmt.Sprintf("%s(%v -> %v)", p, fmtValue(fd, after.ProtoReflect().Get(fd)), fmtValue(fd, final.ProtoReflect().Get(fd))))
		}
	}
	if len(clobbered) > 0 {
		t.Errorf("%s.Update 改了 updateMask 之外的字段: %s", tc.name, strings.Join(clobbered, ", "))
	}
}

func fmtValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	if fd.IsList() {
		items := make([]string, 0, v.List().Len())
		for i := 0; i < v.List().Len(); i++ {
			items = append(items, fmt.Sprint(v.List().Get(i).Interface()))
		}
		return "[" + strings.Join(items, " ") + "]"
	}
	return fmt.Sprint(v.Interface())
}

func valueEqual(fd protoreflect.FieldDescriptor, a, b protoreflect.Value) bool {
	if fd.IsList() {
		la, lb := a.List(), b.List()
		if la.Len() != lb.Len() {
			return false
		}
		for i := 0; i < la.Len(); i++ {
			if la.Get(i).Interface() != lb.Get(i).Interface() {
				return false
			}
		}
		return true
	}
	if fd.Kind() == protoreflect.MessageKind {
		return proto.Equal(a.Message().Interface(), b.Message().Interface())
	}
	return a.Interface() == b.Interface()
}

// setDifferent 把 msg 的 fd 设成与 cur 不同的值（values 里指定了就用指定值）。
func setDifferent(t *testing.T, msg proto.Message, fd protoreflect.FieldDescriptor, cur protoreflect.Value, values map[string]any) {
	t.Helper()
	if v, ok := values[string(fd.Name())]; ok {
		msg.ProtoReflect().Set(fd, toProtoValue(t, fd, v))
		return
	}
	if fd.IsList() { // repeated string：比当前多一个元素
		list := msg.ProtoReflect().Mutable(fd).List()
		for i := 0; i <= cur.List().Len(); i++ {
			list.Append(protoreflect.ValueOfString(fmt.Sprintf("rt_%s_%d", fd.Name(), i)))
		}
		return
	}
	msg.ProtoReflect().Set(fd, differentValue(t, fd, cur))
}

func toProtoValue(t *testing.T, fd protoreflect.FieldDescriptor, v any) protoreflect.Value {
	t.Helper()
	switch x := v.(type) {
	case protoreflect.EnumNumber:
		return protoreflect.ValueOfEnum(x)
	case *timestamppb.Timestamp:
		return protoreflect.ValueOfMessage(x.ProtoReflect())
	default:
		return protoreflect.ValueOf(v)
	}
}

// differentValue 生成和 cur 不同的合法值。
func differentValue(t *testing.T, fd protoreflect.FieldDescriptor, cur protoreflect.Value) protoreflect.Value {
	t.Helper()
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(cur.String() + "_rt" + string(fd.Name()))
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(!cur.Bool())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(int32(cur.Int()) + 7)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(cur.Int() + 7)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(uint32(cur.Uint()) + 7)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(cur.Uint() + 7)
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(cur.Float() + 7)
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(float32(cur.Float()) + 7)
	case protoreflect.EnumKind:
		values := fd.Enum().Values()
		for i := 0; i < values.Len(); i++ {
			n := values.Get(i).Number()
			upper := strings.ToUpper(string(values.Get(i).Name()))
			if n == 0 && (strings.Contains(upper, "UNSPECIFIED") || strings.Contains(upper, "INVALID")) {
				continue
			}
			if n != cur.Enum() {
				return protoreflect.ValueOfEnum(n)
			}
		}
	case protoreflect.MessageKind: // Timestamp
		ts := timestamppb.New(time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC))
		return protoreflect.ValueOfMessage(ts.ProtoReflect())
	}
	t.Fatalf("字段 %s 的类型 %v 没有可用的替换值", fd.Name(), fd.Kind())
	return protoreflect.Value{}
}

// TestRepoUpdateRoundTrip 每个 repo 的 Update 都要把 updateMask 里的每个字段写进库。
func TestRepoUpdateRoundTrip(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	cli := entClient.Client()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	dictTypeRepo := NewDictTypeRepoForTest(entClient)
	dictEntryRepo := NewDictEntryRepoForTest(entClient)
	languageRepo := NewLanguageRepoForTest(entClient)
	apiRepo := NewApiRepoForTest(entClient)
	menuRepo := NewMenuRepoForTest(entClient)
	permissionRepo := NewPermissionRepoForTest(entClient)
	permissionGroupRepo := NewPermissionGroupRepoForTest(entClient)
	roleRepo := NewRoleRepoForTest(entClient)
	positionRepo := NewPositionRepoForTest(entClient)
	orgUnitRepo := NewOrgUnitRepoForTest(entClient)
	taskRepo := NewTaskRepoForTest(entClient)
	configRepo := NewConfigRepoForTest(entClient, rdb)
	fileRepo := NewFileRepo(boot, entClient)
	messageRepo := NewInternalMessageRepoForTest(entClient)
	categoryRepo := NewInternalMessageCategoryRepoForTest(entClient)
	recipientRepo := NewInternalMessageRecipientRepoForTest(entClient)
	credentialRepo := NewUserCredentialRepoForTest(entClient, password.NewBCryptCrypto(), configRepo)
	userRepo := NewUserRepo(boot, entClient,
		NewUserRoleRepo(boot, entClient), NewUserOrgUnitRepo(boot, entClient), NewUserPositionRepo(boot, entClient), nil)

	menuParent := cli.Menu.Create().SetName("rt_parent").SetPath("/rt_parent").SaveX(ctx).ID
	groupParent := cli.PermissionGroup.Create().SetName("rt_parent").SetModule("rt_parent").SaveX(ctx).ID
	orgParent := cli.OrgUnit.Create().SetName("rt_parent").SetCode("rt_parent").SaveX(ctx).ID
	positionOrgUnit := cli.OrgUnit.Create().SetName("rt_pos_org").SetCode("rt_pos_org").SaveX(ctx).ID

	seq := 0
	uniq := func(prefix string) string { seq++; return fmt.Sprintf("%s%d", prefix, seq) }

	cases := []roundTripCase{
		{
			name: "DictType",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.DictType.Create().SetTypeCode(uniq("rt_type")).SetTypeName("n").SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return dictTypeRepo.Get(ctx, &dictV1.GetDictTypeRequest{QueryBy: &dictV1.GetDictTypeRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return dictTypeRepo.Update(ctx, &dictV1.UpdateDictTypeRequest{Id: id, Data: d.(*dictV1.DictType), UpdateMask: m})
			},
			skip: map[string]string{"type_code": "schema Immutable"},
		},
		{
			name: "DictEntry",
			create: func(t *testing.T, ctx context.Context) uint32 {
				typ := cli.DictType.Create().SetTypeCode(uniq("rt_et")).SetTypeName("n").SaveX(ctx)
				return cli.DictEntry.Create().SetDictTypeID(typ.ID).SetEntryValue(uniq("v")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return dictEntryRepo.Get(ctx, &dictV1.GetDictEntryRequest{QueryBy: &dictV1.GetDictEntryRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return dictEntryRepo.Update(ctx, &dictV1.UpdateDictEntryRequest{Id: id, Data: d.(*dictV1.DictEntry), UpdateMask: m})
			},
			skip: map[string]string{
				"i18n":         "多语言子表，由 DictEntryI18n 接口写入",
				"current_i18n": "读侧按当前语言回填",
				"type_id":      "条目不跨字典类型移动，Update 不改归属",
			},
		},
		{
			name: "Language",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Language.Create().SetLanguageCode(uniq("rt-")).SetLanguageName("n").SetNativeName("n").SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return languageRepo.Get(ctx, &dictV1.GetLanguageRequest{QueryBy: &dictV1.GetLanguageRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return languageRepo.Update(ctx, &dictV1.UpdateLanguageRequest{Id: id, Data: d.(*dictV1.Language), UpdateMask: m})
			},
			skip: map[string]string{"language_code": "schema Immutable"},
		},
		{
			name: "Api",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Api.Create().SetPath(uniq("/rt/")).SetMethod("GET").SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return apiRepo.Get(ctx, &permissionV1.GetApiRequest{QueryBy: &permissionV1.GetApiRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return apiRepo.Update(ctx, &permissionV1.UpdateApiRequest{Id: id, Data: d.(*permissionV1.Api), UpdateMask: m})
			},
		},
		{
			name: "Menu",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Menu.Create().SetName(uniq("rtmenu")).SetPath(uniq("/rtm")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return menuRepo.Get(ctx, &permissionV1.GetMenuRequest{QueryBy: &permissionV1.GetMenuRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return menuRepo.Update(ctx, &permissionV1.UpdateMenuRequest{Id: id, Data: d.(*permissionV1.Menu), UpdateMask: m})
			},
			skip: map[string]string{
				"meta":     "嵌套 JSON，按 meta.xxx 子路径由 updateMetaField 写入",
				"children": "树形，读侧拼装",
			},
			values: func() map[string]any { return map[string]any{"parent_id": menuParent} },
		},
		{
			name: "Permission",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Permission.Create().SetName("n").SetCode(uniq("rt:")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return permissionRepo.Get(ctx, &permissionV1.GetPermissionRequest{QueryBy: &permissionV1.GetPermissionRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return permissionRepo.Update(ctx, &permissionV1.UpdatePermissionRequest{Id: id, Data: d.(*permissionV1.Permission), UpdateMask: m})
			},
			skip: map[string]string{
				"menu_ids":   "关联表 sys_permission_menus，Update 里单独同步",
				"api_ids":    "关联表 sys_permission_apis，Update 里单独同步",
				"group_name": "读侧回填",
			},
		},
		{
			name: "PermissionGroup",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.PermissionGroup.Create().SetName(uniq("rtg")).SetModule(uniq("m")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return permissionGroupRepo.Get(ctx, &permissionV1.GetPermissionGroupRequest{QueryBy: &permissionV1.GetPermissionGroupRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return permissionGroupRepo.Update(ctx, &permissionV1.UpdatePermissionGroupRequest{Id: id, Data: d.(*permissionV1.PermissionGroup), UpdateMask: m})
			},
			skip:   map[string]string{"children": "树形，读侧拼装", "path": "物化路径，按 parent_id 重算"},
			values: func() map[string]any { return map[string]any{"parent_id": groupParent} },
		},
		{
			name: "Role",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Role.Create().SetName(uniq("rtr")).SetCode(uniq("rtr")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return roleRepo.Get(ctx, &permissionV1.GetRoleRequest{QueryBy: &permissionV1.GetRoleRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return roleRepo.Update(ctx, &permissionV1.UpdateRoleRequest{Id: id, Data: d.(*permissionV1.Role), UpdateMask: m})
			},
			skip: map[string]string{
				"permissions":       "关联表，由 ReplacePermissions 同步",
				"org_units":         "关联表，由 ReplaceOrgUnits 同步",
				"field_permissions": "关联表 sys_role_field_permissions，单独同步",
			},
		},
		{
			name: "Position",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Position.Create().SetName(uniq("rtp")).SetCode(uniq("rtp")).SetOrgUnitID(orgParent).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return positionRepo.Get(ctx, &identityV1.GetPositionRequest{QueryBy: &identityV1.GetPositionRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return positionRepo.Update(ctx, &identityV1.UpdatePositionRequest{Id: id, Data: d.(*identityV1.Position), UpdateMask: m})
			},
			skip: map[string]string{
				"org_unit_name":            "读侧回填",
				"reports_to_position_name": "读侧回填",
			},
			values: func() map[string]any { return map[string]any{"org_unit_id": positionOrgUnit} },
		},
		{
			name: "OrgUnit",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.OrgUnit.Create().SetName(uniq("rto")).SetCode(uniq("rto")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return orgUnitRepo.Get(ctx, &identityV1.GetOrgUnitRequest{QueryBy: &identityV1.GetOrgUnitRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return orgUnitRepo.Update(ctx, &identityV1.UpdateOrgUnitRequest{Id: id, Data: d.(*identityV1.OrgUnit), UpdateMask: m})
			},
			skip: map[string]string{
				"attributes":        "ent schema 没有这一列，proto 字段从未落库（见 dx-guards 报告）",
				"children":          "树形，读侧拼装",
				"path":              "物化路径，按 parent_id 重算",
				"leader_name":       "读侧回填",
				"contact_user_name": "读侧回填",
			},
			values: func() map[string]any { return map[string]any{"parent_id": orgParent} },
		},
		{
			name: "Task",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.Task.Create().SetTypeName(uniq("rt_task")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return taskRepo.Get(ctx, &taskV1.GetTaskRequest{QueryBy: &taskV1.GetTaskRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				_, err := taskRepo.Update(ctx, &taskV1.UpdateTaskRequest{Id: id, Data: d.(*taskV1.Task), UpdateMask: m})
				return err
			},
			skip: map[string]string{"task_options": "嵌套消息，整体存 JSON 列"},
		},
		{
			name: "Config",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.SysConfig.Create().SetKey(uniq("rt.key")).SetValue("v").SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return configRepo.Get(ctx, &configV1.GetConfigRequest{QueryBy: &configV1.GetConfigRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return configRepo.Update(ctx, &configV1.UpdateConfigRequest{Id: id, Data: d.(*configV1.Config), UpdateMask: m})
			},
		},
		{
			name: "File",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.File.Create().SetFileName(uniq("rt.txt")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return fileRepo.Get(ctx, &storageV1.GetFileRequest{QueryBy: &storageV1.GetFileRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return fileRepo.Update(ctx, &storageV1.UpdateFileRequest{Id: id, Data: d.(*storageV1.File), UpdateMask: m})
			},
		},
		{
			name: "InternalMessageCategory",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.InternalMessageCategory.Create().SetName(uniq("rtc")).SetCode(uniq("rtc")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return categoryRepo.Get(ctx, &internalMessageV1.GetInternalMessageCategoryRequest{QueryBy: &internalMessageV1.GetInternalMessageCategoryRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return categoryRepo.Update(ctx, &internalMessageV1.UpdateInternalMessageCategoryRequest{Id: id, Data: d.(*internalMessageV1.InternalMessageCategory), UpdateMask: m})
			},
		},
		{
			name: "InternalMessage",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.InternalMessage.Create().SetTitle("t").SetContent("c").SetSenderID(1).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return messageRepo.Get(ctx, &internalMessageV1.GetInternalMessageRequest{QueryBy: &internalMessageV1.GetInternalMessageRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return messageRepo.Update(ctx, &internalMessageV1.UpdateInternalMessageRequest{Id: id, Data: d.(*internalMessageV1.InternalMessage), UpdateMask: m})
			},
			skip: map[string]string{"category_name": "读侧回填", "sender_name": "读侧回填"},
		},
		{
			name: "InternalMessageRecipient",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.InternalMessageRecipient.Create().SetMessageID(1).SetRecipientUserID(1).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return recipientRepo.Get(ctx, &internalMessageV1.GetInternalMessageRecipientRequest{QueryBy: &internalMessageV1.GetInternalMessageRecipientRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return recipientRepo.Update(ctx, &internalMessageV1.UpdateInternalMessageRecipientRequest{Id: id, Data: d.(*internalMessageV1.InternalMessageRecipient), UpdateMask: m})
			},
			skip: map[string]string{
				"title":      "读侧从消息表回填",
				"content":    "读侧从消息表回填",
				"updated_by": "表没有 updated_by 列",
			},
		},
		{
			name: "UserCredential",
			create: func(t *testing.T, ctx context.Context) uint32 {
				return cli.UserCredential.Create().SetUserID(1).SetIdentifier(uniq("rtu")).SaveX(ctx).ID
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return credentialRepo.Get(ctx, &authenticationV1.GetUserCredentialRequest{QueryBy: &authenticationV1.GetUserCredentialRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return credentialRepo.Update(ctx, &authenticationV1.UpdateUserCredentialRequest{Id: id, Data: d.(*authenticationV1.UserCredential), UpdateMask: m})
			},
			skip: map[string]string{
				"credential": "写入前按凭证类型做口令策略校验并哈希，读回不是明文；由认证相关测试覆盖",
				"user_id":    "凭证不换绑用户",
				"updated_by": "表没有 updated_by 列",
			},
		},
		{
			name: "User",
			create: func(t *testing.T, ctx context.Context) uint32 {
				u, err := userRepo.Create(ctx, &identityV1.CreateUserRequest{Data: &identityV1.User{Username: trans.Ptr(uniq("rtuser"))}})
				require.NoError(t, err)
				return u.GetId()
			},
			get: func(ctx context.Context, id uint32) (proto.Message, error) {
				return userRepo.Get(ctx, &identityV1.GetUserRequest{QueryBy: &identityV1.GetUserRequest_Id{Id: id}})
			},
			update: func(ctx context.Context, id uint32, d proto.Message, m *fieldmaskpb.FieldMask) error {
				return userRepo.Update(ctx, &identityV1.UpdateUserRequest{Id: id, Data: d.(*identityV1.User), UpdateMask: m})
			},
			skip: map[string]string{
				"username":       "schema Immutable",
				"org_unit_name":  "读侧回填",
				"position_name":  "读侧回填",
				"role_ids":       "关联表，Update 里按快照同步",
				"org_unit_ids":   "关联表，Update 里按快照同步",
				"position_ids":   "关联表，Update 里按快照同步",
				"roles":          "读侧回填",
				"role_names":     "读侧回填",
				"org_unit_names": "读侧回填",
				"position_names": "读侧回填",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runUpdateRoundTrip(t, ctx, tc) })
	}
}
