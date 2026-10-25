package data

import (
	"context"

	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/api"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/ent/predicate"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
)

type ApiRepo struct {
	entClient  *entCrud.EntClient[*ent.Client]
	driverName string
	log        *bLogger.Helper

	mapper                  *mapper.CopierMapper[permissionV1.Api, ent.Api]
	statusConverter         *mapper.EnumTypeConverter[permissionV1.Api_Status, api.Status]
	scopeConverter          *mapper.EnumTypeConverter[permissionV1.Api_Scope, api.Scope]
	businessModuleConverter *mapper.EnumTypeConverter[identityV1.Module, api.BusinessModule]

	repository *entCrud.Repository[
		ent.APIQuery, ent.APISelect,
		ent.APICreate, ent.APICreateBulk,
		ent.APIUpdate, ent.APIUpdateOne,
		ent.APIDelete,
		predicate.Api,
		permissionV1.Api, ent.Api,
	]
}

func NewApiRepo(ctx *bootstrap.Context, entClient *entCrud.EntClient[*ent.Client]) *ApiRepo {
	repo := &ApiRepo{
		log:        ctx.NewLoggerHelper("api/repo/admin-service"),
		entClient:  entClient,
		driverName: driverNameOf(ctx),
		mapper:     mapper.NewCopierMapper[permissionV1.Api, ent.Api](),
		statusConverter: mapper.NewEnumTypeConverter[permissionV1.Api_Status, api.Status](
			permissionV1.Api_Status_name, permissionV1.Api_Status_value,
		),
		scopeConverter: mapper.NewEnumTypeConverter[permissionV1.Api_Scope, api.Scope](
			permissionV1.Api_Scope_name, permissionV1.Api_Scope_value,
		),
		businessModuleConverter: mapper.NewEnumTypeConverter[identityV1.Module, api.BusinessModule](
			identityV1.Module_name, identityV1.Module_value,
		),
	}

	repo.init()

	return repo
}

func (r *ApiRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.APIQuery, ent.APISelect,
		ent.APICreate, ent.APICreateBulk,
		ent.APIUpdate, ent.APIUpdateOne,
		ent.APIDelete,
		predicate.Api,
		permissionV1.Api, ent.Api,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	// status 转换器补注册：本仓 status 实体列为可空指针枚举（SwitchStatus
	// mixin、列默认 ON），但本仓此前从未注册 statusConverter——scope/
	// business_module 均有注册而 status 独缺。mapper 经转换对（指针↔指针）
	// 查表转换，缺注册即无转换对、copier 对该字段直接跳过，读视图恒零值
	//（与 scope 域同表同列形态却长期不可见）。补注册后读路径经 mapper 如实
	// 流通；写路径维持"未指定即走列默认"现状不变。
	r.mapper.AppendConverters(r.statusConverter.NewConverterPair())
	r.mapper.AppendConverters(r.scopeConverter.NewConverterPair())
	r.mapper.AppendConverters(r.businessModuleConverter.NewConverterPair())
}

func (r *ApiRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.CountApiResponse, error) {
	builder := r.entClient.Client().Api.Query()

	whereSelectors, _, _ := r.repository.BuildListSelectorWithPaging(builder, req)
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query api count failed: %s", err.Error())
		return nil, permissionV1.ErrorInternalServerError("query api count failed")
	}

	return &permissionV1.CountApiResponse{
		Count: uint64(count),
	}, nil
}

func (r *ApiRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListApiResponse, error) {
	if req == nil {
		return nil, permissionV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().Api.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &permissionV1.ListApiResponse{Total: 0, Items: nil}, nil
	}

	return &permissionV1.ListApiResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *ApiRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().Api.Query().
		Where(api.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, permissionV1.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *ApiRepo) Get(ctx context.Context, req *permissionV1.GetApiRequest) (*permissionV1.Api, error) {
	if req == nil {
		return nil, permissionV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().Api.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *permissionV1.GetApiRequest_Id:
		whereCond = append(whereCond, api.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// GetApiByEndpoint 根据路径和方法获取API资源
func (r *ApiRepo) GetApiByEndpoint(ctx context.Context, path, method string) (*permissionV1.Api, error) {
	if path == "" || method == "" {
		return nil, permissionV1.ErrorBadRequest("invalid parameter")
	}

	entity, err := r.entClient.Client().Api.Query().
		Where(
			api.PathEQ(path),
			api.MethodEQ(method),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, permissionV1.ErrorNotFound("api not found")
		}

		r.log.Errorf(ctx, "query one data failed: %s", err.Error())

		return nil, permissionV1.ErrorInternalServerError("query data failed")
	}

	return r.mapper.ToDTO(entity), nil
}

// GetApiByIDs 根据ID列表获取API资源
func (r *ApiRepo) GetApiByIDs(ctx context.Context, ids []uint32) ([]*permissionV1.Api, error) {
	if len(ids) == 0 {
		return nil, permissionV1.ErrorBadRequest("invalid parameter")
	}

	entities, err := r.entClient.Client().Api.Query().
		Where(
			api.IDIn(ids...),
		).
		All(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, permissionV1.ErrorNotFound("api not found")
		}

		r.log.Errorf(ctx, "query one data failed: %s", err.Error())

		return nil, permissionV1.ErrorInternalServerError("query data failed")
	}

	dtos := make([]*permissionV1.Api, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *ApiRepo) Create(ctx context.Context, req *permissionV1.CreateApiRequest) error {
	if req == nil || req.Data == nil {
		return permissionV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.newApiCreate(req.Data)

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert api failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("insert api failed")
	}

	return nil
}

func (r *ApiRepo) newApiCreate(api *permissionV1.Api) *ent.APICreate {
	builder := r.entClient.Client().Api.Create().
		SetNillableDescription(api.Description).
		SetNillableModule(api.Module).
		SetNillableModuleDescription(api.ModuleDescription).
		SetNillableOperation(api.Operation).
		SetNillablePath(api.Path).
		SetNillableMethod(api.Method).
		SetNillableScope(r.scopeConverter.ToEntity(api.Scope)).
		SetNillableCreatedBy(api.CreatedBy).
		SetCreatedAt(time.Now())

	// business_module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：ent schema 未声明该值，
	// SetNillableBusinessModule 会触发 BusinessModuleValidator 失败。未指定即留空。
	if api.BusinessModule != nil && *api.BusinessModule != identityV1.Module_MODULE_UNSPECIFIED {
		builder.SetNillableBusinessModule(r.businessModuleConverter.ToEntity(api.BusinessModule))
	}

	if api.Id != nil {
		builder.SetID(api.GetId())
	}

	return builder
}

func (r *ApiRepo) BatchCreate(ctx context.Context, apis []*permissionV1.Api) error {
	if len(apis) == 0 {
		return nil
	}

	bulk := make([]*ent.APICreate, 0, len(apis))
	for _, dto := range apis {
		builder := r.newApiCreate(dto)
		bulk = append(bulk, builder)
	}

	bulkBuilder := r.entClient.Client().Api.CreateBulk(bulk...)

	if err := bulkBuilder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "batch insert apis failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("batch insert apis failed")
	}

	return nil
}

func (r *ApiRepo) Update(ctx context.Context, req *permissionV1.UpdateApiRequest) error {
	if req == nil || req.Data == nil {
		return permissionV1.ErrorBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return permissionV1.ErrorBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &permissionV1.CreateApiRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.entClient.Client().Api.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *permissionV1.Api) {
			builder.
				SetNillableDescription(req.Data.Description).
				SetNillableModule(req.Data.Module).
				SetNillableModuleDescription(req.Data.ModuleDescription).
				SetNillableOperation(req.Data.Operation).
				SetNillablePath(req.Data.Path).
				SetNillableMethod(req.Data.Method).
				SetNillableScope(r.scopeConverter.ToEntity(req.Data.Scope)).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())

			// business_module 为 proto 零值（MODULE_UNSPECIFIED）时跳过：ent schema 未声明该值，
			// SetNillableBusinessModule 会触发 BusinessModuleValidator 失败。未指定即不更新该字段。
			if req.Data.BusinessModule != nil && *req.Data.BusinessModule != identityV1.Module_MODULE_UNSPECIFIED {
				builder.SetNillableBusinessModule(r.businessModuleConverter.ToEntity(req.Data.BusinessModule))
			}
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(api.FieldID, req.GetId()))
		},
	)

	return err
}

func (r *ApiRepo) Delete(ctx context.Context, req *permissionV1.DeleteApiRequest) error {
	if req == nil {
		return permissionV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().Api.Delete()

	_, err := r.repository.Delete(ctx, builder, func(s *sql.Selector) {
		s.Where(sql.EQ(api.FieldID, req.GetId()))
	})
	if err != nil {
		r.log.Errorf(ctx, "delete api failed: %s", err.Error())
		return permissionV1.ErrorInternalServerError("delete api failed")
	}

	return nil
}

// DeleteExcept 删除 ID 不在 keepIDs 中的接口，连同权限上对它们的绑定。
// sys_permission_apis.api_id 没有外键，接口行删掉后绑定不会自动级联，
// 留着的话这些 ID 将来被新接口复用时会把权限错绑到无关接口上。
func (r *ApiRepo) DeleteExcept(ctx context.Context, keepIDs []uint32) (int, error) {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return 0, permissionV1.ErrorInternalServerError("start transaction failed")
	}

	if _, err = tx.PermissionApi.Delete().Where(permissionapi.APIIDNotIn(keepIDs...)).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "delete stale permission apis failed: %s", err.Error())
		if rbErr := tx.Rollback(); rbErr != nil {
			r.log.Errorf(ctx, "rollback failed: %s", rbErr.Error())
		}
		return 0, permissionV1.ErrorInternalServerError("delete stale permission apis failed")
	}

	n, err := tx.Api.Delete().Where(api.IDNotIn(keepIDs...)).Exec(ctx)
	if err != nil {
		r.log.Errorf(ctx, "delete stale apis failed: %s", err.Error())
		if rbErr := tx.Rollback(); rbErr != nil {
			r.log.Errorf(ctx, "rollback failed: %s", rbErr.Error())
		}
		return 0, permissionV1.ErrorInternalServerError("delete stale apis failed")
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf(ctx, "commit failed: %s", err.Error())
		return 0, permissionV1.ErrorInternalServerError("commit failed")
	}

	return n, nil
}

// Truncate 清空表数据
func (r *ApiRepo) Truncate(ctx context.Context) error {
	if _, err := r.entClient.Client().Api.Delete().Exec(ctx); err != nil {
		r.log.Errorf(ctx, "failed to truncate apis table: %s", err.Error())
		return permissionV1.ErrorInternalServerError("truncate failed")
	}
	return nil
}
