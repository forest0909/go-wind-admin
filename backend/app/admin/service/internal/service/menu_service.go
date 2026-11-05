package service

import (
	"context"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-crud/viewer"
)

type MenuService struct {
	adminV1.MenuServiceHTTPServer

	log *bLogger.Helper

	menuRepo *data.MenuRepo
}

func NewMenuService(ctx *bootstrap.Context, menuRepo *data.MenuRepo) *MenuService {
	svc := &MenuService{
		log:      ctx.NewLoggerHelper("menu/service/admin-service"),
		menuRepo: menuRepo,
	}

	svc.init()

	return svc
}

func (s *MenuService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.menuRepo.Count(ctx, nil); count == 0 {
		_ = s.createDefaultMenus(ctx)
	}

	// 种子是带显式 ID 插入的，PG 的 identity 序列不会因此前进；不对齐的话
	// 「新建菜单」与菜单同步的新增分支必撞 sys_menus_pkey（HTTP 500）。
	// 放在 count==0 守卫之外：已部署实例的序列同样落后，需要每次启动自愈。
	if err := s.menuRepo.AlignIdentitySequence(ctx); err != nil {
		s.log.Errorf(ctx, "菜单 id 序列对齐失败: %v", err)
	}
}

func (s *MenuService) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListMenuResponse, error) {
	ret, err := s.menuRepo.List(ctx, req, false)
	if err != nil {

		return nil, err
	}

	return ret, nil
}

func (s *MenuService) Get(ctx context.Context, req *permissionV1.GetMenuRequest) (*permissionV1.Menu, error) {
	ret, err := s.menuRepo.Get(ctx, req)
	if err != nil {

		return nil, err
	}

	return ret, nil
}

func (s *MenuService) Create(ctx context.Context, req *permissionV1.CreateMenuRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.menuRepo.Create(ctx, req); err != nil {

		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *MenuService) Update(ctx context.Context, req *permissionV1.UpdateMenuRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
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

	if err = s.menuRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *MenuService) Delete(ctx context.Context, req *permissionV1.DeleteMenuRequest) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.OperatorId = trans.Ptr(operator.UserId)

	if err := s.menuRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// SyncMenus 同步菜单：将前端推送的路由树按模式合并进数据库。
// REPLACE（缺省）= 清空重建（菜单 ID 全变，角色授权失效）；MERGE = 按全路径增量合并（保留 ID 与角色授权）。
// 两模式均在单个事务内执行（见 MenuRepo.SyncMenus）。
func (s *MenuService) SyncMenus(ctx context.Context, req *permissionV1.SyncMenusRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	count, err := s.menuRepo.SyncMenus(ctx, req.GetItems(), req.GetMode(), operator.UserId)
	if err != nil {
		return nil, err
	}

	s.log.Infof(ctx, "sync menus success, mode: %s, total: %d", req.GetMode().String(), count)

	return &emptypb.Empty{}, nil
}

func (s *MenuService) createDefaultMenus(ctx context.Context) error {
	for _, m := range parentFirstMenus(constants.DefaultMenus) {
		if err := s.menuRepo.Create(ctx, &permissionV1.CreateMenuRequest{Data: m}); err != nil {
			s.log.Errorf(ctx, "create default menu err: %v", err)
			return err
		}
	}
	return nil
}

// parentFirstMenus 把菜单排成「父菜单先于子菜单」的插入顺序，同层保持原有先后。
// sys_menus.parent_id 是自引用外键，子菜单先插会违约；DefaultMenus 的书写顺序按页面分组，
// 不保证父在前（如 ProfilePage 挂在后面才定义的 /opm 下），所以播种前必须重排。
// 父 ID 不在列表里的菜单原样放到最后，交给数据库报错，不在这里静默丢弃。
func parentFirstMenus(menus []*permissionV1.Menu) []*permissionV1.Menu {
	out := make([]*permissionV1.Menu, 0, len(menus))
	placed := make(map[uint32]bool, len(menus))
	pending := menus
	for len(pending) > 0 {
		next := pending[:0:0]
		for _, m := range pending {
			if m.GetParentId() == 0 || placed[m.GetParentId()] {
				out = append(out, m)
				placed[m.GetId()] = true
			} else {
				next = append(next, m)
			}
		}
		if len(next) == len(pending) {
			return append(out, next...)
		}
		pending = next
	}
	return out
}
