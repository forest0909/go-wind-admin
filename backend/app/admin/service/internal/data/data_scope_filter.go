package data

import (
	"context"

	"entgo.io/ent/dialect/sql"

	"go-wind-admin/third_party/go-crud/viewer"

	"go-wind-admin/app/admin/service/internal/data/ent/userorgunit"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
)

// 用户、组织单元、登录/操作日志的数据范围过滤（见 docs/data_scope_design.md 第 4 节）。
//
// 这几张表不挂 ent 隐私策略：登录、站内信广播、组织子树维护、关联名称回填等内部流程
// 要读全量。所以只在对外接口的入口用 WithDataScope 标记 ctx，被标记的仓储读方法
// （List/Count/Get）才注入过滤谓词。按 id 改删先用标记过的 ctx 做一次 Get，越界即 404。

type dataScopeEnforcedKey struct{}

// WithDataScope 标记本次仓储读取要按调用者的数据范围过滤。
func WithDataScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, dataScopeEnforcedKey{}, true)
}

// resolvedDataScope 是调用者数据范围的并集：self=SELF 档，units=UNIT 类目标集。
type resolvedDataScope struct {
	uid   uint32
	self  bool
	units []uint32
}

// resolveDataScope 返回 nil 表示不过滤：ctx 未标记、系统上下文、或任一档为 ALL。
// 缺 viewer、无档位、显式 NONE 一律拒绝（与 rule.PermissionRule 一致，fail-closed）。
func resolveDataScope(ctx context.Context) (*resolvedDataScope, error) {
	if enforced, _ := ctx.Value(dataScopeEnforcedKey{}).(bool); !enforced {
		return nil, nil
	}
	vc, ok := viewer.FromContext(ctx)
	if !ok || vc == nil {
		return nil, adminV1.ErrorForbidden("missing viewer context")
	}
	if vc.IsSystemContext() {
		return nil, nil
	}

	rs := &resolvedDataScope{uid: uint32(vc.UserID())}
	valid := false
	for _, s := range vc.DataScope() {
		switch s.ScopeType {
		case viewer.ScopeTypeAll:
			return nil, nil
		case viewer.ScopeTypeNone:
			return nil, adminV1.ErrorForbidden("data access denied by data scope")
		case viewer.ScopeTypeSelf:
			rs.self = true
			valid = true
		case viewer.ScopeTypeUnit:
			valid = true
			for _, id := range s.TargetIDs {
				rs.units = append(rs.units, uint32(id))
			}
		}
	}
	if !valid {
		return nil, adminV1.ErrorForbidden("no data scope defined for current user")
	}
	return rs, nil
}

// usersInUnits 子查询：属于目标单元集中任一单元的用户 id。
func usersInUnits(units []uint32) *sql.Selector {
	t := sql.Table(userorgunit.Table)
	args := make([]any, 0, len(units))
	for _, id := range units {
		args = append(args, id)
	}
	return sql.Select(t.C(userorgunit.FieldUserID)).From(t).
		Where(sql.In(t.C(userorgunit.FieldOrgUnitID), args...))
}

// visibleUserColumn 生成"列值属于可见用户"的谓词：本人总是可见，
// 另加 UNIT 类目标集内任一单元的成员。SELF 档只剩本人。
func (rs *resolvedDataScope) visibleUserColumn(column string) func(*sql.Selector) {
	return func(s *sql.Selector) {
		preds := []*sql.Predicate{sql.EQ(s.C(column), rs.uid)}
		if len(rs.units) > 0 {
			preds = append(preds, sql.In(s.C(column), usersInUnits(rs.units)))
		}
		s.Where(sql.Or(preds...))
	}
}

// userScopeSelector 用户表（sys_users.id）的数据范围谓词；nil 表示不过滤。
func userScopeSelector(ctx context.Context) (func(*sql.Selector), error) {
	rs, err := resolveDataScope(ctx)
	if err != nil || rs == nil {
		return nil, err
	}
	return rs.visibleUserColumn("id"), nil
}

// operatorScopeSelector 日志表（user_id 为操作人）的数据范围谓词；nil 表示不过滤。
func operatorScopeSelector(ctx context.Context) (func(*sql.Selector), error) {
	rs, err := resolveDataScope(ctx)
	if err != nil || rs == nil {
		return nil, err
	}
	return rs.visibleUserColumn("user_id"), nil
}

// orgUnitScopeSelector 组织单元表的数据范围谓词；nil 表示不过滤。
// UNIT 类：单元在目标集内（及以下已在登录时展开）；SELF：本人所属单元。
func orgUnitScopeSelector(ctx context.Context) (func(*sql.Selector), error) {
	rs, err := resolveDataScope(ctx)
	if err != nil || rs == nil {
		return nil, err
	}
	return func(s *sql.Selector) {
		var preds []*sql.Predicate
		if len(rs.units) > 0 {
			args := make([]any, 0, len(rs.units))
			for _, id := range rs.units {
				args = append(args, id)
			}
			preds = append(preds, sql.In(s.C("id"), args...))
		}
		if rs.self {
			t := sql.Table(userorgunit.Table)
			preds = append(preds, sql.In(s.C("id"),
				sql.Select(t.C(userorgunit.FieldOrgUnitID)).From(t).
					Where(sql.EQ(t.C(userorgunit.FieldUserID), rs.uid))))
		}
		if len(preds) == 0 {
			// UNIT 类目标集为空且无 SELF：什么都看不到
			s.Where(sql.False())
			return
		}
		s.Where(sql.Or(preds...))
	}, nil
}
