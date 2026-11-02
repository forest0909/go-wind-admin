package data

import (
	"context"

	"slices"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	entCrud "go-wind-admin/third_party/go-crud/entgo"
	"go-wind-admin/third_party/go-crud/pagination"

	"go-wind-admin/third_party/go-utils/copierutil"
	"go-wind-admin/third_party/go-utils/mapper"
	"go-wind-admin/third_party/go-utils/sliceutil"
	"go-wind-admin/third_party/go-utils/timeutil"
	"go-wind-admin/third_party/go-utils/trans"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/predicate"
	"go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/usercredential"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
)

type UserRepo interface {
	List(ctx context.Context, req *paginationV1.PagingRequest) (*identityV1.ListUserResponse, error)

	Get(ctx context.Context, req *identityV1.GetUserRequest) (*identityV1.User, error)

	Create(ctx context.Context, req *identityV1.CreateUserRequest) (*identityV1.User, error)
	CreateWithTx(ctx context.Context, tx *ent.Tx, data *identityV1.User) (*identityV1.User, error)
	// BeginTx 开启 ent 事务。提交和回滚由调用方负责，不要和 Create 叠用。
	BeginTx(ctx context.Context) (*ent.Tx, error)

	Update(ctx context.Context, req *identityV1.UpdateUserRequest) error

	Delete(ctx context.Context, req *identityV1.DeleteUserRequest) error

	Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error)

	UserExists(ctx context.Context, req *identityV1.UserExistsRequest) (*identityV1.UserExistsResponse, error)

	AssignUserRole(ctx context.Context, data *permissionV1.UserRole) error
	AssignUserRoles(ctx context.Context, userID uint32, datas []*permissionV1.UserRole) error

	AssignUserOrgUnit(ctx context.Context, data *identityV1.UserOrgUnit) error
	AssignUserOrgUnits(ctx context.Context, userID uint32, datas []*identityV1.UserOrgUnit) error

	AssignUserPosition(ctx context.Context, data *identityV1.UserPosition) error
	AssignUserPositions(ctx context.Context, userID uint32, datas []*identityV1.UserPosition) error

	ListUsersByIds(ctx context.Context, ids []uint32) ([]*identityV1.User, error)

	ListRoleIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error)

	ListPositionIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error)

	ListOrgUnitIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error)

	ListUserRelationIDs(ctx context.Context, userID uint32) (roleIDs []uint32, positionIDs []uint32, orgUnitIDs []uint32, err error)

	// CountByTenantIDs 批量统计各租户下的用户数，用于回填 tenant.member_count。
	CountByTenantIDs(ctx context.Context, tenantIDs []uint32) (map[uint32]int, error)

	// FindUsernameByIdentifier 登录 identifier 智能解析：含 @ 按 email 查、
	// 纯数字按 mobile 查，其余原样视为 username 返回（userId=0 表示未反查）。
	// mobile 在租户内非唯一，命中多行时返回错误（数据歧义应拒绝登录而非随机取一）。
	FindUsernameByIdentifier(ctx context.Context, tenantID uint32, identifier string) (username string, userId uint32, err error)
}

type userRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper

	mapper          *mapper.CopierMapper[identityV1.User, ent.User]
	genderConverter *mapper.EnumTypeConverter[identityV1.User_Gender, user.Gender]
	statusConverter *mapper.EnumTypeConverter[identityV1.User_Status, user.Status]

	repository *entCrud.Repository[
		ent.UserQuery, ent.UserSelect,
		ent.UserCreate, ent.UserCreateBulk,
		ent.UserUpdate, ent.UserUpdateOne,
		ent.UserDelete,
		predicate.User,
		identityV1.User, ent.User,
	]

	userRoleRepo     *UserRoleRepo
	userOrgUnitRepo  *UserOrgUnitRepo
	userPositionRepo *UserPositionRepo

	membershipRepo *MembershipRepo
}

func NewUserRepo(
	ctx *bootstrap.Context,
	entClient *entCrud.EntClient[*ent.Client],
	userRoleRepo *UserRoleRepo,
	userOrgUnitRepo *UserOrgUnitRepo,
	userPositionRepo *UserPositionRepo,
	membershipRepo *MembershipRepo,
) UserRepo {
	repo := &userRepo{
		log:              ctx.NewLoggerHelper("user/repo/admin-service"),
		entClient:        entClient,
		mapper:           mapper.NewCopierMapper[identityV1.User, ent.User](),
		genderConverter:  mapper.NewEnumTypeConverter[identityV1.User_Gender, user.Gender](identityV1.User_Gender_name, identityV1.User_Gender_value),
		statusConverter:  mapper.NewEnumTypeConverter[identityV1.User_Status, user.Status](identityV1.User_Status_name, identityV1.User_Status_value),
		userRoleRepo:     userRoleRepo,
		userOrgUnitRepo:  userOrgUnitRepo,
		userPositionRepo: userPositionRepo,
		membershipRepo:   membershipRepo,
	}

	repo.init()

	return repo
}

func (r *userRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.UserQuery, ent.UserSelect,
		ent.UserCreate, ent.UserCreateBulk,
		ent.UserUpdate, ent.UserUpdateOne,
		ent.UserDelete,
		predicate.User,
		identityV1.User, ent.User,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.genderConverter.NewConverterPair())
	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
}

// Count 统计用户数量
func (r *userRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error) {
	builder := r.entClient.Client().User.Query()
	scopeSel, err := userScopeSelector(ctx)
	if err != nil {
		r.log.Errorf(ctx, "resolve data scope failed: %v", err)
		return 0, err
	}
	if scopeSel != nil {
		builder.Where(predicate.User(scopeSel))
	}

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, identityV1.ErrorInternalServerError("query count failed")
	}

	return count, nil
}

// queryUserIDsByRelationIDs 根据关联关系查询用户ID列表
func (r *userRepo) queryUserIDsByRelationIDs(ctx context.Context, roleIDs []uint32, orgUnitIDs []uint32, positionIDs []uint32) ([]uint32, error) {
	if len(roleIDs) == 0 && len(orgUnitIDs) == 0 && len(positionIDs) == 0 {
		return nil, nil
	}

	switch constants.DefaultUserTenantRelationType {
	default:
		fallthrough
	case constants.UserTenantRelationOneToOne:
		return r.queryUserIDsByRelationIDsUserTenantRelationOneToOne(ctx, roleIDs, orgUnitIDs, positionIDs)
	case constants.UserTenantRelationOneToMany:
		return r.queryUserIDsByRelationIDsUserTenantRelationOneToMany(ctx, roleIDs, orgUnitIDs, positionIDs)
	}
}

// queryUserIDsByRelationIDsUserTenantRelationOneToMany 根据关联关系一对多查询用户ID列表
func (r *userRepo) queryUserIDsByRelationIDsUserTenantRelationOneToMany(ctx context.Context, roleIDs []uint32, orgUnitIDs []uint32, positionIDs []uint32) ([]uint32, error) {
	if len(roleIDs) == 0 && len(orgUnitIDs) == 0 && len(positionIDs) == 0 {
		return nil, nil
	}

	var err error

	var orgUnitUserIDs []uint32
	var positionUserIDs []uint32
	var roleUserIDs []uint32

	if len(orgUnitIDs) > 0 {
		orgUnitUserIDs, err = r.membershipRepo.ListUserIDsByOrgUnitIDs(ctx, orgUnitIDs, false)
		if err != nil {
			return nil, err
		}
	}
	if len(positionIDs) > 0 {
		positionUserIDs, err = r.membershipRepo.ListUserIDsByPositionIDs(ctx, positionIDs, false)
		if err != nil {
			return nil, err
		}
	}
	if len(roleIDs) > 0 {
		roleUserIDs, err = r.membershipRepo.ListUserIDsByRoleIDs(ctx, roleIDs, false)
		if err != nil {
			return nil, err
		}
	}

	// 收集所有非空列表用于求交集
	lists := make([][]uint32, 0, 3)
	if orgUnitUserIDs != nil {
		lists = append(lists, orgUnitUserIDs)
	}
	if positionUserIDs != nil {
		lists = append(lists, positionUserIDs)
	}
	if roleUserIDs != nil {
		lists = append(lists, roleUserIDs)
	}

	// 如果没有任何实际列表（例如对应 ids 为空导致查询未执行），返回空
	if len(lists) == 0 {
		return []uint32{}, nil
	}

	// 逐步求交集
	result := lists[0]
	for i := 1; i < len(lists); i++ {
		result = sliceutil.Intersection(result, lists[i])
		if len(result) == 0 {
			break
		}
	}

	return result, nil
}

// queryUserIDsByRelationIDsUserTenantRelationOneToOne 根据关联关系一对一查询用户ID列表
func (r *userRepo) queryUserIDsByRelationIDsUserTenantRelationOneToOne(ctx context.Context, roleIDs []uint32, orgUnitIDs []uint32, positionIDs []uint32) ([]uint32, error) {
	if len(roleIDs) == 0 && len(orgUnitIDs) == 0 && len(positionIDs) == 0 {
		return nil, nil
	}

	var err error

	var orgUnitUserIDs []uint32
	var positionUserIDs []uint32
	var roleUserIDs []uint32
	if len(orgUnitIDs) > 0 {
		orgUnitUserIDs, err = r.userOrgUnitRepo.ListUserIDsByOrgUnitIDs(ctx, orgUnitIDs, false)
		if err != nil {
			return nil, err
		}
	}
	if len(positionIDs) > 0 {
		positionUserIDs, err = r.userPositionRepo.ListUserIDsByPositionIDs(ctx, positionIDs, false)
		if err != nil {
			return nil, err
		}
	}
	if len(roleIDs) > 0 {
		roleUserIDs, err = r.userRoleRepo.ListUserIDsByRoleIDs(ctx, roleIDs, false)
		if err != nil {
			return nil, err
		}
	}

	// 收集所有非空列表用于求交集
	lists := make([][]uint32, 0, 3)
	if orgUnitUserIDs != nil {
		lists = append(lists, orgUnitUserIDs)
	}
	if positionUserIDs != nil {
		lists = append(lists, positionUserIDs)
	}
	if roleUserIDs != nil {
		lists = append(lists, roleUserIDs)
	}

	// 如果没有任何实际列表（例如对应 ids 为空导致查询未执行），返回空
	if len(lists) == 0 {
		return []uint32{}, nil
	}

	// 逐步求交集
	result := lists[0]
	for i := 1; i < len(lists); i++ {
		result = sliceutil.Intersection(result, lists[i])
		if len(result) == 0 {
			break
		}
	}

	return result, nil
}

// List 列出用户
func (r *userRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*identityV1.ListUserResponse, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().User.Query()

	scopeSel, err := userScopeSelector(ctx)
	if err != nil {
		r.log.Errorf(ctx, "resolve data scope failed: %v", err)
		return nil, err
	}
	if scopeSel != nil {
		builder.Where(predicate.User(scopeSel))
	}

	filterExpr, err := r.repository.ConvertFilterByPagingRequest(req)
	if err != nil {
		r.log.Errorf(ctx, "convert filter by paging request failed: %s", err.Error())
		return nil, err
	}

	excludeConditions := pagination.FilterFields(filterExpr, []string{
		"org_unit_id", "org_unit_ids",
		"position_id", "position_ids",
		"role_id", "role_ids",
	})

	var orgUnitIDs []uint32
	var positionIDs []uint32
	var roleIDs []uint32
	for _, cond := range excludeConditions {
		//r.log.Debugf(ctx, "excluding filter condition: field=%s operator=%s value=%v", cond.GetField(), cond.GetOp(), cond.GetValue())

		var val uint64
		switch cond.GetField() {
		case "org_unit_id":
			if val, err = strconv.ParseUint(cond.GetValue(), 10, 64); err == nil {
				orgUnitIDs = append(orgUnitIDs, uint32(val))
			} else {
				r.log.Errorf(ctx, "parse org_unit_id value failed: %s", err.Error())
			}
		case "org_unit_ids":
			for _, v := range cond.GetValues() {
				if val, err = strconv.ParseUint(v, 10, 64); err == nil {
					orgUnitIDs = append(orgUnitIDs, uint32(val))
				} else {
					r.log.Errorf(ctx, "parse org_unit_ids value failed: %s", err.Error())
				}
			}

		case "position_id":
			if val, err = strconv.ParseUint(cond.GetValue(), 10, 64); err == nil {
				positionIDs = append(positionIDs, uint32(val))
			} else {
				r.log.Errorf(ctx, "parse position_id value failed: %s", err.Error())
			}
		case "position_ids":
			for _, v := range cond.GetValues() {
				if val, err = strconv.ParseUint(v, 10, 64); err == nil {
					positionIDs = append(positionIDs, uint32(val))
				} else {
					r.log.Errorf(ctx, "parse position_ids value failed: %s", err.Error())
				}
			}

		case "role_id":
			if val, err = strconv.ParseUint(cond.GetValue(), 10, 64); err == nil {
				roleIDs = append(roleIDs, uint32(val))
			} else {
				r.log.Errorf(ctx, "parse role_id value failed: %s", err.Error())
			}
		case "role_ids":
			for _, v := range cond.GetValues() {
				if val, err = strconv.ParseUint(v, 10, 64); err == nil {
					roleIDs = append(roleIDs, uint32(val))
				} else {
					r.log.Errorf(ctx, "parse role_ids value failed: %s", err.Error())
				}
			}
		}
	}

	var mergedUserIDs []uint32
	mergedUserIDs, err = r.queryUserIDsByRelationIDs(ctx, roleIDs, orgUnitIDs, positionIDs)
	if err != nil {
		r.log.Errorf(ctx, "query user ids by relation ids failed: %s", err.Error())
		return nil, err
	}

	//r.log.Debugf(ctx, "filtered user ids by relation ids: [%v] [%v] [%v] [%v]", roleIDs, orgUnitIDs, positionIDs, mergedUserIDs)

	hasRelationFilter := len(roleIDs) > 0 || len(orgUnitIDs) > 0 || len(positionIDs) > 0
	if hasRelationFilter && len(mergedUserIDs) == 0 {
		// 如果有关系过滤条件但没有匹配的用户ID，直接返回空结果
		return &identityV1.ListUserResponse{Total: 0, Items: nil}, nil
	}

	if len(mergedUserIDs) > 0 {
		filterExpr.Conditions = append(filterExpr.Conditions, &paginationV1.FilterCondition{
			Field: "id",
			Op:    paginationV1.Operator_IN,
			Values: func() []string {
				values := make([]string, 0, len(mergedUserIDs))
				for _, id := range mergedUserIDs {
					values = append(values, strconv.FormatUint(uint64(id), 10))
				}
				return values
			}(),
		})
	}

	req.FilteringType = &paginationV1.PagingRequest_FilterExpr{FilterExpr: filterExpr}

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &identityV1.ListUserResponse{Total: 0, Items: nil}, nil
	}

	resp := &identityV1.ListUserResponse{
		Total: ret.Total,
		Items: ret.Items,
	}

	if err = r.fillRelationIDs(ctx, resp.Items); err != nil {
		return nil, err
	}

	return resp, nil
}

// fillRelationIDs 给一页用户回填角色、岗位、组织单元 ID。
// 一对一模式下每种关联按 user_id IN (...) 各查一次；以前逐个用户查三次，一页 100 人是 300 次查询。
// 一对多（Membership）模式不是默认配置，仍逐个用户查。
func (r *userRepo) fillRelationIDs(ctx context.Context, users []*identityV1.User) error {
	if len(users) == 0 {
		return nil
	}

	if constants.DefaultUserTenantRelationType == constants.UserTenantRelationOneToMany {
		for _, item := range users {
			roleIDs, positionIDs, orgUnitIDs, err := r.ListUserRelationIDs(ctx, item.GetId())
			if err != nil {
				r.log.Errorf(ctx, "list user relation ids failed: %s", err.Error())
				return err
			}
			item.RoleIds, item.PositionIds, item.OrgUnitIds = roleIDs, positionIDs, orgUnitIDs
		}
		return nil
	}

	userIDs := make([]uint32, 0, len(users))
	for _, item := range users {
		userIDs = append(userIDs, item.GetId())
	}
	roleIDs, err := r.userRoleRepo.ListRoleIDsByUserIDs(ctx, userIDs)
	if err != nil {
		return err
	}
	positionIDs, err := r.userPositionRepo.ListPositionIDsByUserIDs(ctx, userIDs)
	if err != nil {
		return err
	}
	orgUnitIDs, err := r.userOrgUnitRepo.ListOrgUnitIDsByUserIDs(ctx, userIDs)
	if err != nil {
		return err
	}
	for _, item := range users {
		item.RoleIds = roleIDs[item.GetId()]
		item.PositionIds = positionIDs[item.GetId()]
		item.OrgUnitIds = orgUnitIDs[item.GetId()]
	}
	return nil
}

// Get 获取用户
func (r *userRepo) Get(ctx context.Context, req *identityV1.GetUserRequest) (*identityV1.User, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().User.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	case *identityV1.GetUserRequest_Id:
		whereCond = append(whereCond, user.IDEQ(req.GetId()))
	case *identityV1.GetUserRequest_Username:
		// username 只在 (tenant_id, username) 上唯一：固定按租户 0 查，旧库遗留的 tenant_id>0 行不参与匹配
		whereCond = append(whereCond, user.UsernameEQ(req.GetUsername()), user.TenantIDEQ(0))
	default:
		whereCond = append(whereCond, user.IDEQ(req.GetId()))
	}

	scopeSel, err := userScopeSelector(ctx)
	if err != nil {
		r.log.Errorf(ctx, "resolve data scope failed: %v", err)
		return nil, err
	}
	if scopeSel != nil {
		// 越界与不存在同样回 404，不泄露存在性
		whereCond = append(whereCond, scopeSel)
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		// 不存在（含别的租户的用户被租户过滤掉）回 404，不当成 500
		if ent.IsNotFound(err) {
			return nil, identityV1.ErrorNotFound("user not found")
		}
		return nil, err
	}

	roleIDs, positionIDs, orgUnitIDs, err := r.ListUserRelationIDs(ctx, dto.GetId())
	if err != nil {
		r.log.Errorf(ctx, "list user relation ids failed: %s", err.Error())
	}
	dto.RoleIds = roleIDs
	dto.PositionIds = positionIDs
	dto.OrgUnitIds = orgUnitIDs

	r.log.Debugf(ctx, "get user id=%d role_ids=%v position_ids=%v org_unit_ids=%v", dto.GetId(), roleIDs, positionIDs, orgUnitIDs)

	return dto, err
}

// BeginTx 开启 ent 事务。UserService.Create 用它把用户行、关联和凭据放进同一个事务。
func (r *userRepo) BeginTx(ctx context.Context) (*ent.Tx, error) {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return nil, identityV1.ErrorInternalServerError("start transaction failed")
	}
	return tx, nil
}

// Create 创建用户
func (r *userRepo) Create(ctx context.Context, req *identityV1.CreateUserRequest) (dto *identityV1.User, err error) {
	if req == nil || req.Data == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	var tx *ent.Tx
	tx, err = r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return nil, identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	dto, err = r.CreateWithTx(ctx, tx, req.GetData())
	if err != nil {
		return dto, err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return dto, identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return dto, nil
}

// CreateWithTx 在事务中创建用户
func (r *userRepo) CreateWithTx(ctx context.Context, tx *ent.Tx, data *identityV1.User) (dto *identityV1.User, err error) {
	if data == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}
	normalizeUserUniqueFields(data)

	builder := tx.User.Create().
		SetNillableTenantID(data.TenantId).
		SetNillableUsername(data.Username).
		SetNillableNickname(data.Nickname).
		SetNillableRealname(data.Realname).
		SetNillableAvatar(data.Avatar).
		SetNillableEmail(data.Email).
		SetNillableMobile(data.Mobile).
		SetNillableTelephone(data.Telephone).
		SetNillableRegion(data.Region).
		SetNillableLocale(data.Locale).
		SetNillableAddress(data.Address).
		SetNillableDescription(data.Description).
		SetNillableRemark(data.Remark).
		SetNillableLastLoginAt(timeutil.TimestamppbToTime(data.LastLoginAt)).
		SetNillableLockedUntil(timeutil.TimestamppbToTime(data.LockedUntil)).
		SetNillableLastLoginIP(data.LastLoginIp).
		SetNillableGender(r.genderConverter.ToEntity(data.Gender)).
		SetNillableStatus(r.statusConverter.ToEntity(data.Status)).
		SetNillableCreatedBy(data.CreatedBy).
		SetCreatedAt(time.Now())

	if data.Id != nil {
		builder.SetID(data.GetId())
	}

	var entity *ent.User
	if entity, err = builder.Save(ctx); err != nil {
		return nil, mapUserWriteError(r.log, ctx, "insert user", err)
	}

	if data.GetRoleId() > 0 {
		data.RoleIds = append(data.RoleIds, data.GetRoleId())
	}
	if data.GetOrgUnitId() > 0 {
		data.OrgUnitIds = append(data.OrgUnitIds, data.GetOrgUnitId())
	}
	if data.GetPositionId() > 0 {
		data.PositionIds = append(data.PositionIds, data.GetPositionId())
	}

	switch constants.DefaultUserTenantRelationType {
	case constants.UserTenantRelationNone, constants.UserTenantRelationOneToOne:
		if err = r.assignUserRelations(ctx, tx,
			data.GetTenantId(), entity.ID,
			data.GetRoleIds(),
			data.GetOrgUnitIds(),
			data.GetPositionIds()); err != nil {
			return nil, err
		}
	case constants.UserTenantRelationOneToMany:
		if err = r.assignMembershipRelations(ctx, tx,
			data.GetTenantId(), entity.ID,
			data.GetRoleIds(),
			data.GetOrgUnitIds(),
			data.GetPositionIds()); err != nil {
			return nil, err
		}
	}

	return r.mapper.ToDTO(entity), nil
}

// Update 更新用户
func (r *userRepo) Update(ctx context.Context, req *identityV1.UpdateUserRequest) (err error) {
	if req == nil || req.Data == nil {
		return identityV1.ErrorBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return identityV1.ErrorBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		var existResp *identityV1.UserExistsResponse
		existResp, err = r.UserExists(ctx, &identityV1.UserExistsRequest{
			QueryBy: &identityV1.UserExistsRequest_Id{Id: req.GetId()},
		})
		if err != nil {
			return err
		}
		if !existResp.Exist {
			createReq := &identityV1.CreateUserRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			_, err = r.Create(ctx, createReq)
			return err
		}
	}

	var tx *ent.Tx
	tx, err = r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	// 关联（角色/组织/岗位）的重建采用 delete-then-insert，故只有当对应字段确实在本次
	// 更新范围（field-mask）内时才收集其 ID，否则只改昵称等普通字段的"部分更新"若 Data 里
	// 带了（哪怕为空/旧值）关联字段，会误把用户已有角色/组织/岗位全部清空。
	// maskPathInUpdate：无 mask 视为全量更新（true）；有 mask 则按路径精确匹配。
	// 注意 User 同时有单数（role_id）和复数（role_ids）字段，两者任一在 mask 内即视为
	// 本类关联参与本次更新（与下方收集逻辑一致：RoleIds + RoleId 都会被收集）。
	// nil mask 守卫：无 mask = 全量更新语义，但后续 FilterBlacklist 需要
	// 非 nil mask 做赋值/剔除，故此处初始化空 mask（panic 修复，e2e 揪出）。
	normalizeUserUniqueFields(req.Data)

	if req.GetUpdateMask() == nil {
		req.UpdateMask = &fieldmaskpb.FieldMask{}
	}

	maskPathInUpdate := func(paths ...string) bool {
		fm := req.GetUpdateMask()
		if fm == nil || len(fm.Paths) == 0 {
			return true
		}
		for _, p := range paths {
			if slices.Contains(fm.Paths, p) {
				return true
			}
		}
		return false
	}

	var roleIds []uint32
	if maskPathInUpdate("role_ids", "role_id") {
		if len(req.Data.GetRoleIds()) > 0 {
			roleIds = req.Data.GetRoleIds()
		}
		if req.Data.RoleId != nil && *req.Data.RoleId > 0 {
			roleIds = append(roleIds, *req.Data.RoleId)
		}
		roleIds = sliceutil.Unique(roleIds)
	}

	var orgUnitIds []uint32
	if maskPathInUpdate("org_unit_ids", "org_unit_id") {
		if len(req.Data.GetOrgUnitIds()) > 0 {
			orgUnitIds = req.Data.GetOrgUnitIds()
		}
		if req.Data.OrgUnitId != nil && *req.Data.OrgUnitId > 0 {
			orgUnitIds = append(orgUnitIds, *req.Data.OrgUnitId)
		}
		orgUnitIds = sliceutil.Unique(orgUnitIds)
	}

	var positionIds []uint32
	if maskPathInUpdate("position_ids", "position_id") {
		if len(req.Data.GetPositionIds()) > 0 {
			positionIds = req.Data.GetPositionIds()
		}
		if req.Data.PositionId != nil && *req.Data.PositionId > 0 {
			positionIds = append(positionIds, *req.Data.PositionId)
		}
		positionIds = sliceutil.Unique(positionIds)
	}

	req.GetUpdateMask().Paths = sliceutil.FilterBlacklist(req.GetUpdateMask().Paths, []string{
		"role_ids",
		"role_id",
		"position_ids",
		"position_id",
		"org_unit_ids",
		"org_unit_id",
		// password 不是 User proto 的字段（密码走 UpdateUserRequest 顶层独立字段），
		// 残留在 mask 里会让 FilterByFieldMask 静默清空整个 DTO，导致更新 200 但零写入
		"password",
	})

	// 列表接口返回的 email/mobile 是脱敏掩码（如 ab***@x.com）；编辑抽屉把它原样回传时，
	// 若不加拦截会把掩码串当真实值入库造成数据损坏。含 '*' 即视为掩码，跳过写入。
	if req.Data.Email != nil && strings.Contains(*req.Data.Email, "*") {
		req.Data.Email = nil
		req.GetUpdateMask().Paths = sliceutil.FilterBlacklist(req.GetUpdateMask().Paths, []string{"email"})
	}
	if req.Data.Mobile != nil && strings.Contains(*req.Data.Mobile, "*") {
		req.Data.Mobile = nil
		req.GetUpdateMask().Paths = sliceutil.FilterBlacklist(req.GetUpdateMask().Paths, []string{"mobile"})
	}

	var entity *identityV1.User
	builder := tx.User.UpdateOneID(req.GetId())
	entity, err = r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *identityV1.User) {
			builder.
				SetNillableNickname(req.Data.Nickname).
				SetNillableRealname(req.Data.Realname).
				SetNillableAvatar(req.Data.Avatar).
				SetNillableEmail(req.Data.Email).
				SetNillableMobile(req.Data.Mobile).
				SetNillableTelephone(req.Data.Telephone).
				SetNillableRegion(req.Data.Region).
				SetNillableLocale(req.Data.Locale).
				SetNillableAddress(req.Data.Address).
				SetNillableDescription(req.Data.Description).
				SetNillableRemark(req.Data.Remark).
				SetNillableLastLoginAt(timeutil.TimestamppbToTime(req.Data.LastLoginAt)).
				SetNillableLockedUntil(timeutil.TimestamppbToTime(req.Data.LockedUntil)).
				SetNillableLastLoginIP(req.Data.LastLoginIp).
				SetNillableGender(r.genderConverter.ToEntity(req.Data.Gender)).
				SetNillableStatus(r.statusConverter.ToEntity(req.Data.Status)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(user.FieldID, req.GetId()))
		},
	)
	// 用户行没更新成功就不能再动关联：以前这里不查 err，紧接着被 assignUserRelations 的结果覆盖，
	// 用户不存在 / 唯一冲突时照样按 tenant_id=0 重写关联并提交，接口回 200。
	if err != nil {
		return mapUserWriteError(r.log, ctx, "update user", err)
	}

	switch constants.DefaultUserTenantRelationType {
	case constants.UserTenantRelationNone, constants.UserTenantRelationOneToOne:
		if err = r.assignUserRelations(ctx, tx,
			entity.GetTenantId(),
			req.GetId(),
			roleIds,
			orgUnitIds,
			positionIds); err != nil {
			return err
		}
	case constants.UserTenantRelationOneToMany:
		if err = r.assignMembershipRelations(ctx, tx,
			entity.GetTenantId(),
			req.GetId(),
			roleIds,
			orgUnitIds,
			positionIds); err != nil {
			return err
		}
	}

	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// normalizeUserUniqueFields 把参与唯一索引的可选字段里的空串归一成 NULL。
//
// email 在 (tenant_id, email) 上有唯一索引，NULL 不参与唯一判定，空串参与：
// 两个都没填邮箱的用户一旦都存成 ""，第二个就写不进去。编辑表单清空邮箱、
// 或者 updateMask 里带 email 而值为空时，email 置 nil，mask 里的 email 会让仓储把列写成 NULL。
// username 有 NotEmpty 校验；mobile 只有普通索引，不受影响。
func normalizeUserUniqueFields(data *identityV1.User) {
	if data == nil {
		return
	}
	if data.Email != nil && strings.TrimSpace(*data.Email) == "" {
		data.Email = nil
	}
}

// mapUserWriteError 把用户行写入失败映射成调用方能处理的错误：
// 行不存在 → 404，唯一索引冲突（tenant_id+username / tenant_id+email）→ 409，其余 → 500。原始错误写日志。
func mapUserWriteError(log *bLogger.Helper, ctx context.Context, op string, err error) error {
	switch {
	case ent.IsNotFound(err):
		log.Warnf(ctx, "%s: user not found: %s", op, err.Error())
		return identityV1.ErrorNotFound("user not found")
	case ent.IsConstraintError(err):
		log.Warnf(ctx, "%s: unique constraint violated: %s", op, err.Error())
		return identityV1.ErrorConflict("username or email already in use")
	}
	log.Errorf(ctx, "%s failed: %s", op, err.Error())
	return identityV1.ErrorInternalServerError("%s failed", op)
}

// Delete 删除用户
func (r *userRepo) Delete(ctx context.Context, req *identityV1.DeleteUserRequest) (err error) {
	var existResp *identityV1.UserExistsResponse
	existReq := &identityV1.UserExistsRequest{}
	switch req.QueryBy.(type) {
	default:
	case *identityV1.DeleteUserRequest_Id:
		existReq.QueryBy = &identityV1.UserExistsRequest_Id{Id: req.GetId()}
	case *identityV1.DeleteUserRequest_Username:
		existReq.QueryBy = &identityV1.UserExistsRequest_Username{Username: req.GetUsername()}
	}
	existResp, err = r.UserExists(ctx, existReq)
	if err != nil {
		return err
	}
	if !existResp.Exist {
		return identityV1.ErrorNotFound("user not found")
	}

	var tx *ent.Tx
	tx, err = r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	builder := tx.User.Delete()

	switch req.QueryBy.(type) {
	case *identityV1.DeleteUserRequest_Id:
		builder.Where(user.IDEQ(req.GetId()))
	case *identityV1.DeleteUserRequest_Username:
		// username 只在 (tenant_id, username) 上唯一：固定按租户 0 删，不波及旧库遗留的同名 tenant_id>0 用户
		builder.Where(user.UsernameEQ(req.GetUsername()), user.TenantIDEQ(0))
	default:
		builder.Where(user.IDEQ(req.GetId()))
	}

	// 按用户名删除时 req.GetId() 为 0，先解析出真实 ID，后面的关联与凭证清理按它来。
	userID := req.GetId()
	if _, byName := req.QueryBy.(*identityV1.DeleteUserRequest_Username); byName {
		if userID, err = tx.User.Query().
			Where(user.UsernameEQ(req.GetUsername()), user.TenantIDEQ(0)).
			OnlyID(ctx); err != nil {
			if ent.IsNotFound(err) {
				return identityV1.ErrorNotFound("user not found")
			}
			r.log.Errorf(ctx, "resolve user id by username failed: %s", err.Error())
			return identityV1.ErrorInternalServerError("delete failed")
		}
	}

	if _, err = builder.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return identityV1.ErrorNotFound("user not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return identityV1.ErrorInternalServerError("delete failed")
	}

	switch constants.DefaultUserTenantRelationType {
	case constants.UserTenantRelationNone, constants.UserTenantRelationOneToOne:
		if err = r.removeUserRelations(ctx, tx, userID); err != nil {
			return err
		}
	case constants.UserTenantRelationOneToMany:
		if err = r.removeMembershipRelations(ctx, tx, userID); err != nil {
			return err
		}
	}

	// 凭证随用户一起删。留下的孤儿凭证会让同名重建的用户登录时 NotSingular → 503。
	if _, err = tx.UserCredential.Delete().Where(usercredential.UserIDEQ(userID)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete user credentials failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("delete user credentials failed")
	}

	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// removeUserRelations 移除用户关联关系
// 任何一步失败立即返回，由调用方回滚整个删除事务。
// 以前三步的 err 互相覆盖，只回最后一步的结果：前面失败、最后成功时照样提交，留下孤儿关联。
func (r *userRepo) removeUserRelations(ctx context.Context, tx *ent.Tx, userID uint32) error {
	if err := r.userRoleRepo.CleanRelationsByUserID(ctx, tx, userID); err != nil {
		r.log.Errorf(ctx, "clean user role relations failed: %s", err.Error())
		return err
	}
	if err := r.userOrgUnitRepo.CleanRelationsByUserID(ctx, tx, userID); err != nil {
		r.log.Errorf(ctx, "clean user org unit relations failed: %s", err.Error())
		return err
	}
	if err := r.userPositionRepo.CleanRelationsByUserID(ctx, tx, userID); err != nil {
		r.log.Errorf(ctx, "clean user position relations failed: %s", err.Error())
		return err
	}
	return nil
}

// removeMembershipRelations 移除用户关联关系（通过 Membership 关联）
func (r *userRepo) removeMembershipRelations(ctx context.Context, tx *ent.Tx, userID uint32) error {
	if err := r.membershipRepo.CleanRelationsByUserID(ctx, tx, userID); err != nil {
		r.log.Errorf(ctx, "clean user membership relations failed: %s", err.Error())
		return err
	}
	return nil
}

// UserExists 检查用户是否存在
func (r *userRepo) UserExists(ctx context.Context, req *identityV1.UserExistsRequest) (*identityV1.UserExistsResponse, error) {
	builder := r.entClient.Client().User.Query()

	switch req.QueryBy.(type) {
	case *identityV1.UserExistsRequest_Id:
		builder.Where(user.IDEQ(req.GetId()))
	case *identityV1.UserExistsRequest_Username:
		// username 只在 (tenant_id, username) 上唯一：固定按租户 0 查
		builder.Where(user.UsernameEQ(req.GetUsername()), user.TenantIDEQ(0))
	default:
		return &identityV1.UserExistsResponse{
			Exist: false,
		}, identityV1.ErrorBadRequest("invalid query by type")
	}

	exist, err := builder.Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return &identityV1.UserExistsResponse{
			Exist: false,
		}, identityV1.ErrorInternalServerError("query exist failed")
	}

	return &identityV1.UserExistsResponse{
		Exist: exist,
	}, nil
}

// assignMembershipRelations 分配用户关联关系（通过 Membership 关联）
func (r *userRepo) assignMembershipRelations(ctx context.Context, tx *ent.Tx,
	tenantID, userID uint32,
	roleIDs, orgUnitIDs, positionIDs []uint32,
) error {
	now := time.Now()
	err := r.membershipRepo.AssignTenantMembershipWithTx(ctx, tx, &identityV1.Membership{
		TenantId:   trans.Ptr(tenantID),
		UserId:     trans.Ptr(userID),
		Status:     identityV1.Membership_ACTIVE.Enum(),
		IsPrimary:  trans.Ptr(true),
		AssignedAt: timeutil.TimeToTimestamppb(&now),

		RoleIds:     roleIDs,
		OrgUnitIds:  orgUnitIDs,
		PositionIds: positionIDs,
	})
	if err != nil {
		return err
	}

	return nil
}

// assignUserRelations 分配用户关联关系
func (r *userRepo) assignUserRelations(ctx context.Context, tx *ent.Tx,
	tenantID, userID uint32,
	roleIDs, orgUnitIDs, positionIDs []uint32,
) (err error) {
	if len(roleIDs) == 0 && len(orgUnitIDs) == 0 && len(positionIDs) == 0 {
		return nil
	}

	now := time.Now()

	if len(roleIDs) > 0 {
		roleIDs = sliceutil.Unique(roleIDs)
		var userRoles []*permissionV1.UserRole
		for _, roleID := range roleIDs {
			userRoles = append(userRoles, &permissionV1.UserRole{
				TenantId:   trans.Ptr(tenantID),
				UserId:     trans.Ptr(userID),
				RoleId:     trans.Ptr(roleID),
				Status:     permissionV1.UserRole_ACTIVE.Enum(),
				IsPrimary:  trans.Ptr(true),
				AssignedAt: timeutil.TimeToTimestamppb(&now),
			})
		}
		if err = r.userRoleRepo.AssignUserRoles(ctx, tx, userID, userRoles); err != nil {
			return err
		}
	}
	if len(orgUnitIDs) > 0 {
		orgUnitIDs = sliceutil.Unique(orgUnitIDs)
		//r.log.Debugf(ctx, "assigning org unit ids: %v", orgUnitIDs)
		var userOrgUnits []*identityV1.UserOrgUnit
		for _, orgUnitID := range orgUnitIDs {
			userOrgUnits = append(userOrgUnits, &identityV1.UserOrgUnit{
				TenantId:   trans.Ptr(tenantID),
				UserId:     trans.Ptr(userID),
				OrgUnitId:  trans.Ptr(orgUnitID),
				Status:     identityV1.UserOrgUnit_ACTIVE.Enum(),
				IsPrimary:  trans.Ptr(true),
				AssignedAt: timeutil.TimeToTimestamppb(&now),
			})
		}
		if err = r.userOrgUnitRepo.AssignUserOrgUnits(ctx, tx, userID, userOrgUnits); err != nil {
			return err
		}
	}
	if len(positionIDs) > 0 {
		positionIDs = sliceutil.Unique(positionIDs)
		//r.log.Debugf(ctx, "assigning position ids: %v", positionIDs)
		var userPositions []*identityV1.UserPosition
		for _, positionID := range positionIDs {
			userPositions = append(userPositions, &identityV1.UserPosition{
				TenantId:   trans.Ptr(tenantID),
				UserId:     trans.Ptr(userID),
				PositionId: trans.Ptr(positionID),
				Status:     identityV1.UserPosition_ACTIVE.Enum(),
				IsPrimary:  trans.Ptr(true),
				AssignedAt: timeutil.TimeToTimestamppb(&now),
			})
		}
		if err = r.userPositionRepo.AssignUserPositions(ctx, tx, userID, userPositions); err != nil {
			return err
		}
	}

	return nil
}

// AssignUserRole 分配角色
func (r *userRepo) AssignUserRole(ctx context.Context, data *permissionV1.UserRole) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userRoleRepo.AssignUserRole(ctx, tx, data); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// AssignUserRoles 分配角色
func (r *userRepo) AssignUserRoles(ctx context.Context, userID uint32, datas []*permissionV1.UserRole) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userRoleRepo.AssignUserRoles(ctx, tx, userID, datas); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// AssignUserOrgUnit 分配组织单元给用户
func (r *userRepo) AssignUserOrgUnit(ctx context.Context, data *identityV1.UserOrgUnit) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userOrgUnitRepo.AssignUserOrgUnit(ctx, tx, data); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// AssignUserOrgUnits 分配组织单元给用户
func (r *userRepo) AssignUserOrgUnits(ctx context.Context, userID uint32, datas []*identityV1.UserOrgUnit) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userOrgUnitRepo.AssignUserOrgUnits(ctx, tx, userID, datas); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// AssignUserPosition 分配岗位给用户
func (r *userRepo) AssignUserPosition(ctx context.Context, data *identityV1.UserPosition) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userPositionRepo.AssignUserPosition(ctx, tx, data); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// AssignUserPositions 分配岗位给用户
func (r *userRepo) AssignUserPositions(ctx context.Context, userID uint32, datas []*identityV1.UserPosition) error {
	var tx *ent.Tx
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
	})

	if err = r.userPositionRepo.AssignUserPositions(ctx, tx, userID, datas); err != nil {
		return err
	}
	if commitErr := commitEntTx(tx, &txCommitted); commitErr != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
		return identityV1.ErrorInternalServerError("transaction commit failed")
	}
	return nil
}

// ListUsersByIds 根据ID列表获取用户列表
func (r *userRepo) ListUsersByIds(ctx context.Context, ids []uint32) ([]*identityV1.User, error) {
	if len(ids) == 0 {
		return []*identityV1.User{}, nil
	}

	entities, err := r.entClient.Client().User.Query().
		Where(user.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query user by ids failed: %s", err.Error())
		return nil, identityV1.ErrorInternalServerError("query user by ids failed")
	}

	dtos := make([]*identityV1.User, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

// CountByTenantIDs 批量统计各租户下的用户数。
// 仅返回存在的 tenant_id 计数，未出现的 tenant 隐含 0。
func (r *userRepo) CountByTenantIDs(ctx context.Context, tenantIDs []uint32) (map[uint32]int, error) {
	result := make(map[uint32]int, len(tenantIDs))
	if len(tenantIDs) == 0 {
		return result, nil
	}
	for _, tid := range tenantIDs {
		cnt, err := r.entClient.Client().User.Query().
			Where(user.TenantIDEQ(tid)).
			Count(ctx)
		if err != nil {
			r.log.Errorf(ctx, "count users by tenant %d failed: %s", tid, err.Error())
			return nil, identityV1.ErrorInternalServerError("count users by tenant failed")
		}
		result[tid] = cnt
	}
	return result, nil
}

// FindUsernameByIdentifier 登录 identifier 智能解析。
// 凭证表只存 USERNAME 维度的密码行，email/mobile 登录通过 user 表反查得到 username
// 后仍走现有凭证校验，不改凭证数据模型。
// mobile 在租户内非唯一索引：命中多行属于数据歧义，返回错误拒绝登录（而非随机取一行）。
func (r *userRepo) FindUsernameByIdentifier(ctx context.Context, tenantID uint32, identifier string) (string, uint32, error) {
	switch {
	case strings.Contains(identifier, "@"):
		entity, err := r.entClient.Client().User.Query().
			Select(user.FieldID, user.FieldUsername, user.FieldStatus).
			Where(
				user.TenantIDEQ(tenantID),
				user.EmailEQ(identifier),
			).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				r.log.Warnf(ctx, "login email [%s] not matched in tenant [%d], fallthrough to credential check", identifier, tenantID)
				return identifier, 0, nil
			}
			r.log.Errorf(ctx, "find user by email failed: %s", err.Error())
			return "", 0, identityV1.ErrorInternalServerError("find user by email failed")
		}
		return *entity.Username, entity.ID, nil

	case isDigits(identifier):
		entities, err := r.entClient.Client().User.Query().
			Select(user.FieldID, user.FieldUsername, user.FieldStatus).
			Where(
				user.TenantIDEQ(tenantID),
				user.MobileEQ(identifier),
			).
			All(ctx)
		if err != nil {
			r.log.Errorf(ctx, "find user by mobile failed: %s", err.Error())
			return "", 0, identityV1.ErrorInternalServerError("find user by mobile failed")
		}
		switch len(entities) {
		case 0:
			// 未命中：原样返回，交给后续凭证校验走统一失败路径（防枚举）
			return identifier, 0, nil
		case 1:
			return *entities[0].Username, entities[0].ID, nil
		default:
			r.log.Errorf(ctx, "ambiguous mobile login: tenant=%d mobile=%s rows=%d", tenantID, identifier, len(entities))
			return "", 0, identityV1.ErrorInternalServerError("ambiguous account identifier")
		}

	default:
		return identifier, 0, nil
	}
}

// isDigits 判断字符串是否全为数字（手机号判定）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (r *userRepo) ListRoleIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error) {
	return r.userRoleRepo.ListRoleIDs(ctx, userID, false)
}

func (r *userRepo) ListPositionIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error) {
	return r.userPositionRepo.ListPositionIDs(ctx, userID, false)
}

func (r *userRepo) ListOrgUnitIDsByUserID(ctx context.Context, userID uint32) ([]uint32, error) {
	return r.userOrgUnitRepo.ListOrgUnitIDs(ctx, userID, false)
}

// ListUserRelationIDs 列出用户关联的角色、岗位、组织单元ID列表
func (r *userRepo) ListUserRelationIDs(ctx context.Context, userID uint32) (roleIDs []uint32, positionIDs []uint32, orgUnitIDs []uint32, err error) {
	if userID == 0 {
		return
	}

	switch constants.DefaultUserTenantRelationType {
	default:
		fallthrough
	case constants.UserTenantRelationOneToOne:
		return r.listUserRelationIDs(ctx, userID)
	case constants.UserTenantRelationOneToMany:
		return r.membershipRepo.ListMembershipRelationIDs(ctx, userID)
	}
}

// listUserRelationIDsOneToOne 列出用户关联的角色、岗位、组织单元ID列表（一对一关系）
func (r *userRepo) listUserRelationIDs(ctx context.Context, userID uint32) (roleIDs []uint32, positionIDs []uint32, orgUnitIDs []uint32, err error) {
	if userID == 0 {
		r.log.Errorf(ctx, "invalid user id: %d", userID)
		return
	}

	if roleIDs, err = r.userRoleRepo.ListRoleIDs(ctx, userID, false); err != nil {
		r.log.Errorf(ctx, "list user role ids failed: %s", err.Error())
		return
	}

	if positionIDs, err = r.userPositionRepo.ListPositionIDs(ctx, userID, false); err != nil {
		r.log.Errorf(ctx, "list user position ids failed: %s", err.Error())
		return
	}

	if orgUnitIDs, err = r.userOrgUnitRepo.ListOrgUnitIDs(ctx, userID, false); err != nil {
		r.log.Errorf(ctx, "list user org unit ids failed: %s", err.Error())
		return
	}

	r.log.Debugf(ctx, "list user relation ids: user_id=%d role_ids=%v position_ids=%v org_unit_ids=%v", userID, roleIDs, positionIDs, orgUnitIDs)

	return
}
