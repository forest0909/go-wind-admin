package service

import (
	"sort"

	"context"

	"github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"github.com/getkin/kin-openapi/openapi3"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/cmd/server/assets"
	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/authorizer"
)

type RouteWalker interface {
	WalkRoute(fn http.WalkRouteFunc) error
}

type ApiService struct {
	adminV1.ApiServiceHTTPServer

	log *bLogger.Helper

	repo        *data.ApiRepo
	authorizer  *authorizer.Authorizer
	routeWalker RouteWalker
}

func NewApiService(
	ctx *bootstrap.Context,
	repo *data.ApiRepo,
	authorizer *authorizer.Authorizer,
) *ApiService {
	svc := &ApiService{
		log:        ctx.NewLoggerHelper("api/service/admin-service"),
		repo:       repo,
		authorizer: authorizer,
	}

	svc.init()

	return svc
}

func (s *ApiService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	// 内嵌 openapi.yaml 和接口表不一致就自动同步，部署新版本后不必再手点「接口同步」。
	// 同步按 (method, path) 合并，一致时只多一次列表查询。
	outdated, err := s.apisOutdated(ctx)
	if err != nil {
		s.log.Errorf(ctx, "启动期比对接口表失败: %v", err)
	} else if outdated {
		if _, err = s.SyncApis(ctx, &emptypb.Empty{}); err != nil {
			s.log.Errorf(ctx, "启动期接口同步失败: %v", err)
		}
	}
	// SyncApis 内部的对齐只在空表首启（或手工触发同步）时跑到；已部署实例的
	// sys_apis 序列被上一次同步的显式 ID 留在原地，只能靠这条启动期自愈补上。
	// 重复执行是幂等的（只前进不回退）。
	if err := s.repo.AlignIdentitySequence(ctx); err != nil {
		s.log.Errorf(ctx, "接口 id 序列对齐失败: %v", err)
	}
}

func (s *ApiService) RegisterRouteWalker(routeWalker RouteWalker) {
	s.routeWalker = routeWalker
}

func (s *ApiService) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListApiResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *ApiService) Get(ctx context.Context, req *permissionV1.GetApiRequest) (*permissionV1.Api, error) {
	return s.repo.Get(ctx, req)
}

func (s *ApiService) Create(ctx context.Context, req *permissionV1.CreateApiRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) Update(ctx context.Context, req *permissionV1.UpdateApiRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.repo.Update(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) Delete(ctx context.Context, req *permissionV1.DeleteApiRequest) (*emptypb.Empty, error) {
	if err := s.repo.Delete(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) SyncApis(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	//if err := s.syncWithWalkRoute(ctx); err != nil {
	//	return nil, err
	//}

	if err := s.syncWithOpenAPI(ctx); err != nil {
		return nil, err
	}

	// 新接口按显式 ID 写入，PG 的序列不会跟着走；不对齐的话
	// 之后「新建 API」必撞 sys_apis_pkey（HTTP 500）。
	if err := s.repo.AlignIdentitySequence(ctx); err != nil {
		s.log.Errorf(ctx, "接口 id 序列对齐失败: %v", err)
	}

	// 重置权限策略
	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// apiEndpointKey 接口的稳定身份：同一 (method, path) 跨多次同步保持同一个 ID。
func apiEndpointKey(method, path string) string {
	return method + " " + path
}

// loadOpenAPIApis 从内嵌 OpenAPI 文档读出全部接口，按 (path, method) 排序
func (s *ApiService) loadOpenAPIApis(ctx context.Context) ([]*permissionV1.Api, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(assets.OpenApiData)
	if err != nil {
		// 此前用 log.Fatal（os.Exit 直接终止进程），其后的 return 是死代码。
		// 启动期同步失败应返回错误由上层决定，而非杀掉整个进程。
		s.log.Errorf(ctx, "加载 OpenAPI 文档失败: %v", err)
		return nil, adminV1.ErrorInternalServerError("load OpenAPI document failed")
	}

	if doc == nil {
		s.log.Error(ctx, "OpenAPI 文档为空")
		return nil, adminV1.ErrorInternalServerError("OpenAPI document is nil")
	}
	if doc.Paths == nil {
		s.log.Error(ctx, "OpenAPI 文档的路径为空")
		return nil, adminV1.ErrorInternalServerError("OpenAPI document paths is nil")
	}

	var apiList []*permissionV1.Api

	// 遍历所有路径和操作
	for path, pathItem := range doc.Paths.Map() {
		for method, operation := range pathItem.Operations() {

			var module string
			var moduleDescription string
			if len(operation.Tags) > 0 {
				tag := doc.Tags.Get(operation.Tags[0])
				if tag != nil {
					module = tag.Name
					moduleDescription = tag.Description
				}
			}

			var businessModule = identityV1.Module_MODULE_UNSPECIFIED
			if module != "" {
				if bm, ok := constants.ServiceTagToBusinessModule[module]; ok {
					businessModule = bm
				}
			}

			apiList = append(apiList, &permissionV1.Api{
				Path:              trans.Ptr(path),
				Method:            trans.Ptr(method),
				Module:            trans.Ptr(module),
				ModuleDescription: trans.Ptr(moduleDescription),
				BusinessModule:    trans.Ptr(businessModule),
				Description:       trans.Ptr(operation.Description),
				Operation:         trans.Ptr(operation.OperationID),
			})
		}
	}

	// 新接口的 ID 分配顺序要确定，不能随 map 遍历顺序变化
	sort.Slice(apiList, func(i, j int) bool {
		if apiList[i].GetPath() != apiList[j].GetPath() {
			return apiList[i].GetPath() < apiList[j].GetPath()
		}
		return apiList[i].GetMethod() < apiList[j].GetMethod()
	})

	return apiList, nil
}

// apisOutdated 接口表与内嵌 OpenAPI 文档是否不一致（增删接口、模块或描述变化）
func (s *ApiService) apisOutdated(ctx context.Context) (bool, error) {
	want, err := s.loadOpenAPIApis(ctx)
	if err != nil {
		return false, err
	}
	have, err := s.repo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	if err != nil {
		return false, err
	}
	if len(have.Items) != len(want) {
		return true, nil
	}
	byKey := make(map[string]*permissionV1.Api, len(have.Items))
	for _, a := range have.Items {
		byKey[apiEndpointKey(a.GetMethod(), a.GetPath())] = a
	}
	for _, w := range want {
		h, ok := byKey[apiEndpointKey(w.GetMethod(), w.GetPath())]
		if !ok ||
			h.GetModule() != w.GetModule() ||
			h.GetModuleDescription() != w.GetModuleDescription() ||
			h.GetBusinessModule() != w.GetBusinessModule() ||
			h.GetDescription() != w.GetDescription() ||
			h.GetOperation() != w.GetOperation() {
			return true, nil
		}
	}
	return false, nil
}

// syncWithOpenAPI 使用 OpenAPI 文档对齐 API 资源。
//
// 按 (method, path) 对齐而不是清空重建：已有接口保留原 ID，新接口按路径排序后
// 接在最大 ID 之后，文档里已不存在的接口连同权限绑定一起删除。
// sys_permission_apis 按 api_id 记录绑定，清空重建会让 ID 重新分配（文档路径表是 map，
// 遍历顺序随机），所有角色的接口授权都会错位到无关接口上。
func (s *ApiService) syncWithOpenAPI(ctx context.Context) error {
	apiList, err := s.loadOpenAPIApis(ctx)
	if err != nil {
		return err
	}

	existing, err := s.repo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	if err != nil {
		s.log.Errorf(ctx, "读取现有接口失败: %v", err)
		return err
	}
	existingIDs := make(map[string]uint32, len(existing.Items))
	var maxID uint32
	for _, a := range existing.Items {
		existingIDs[apiEndpointKey(a.GetMethod(), a.GetPath())] = a.GetId()
		maxID = max(maxID, a.GetId())
	}

	keepIDs := make([]uint32, 0, len(apiList))
	for _, res := range apiList {
		id, ok := existingIDs[apiEndpointKey(res.GetMethod(), res.GetPath())]
		if !ok {
			maxID++
			id = maxID
		}
		res.Id = trans.Ptr(id)
		keepIDs = append(keepIDs, id)

		// 请求自身的 id 必须同步设置：repo.Update 的 id==0 守卫在
		// AllowMissing 分支之前，缺省时整批请求被静默弹回（历史上
		// 接口同步因此从未写入任何行）。
		if err = s.repo.Update(ctx, &permissionV1.UpdateApiRequest{
			Id:           id,
			AllowMissing: trans.Ptr(true),
			Data:         res,
		}); err != nil {
			s.log.Errorf(ctx, "写入接口 [%s %s] 失败: %v", res.GetMethod(), res.GetPath(), err)
			return err
		}
	}

	removed, err := s.repo.DeleteExcept(ctx, keepIDs)
	if err != nil {
		return err
	}

	s.log.Infof(ctx, "接口同步完成：共 %d 个，新增 %d 个，删除 %d 个",
		len(apiList), len(apiList)-(len(existing.Items)-removed), removed)

	return nil
}

func (s *ApiService) GetWalkRouteData(_ context.Context, _ *emptypb.Empty) (*permissionV1.ListApiResponse, error) {
	if s.routeWalker == nil {
		return nil, adminV1.ErrorInternalServerError("router walker is nil")
	}

	resp := &permissionV1.ListApiResponse{
		Items: []*permissionV1.Api{},
	}
	var count uint32 = 0
	if err := s.routeWalker.WalkRoute(func(info http.RouteInfo) error {
		//log.Infof("Path[%s] Method[%s]", info.Path, info.Method)
		count++
		resp.Items = append(resp.Items, &permissionV1.Api{
			Id:     trans.Ptr(count),
			Path:   trans.Ptr(info.Path),
			Method: trans.Ptr(info.Method),
			Status: trans.Ptr(permissionV1.Api_ON),
		})
		return nil
	}); err != nil {
		s.log.Errorf(context.Background(), "failed to walk route: %v", err)
		return nil, adminV1.ErrorInternalServerError("failed to walk route")
	}
	resp.Total = uint64(count)

	return resp, nil
}
